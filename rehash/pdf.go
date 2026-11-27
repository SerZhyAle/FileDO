package rehash

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
)

// ---- PDF (ISO 32000 7.5.6: incremental update) -----------------------------
//
// The copy keeps every original byte and appends one update section: a new,
// unreferenced object that carries the rehash tag, and a cross-reference
// section in the source's own form (a table, or an uncompressed xref stream)
// whose trailer repeats /Root, /Info and /ID and points back with /Prev. The
// whole xref chain of the input and of the output is resolved strictly: every
// in-use entry must land on its "n g obj" header.

type pdfObj struct {
	kind byte // 'd' dict, 'a' array, 'n' name, 'i' integer, 'f' real, 'r' reference, 's' string, 'k' keyword
	raw  []byte
	num  int64
	name string
	dict map[string]*pdfObj
	arr  []*pdfObj
}

func pdfIsWS(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func pdfIsDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func pdfSkipWS(d []byte, i int) int {
	for i < len(d) {
		switch {
		case pdfIsWS(d[i]):
			i++
		case d[i] == '%':
			for i < len(d) && d[i] != '\n' && d[i] != '\r' {
				i++
			}
		default:
			return i
		}
	}
	return i
}

func pdfToken(d []byte, i int) (string, int) {
	j := i
	for j < len(d) && !pdfIsWS(d[j]) && !pdfIsDelim(d[j]) {
		j++
	}
	return string(d[i:j]), j
}

func pdfInt(d []byte, i int) (int64, int, bool) {
	tok, j := pdfToken(d, i)
	v, err := strconv.ParseInt(tok, 10, 64)
	if err != nil || tok == "" || tok[0] == '+' {
		return 0, i, false
	}
	return v, j, true
}

func pdfParse(d []byte, i, depth int) (*pdfObj, int, error) {
	if depth > 64 {
		return nil, i, fmt.Errorf("PDF object nests too deeply")
	}
	i = pdfSkipWS(d, i)
	if i >= len(d) {
		return nil, i, fmt.Errorf("unexpected end of PDF data")
	}
	start := i
	o := &pdfObj{}
	switch c := d[i]; {
	case c == '<' && i+1 < len(d) && d[i+1] == '<':
		o.kind, o.dict = 'd', map[string]*pdfObj{}
		i += 2
		for {
			i = pdfSkipWS(d, i)
			if i+1 < len(d) && d[i] == '>' && d[i+1] == '>' {
				i += 2
				break
			}
			if i >= len(d) || d[i] != '/' {
				return nil, i, fmt.Errorf("PDF dictionary key expected at offset %d", i)
			}
			key, j := pdfToken(d, i+1)
			v, j, err := pdfParse(d, j, depth+1)
			if err != nil {
				return nil, j, err
			}
			o.dict[key] = v
			i = j
		}
	case c == '<':
		j := bytes.IndexByte(d[i:], '>')
		if j < 0 {
			return nil, i, fmt.Errorf("unterminated PDF hex string")
		}
		o.kind, i = 's', i+j+1
	case c == '[':
		o.kind = 'a'
		i++
		for {
			i = pdfSkipWS(d, i)
			if i < len(d) && d[i] == ']' {
				i++
				break
			}
			v, j, err := pdfParse(d, i, depth+1)
			if err != nil {
				return nil, j, err
			}
			o.arr = append(o.arr, v)
			i = j
		}
	case c == '(':
		o.kind = 's'
		nest := 0
		for i < len(d) {
			ch := d[i]
			i++
			if ch == '\\' {
				i++
				continue
			}
			if ch == '(' {
				nest++
			} else if ch == ')' {
				nest--
				if nest == 0 {
					break
				}
			}
		}
		if nest != 0 {
			return nil, i, fmt.Errorf("unterminated PDF string")
		}
	case c == '/':
		o.kind = 'n'
		o.name, i = pdfToken(d, i+1)
	case c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.':
		tok, j := pdfToken(d, i)
		if v, err := strconv.ParseInt(tok, 10, 64); err == nil {
			o.kind, o.num, i = 'i', v, j
			// "n g R" is an indirect reference.
			k := pdfSkipWS(d, j)
			if g, k2, ok := pdfInt(d, k); ok && g >= 0 && v >= 0 {
				k2 = pdfSkipWS(d, k2)
				if k2 < len(d) && d[k2] == 'R' && (k2+1 == len(d) || pdfIsWS(d[k2+1]) || pdfIsDelim(d[k2+1])) {
					o.kind, i = 'r', k2+1
				}
			}
		} else if _, err := strconv.ParseFloat(tok, 64); err == nil {
			o.kind, i = 'f', j
		} else {
			return nil, i, fmt.Errorf("invalid PDF number %q", tok)
		}
	default:
		tok, j := pdfToken(d, i)
		if tok == "" {
			return nil, i, fmt.Errorf("unexpected PDF byte 0x%02X at offset %d", c, i)
		}
		o.kind, i = 'k', j
	}
	o.raw = d[start:i]
	return o, i, nil
}

func (o *pdfObj) get(key string) *pdfObj {
	if o == nil || o.kind != 'd' {
		return nil
	}
	return o.dict[key]
}

func (o *pdfObj) intVal(key string) (int64, bool) {
	v := o.get(key)
	if v == nil || v.kind != 'i' {
		return 0, false
	}
	return v.num, true
}

// pdfObjHeader parses "num gen obj" at off.
func pdfObjHeader(d []byte, off int64) (num, gen int64, after int, err error) {
	if off < 0 || off >= int64(len(d)) {
		return 0, 0, 0, fmt.Errorf("object offset %d out of range", off)
	}
	i := pdfSkipWS(d, int(off))
	num, i, ok1 := pdfInt(d, i)
	i = pdfSkipWS(d, i)
	gen, i, ok2 := pdfInt(d, i)
	i = pdfSkipWS(d, i)
	kw, j := pdfToken(d, i)
	if !ok1 || !ok2 || kw != "obj" {
		return 0, 0, 0, fmt.Errorf("no object header at offset %d", off)
	}
	return num, gen, j, nil
}

type pdfXref struct {
	stream  bool
	trailer *pdfObj
	objects map[int64]int64 // object number -> offset of in-use type-1 entries
}

func pdfReadTable(d []byte, off int) (*pdfXref, error) {
	x := &pdfXref{objects: map[int64]int64{}}
	i := off + 4
	for {
		i = pdfSkipWS(d, i)
		if bytes.HasPrefix(d[i:], []byte("trailer")) {
			i += 7
			break
		}
		first, j, ok1 := pdfInt(d, i)
		j = pdfSkipWS(d, j)
		count, j, ok2 := pdfInt(d, j)
		if !ok1 || !ok2 || first < 0 || count < 0 || count > 1<<24 {
			return nil, fmt.Errorf("invalid xref subsection at offset %d", i)
		}
		i = j
		for k := int64(0); k < count; k++ {
			i = pdfSkipWS(d, i)
			o, j, ok1 := pdfInt(d, i)
			j = pdfSkipWS(d, j)
			_, j, ok2 := pdfInt(d, j)
			j = pdfSkipWS(d, j)
			if !ok1 || !ok2 || j >= len(d) || (d[j] != 'n' && d[j] != 'f') {
				return nil, fmt.Errorf("invalid xref entry at offset %d", i)
			}
			if d[j] == 'n' && o > 0 {
				x.objects[first+k] = o
			}
			i = j + 1
		}
	}
	t, _, err := pdfParse(d, i, 0)
	if err != nil || t.kind != 'd' {
		return nil, fmt.Errorf("invalid trailer dictionary")
	}
	x.trailer = t
	return x, nil
}

func pdfPNGUnpredict(b []byte, columns int) ([]byte, error) {
	row := columns + 1
	if columns <= 0 || len(b)%row != 0 {
		return nil, fmt.Errorf("xref stream predictor rows do not fit")
	}
	out := make([]byte, 0, len(b)/row*columns)
	prev := make([]byte, columns)
	for r := 0; r < len(b); r += row {
		f, cur := b[r], append([]byte(nil), b[r+1:r+row]...)
		for x := range cur {
			var a, c byte
			if x > 0 {
				a, c = cur[x-1], prev[x-1]
			}
			up := prev[x]
			switch f {
			case 0:
			case 1:
				cur[x] += a
			case 2:
				cur[x] += up
			case 3:
				cur[x] += byte((int(a) + int(up)) / 2)
			case 4:
				p := int(a) + int(up) - int(c)
				pa, pb, pc := abs(p-int(a)), abs(p-int(up)), abs(p-int(c))
				switch {
				case pa <= pb && pa <= pc:
					cur[x] += a
				case pb <= pc:
					cur[x] += up
				default:
					cur[x] += c
				}
			default:
				return nil, fmt.Errorf("unknown PNG predictor filter %d", f)
			}
		}
		out = append(out, cur...)
		prev = cur
	}
	return out, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pdfReadStream(d []byte, off int64) (*pdfXref, error) {
	_, _, i, err := pdfObjHeader(d, off)
	if err != nil {
		return nil, err
	}
	dict, i, err := pdfParse(d, i, 0)
	if err != nil || dict.kind != 'd' {
		return nil, fmt.Errorf("invalid xref stream dictionary")
	}
	if t := dict.get("Type"); t == nil || t.name != "XRef" {
		return nil, fmt.Errorf("object at startxref is not an xref stream")
	}
	length, ok := dict.intVal("Length")
	if !ok || length < 0 {
		return nil, fmt.Errorf("xref stream /Length is not a direct integer")
	}
	i = pdfSkipWS(d, i)
	if !bytes.HasPrefix(d[i:], []byte("stream")) {
		return nil, fmt.Errorf("xref stream keyword missing")
	}
	i += 6
	if i+1 < len(d) && d[i] == '\r' && d[i+1] == '\n' {
		i += 2
	} else if i < len(d) && (d[i] == '\n' || d[i] == '\r') {
		i++
	} else {
		return nil, fmt.Errorf("xref stream keyword not followed by EOL")
	}
	if int64(i)+length > int64(len(d)) {
		return nil, fmt.Errorf("xref stream data extends past EOF")
	}
	raw := d[i : i+int(length)]
	if e := pdfSkipWS(d, i+int(length)); !bytes.HasPrefix(d[e:], []byte("endstream")) {
		return nil, fmt.Errorf("xref stream /Length does not reach endstream")
	}

	var w [3]int
	wa := dict.get("W")
	if wa == nil || wa.kind != 'a' || len(wa.arr) != 3 {
		return nil, fmt.Errorf("xref stream /W invalid")
	}
	rowLen := 0
	for k, v := range wa.arr {
		if v.kind != 'i' || v.num < 0 || v.num > 8 {
			return nil, fmt.Errorf("xref stream /W invalid")
		}
		w[k] = int(v.num)
		rowLen += w[k]
	}
	if rowLen == 0 {
		return nil, fmt.Errorf("xref stream /W is empty")
	}
	size, ok := dict.intVal("Size")
	if !ok || size < 0 {
		return nil, fmt.Errorf("xref stream /Size missing")
	}
	index := []int64{0, size}
	if ix := dict.get("Index"); ix != nil {
		if ix.kind != 'a' || len(ix.arr)%2 != 0 {
			return nil, fmt.Errorf("xref stream /Index invalid")
		}
		index = index[:0]
		for _, v := range ix.arr {
			if v.kind != 'i' || v.num < 0 {
				return nil, fmt.Errorf("xref stream /Index invalid")
			}
			index = append(index, v.num)
		}
	}
	total := int64(0)
	for k := 1; k < len(index); k += 2 {
		total += index[k]
	}
	if total > 1<<24 {
		return nil, fmt.Errorf("xref stream too large")
	}

	data := raw
	filter := dict.get("Filter")
	if filter != nil && filter.kind == 'a' && len(filter.arr) == 1 {
		filter = filter.arr[0]
	}
	parms := dict.get("DecodeParms")
	if parms != nil && parms.kind == 'a' && len(parms.arr) == 1 {
		parms = parms.arr[0]
	}
	switch {
	case filter == nil:
	case filter.kind == 'n' && filter.name == "FlateDecode":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("xref stream is not valid Flate data: %w", err)
		}
		data, err = io.ReadAll(io.LimitReader(zr, total*int64(rowLen+1)+1))
		zr.Close()
		if err != nil {
			return nil, fmt.Errorf("xref stream Flate data is damaged: %w", err)
		}
	default:
		return nil, fmt.Errorf("xref stream filter is not supported")
	}
	if pred, ok := parms.intVal("Predictor"); ok && pred > 1 {
		if pred < 10 {
			return nil, fmt.Errorf("xref stream TIFF predictor is not supported")
		}
		cols, ok := parms.intVal("Columns")
		if !ok {
			cols = 1
		}
		if cols != int64(rowLen) {
			return nil, fmt.Errorf("xref stream predictor columns do not match /W")
		}
		if data, err = pdfPNGUnpredict(data, int(cols)); err != nil {
			return nil, err
		}
	}
	if int64(len(data)) < total*int64(rowLen) {
		return nil, fmt.Errorf("xref stream holds fewer entries than /Index declares")
	}

	field := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}
	x := &pdfXref{stream: true, trailer: dict, objects: map[int64]int64{}}
	p := 0
	for k := 0; k < len(index); k += 2 {
		for n := int64(0); n < index[k+1]; n++ {
			e := data[p : p+rowLen]
			p += rowLen
			typ := int64(1)
			if w[0] > 0 {
				typ = field(e[:w[0]])
			}
			if typ == 1 {
				if o := field(e[w[0] : w[0]+w[1]]); o > 0 {
					x.objects[index[k]+n] = o
				}
			}
		}
	}
	return x, nil
}

type pdfChain struct {
	sections int
	newest   *pdfXref
	startxrf int64
}

func pdfStartXref(d []byte) (int64, error) {
	tail := d
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	k := bytes.LastIndex(tail, []byte("startxref"))
	if k < 0 {
		return 0, fmt.Errorf("startxref not found near EOF")
	}
	i := pdfSkipWS(tail, k+9)
	off, j, ok := pdfInt(tail, i)
	if !ok || off <= 0 {
		return 0, fmt.Errorf("startxref offset invalid")
	}
	// Whitespace only: pdfSkipWS also skips comments, and %%EOF reads as one.
	for j < len(tail) && pdfIsWS(tail[j]) {
		j++
	}
	if !bytes.HasPrefix(tail[j:], []byte("%%EOF")) {
		return 0, fmt.Errorf("%%%%EOF missing after startxref")
	}
	if len(bytes.TrimRight(tail[j+5:], "\x00\t\n\f\r ")) != 0 {
		return 0, fmt.Errorf("data after the final %%%%EOF")
	}
	return off, nil
}

func pdfResolveChain(d []byte) (*pdfChain, error) {
	if !bytes.HasPrefix(d, []byte("%PDF-")) {
		return nil, fmt.Errorf("missing %%PDF- header")
	}
	start, err := pdfStartXref(d)
	if err != nil {
		return nil, err
	}
	ch := &pdfChain{startxrf: start}
	seen := map[int64]bool{}
	queue := []int64{start}
	for len(queue) > 0 {
		off := queue[0]
		queue = queue[1:]
		if seen[off] {
			continue
		}
		// Every rehash run adds one section, so the bound is far above any
		// realistic number of runs; seen already stops a looping chain.
		if len(seen) >= 1<<20 || off <= 0 || off >= int64(len(d)) {
			return nil, fmt.Errorf("xref chain offset %d invalid", off)
		}
		seen[off] = true
		var x *pdfXref
		if bytes.HasPrefix(d[off:], []byte("xref")) {
			x, err = pdfReadTable(d, int(off))
		} else {
			x, err = pdfReadStream(d, off)
		}
		if err != nil {
			return nil, err
		}
		for num, o := range x.objects {
			n, _, _, err := pdfObjHeader(d, o)
			if err != nil || n != num {
				return nil, fmt.Errorf("xref entry for object %d does not point at its header", num)
			}
		}
		if ch.newest == nil {
			ch.newest = x
		}
		ch.sections++
		if prev, ok := x.trailer.intVal("Prev"); ok {
			queue = append(queue, prev)
		}
		if xs, ok := x.trailer.intVal("XRefStm"); ok && !x.stream {
			queue = append(queue, xs)
		}
	}
	return ch, nil
}

type pdfPlan struct {
	data  []byte
	chain *pdfChain
	size  int64
	keys  []byte // " /Root .. /Info .. /ID [..]" copied from the newest trailer
}

type pdfHandler struct{}

func (pdfHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "PDF",
		Extensions: []string{".pdf"},
		MinDelta:   100,
		MaxDelta:   1024,
		Mechanism:  "incremental update: one new unreferenced object and an xref section in the source's own form with /Prev",
	}
}

func (pdfHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "PDF")
	if err != nil {
		return nil, err
	}
	skip := func(code, f string, a ...any) error {
		return &SkipError{Code: code, Reason: fmt.Sprintf(f, a...)}
	}
	if bytes.Contains(data, []byte("/ByteRange")) || bytes.Contains(data, []byte("/Perms")) {
		return nil, skip("signed-file", "PDF carries a digital signature that the update would mark as modified")
	}
	chain, err := pdfResolveChain(data)
	if err != nil {
		return nil, skip("invalid-format", "invalid PDF: %v", err)
	}
	t := chain.newest.trailer
	if t.get("Encrypt") != nil {
		return nil, skip("unsupported-format", "encrypted PDF is not modified")
	}
	size, ok := t.intVal("Size")
	root := t.get("Root")
	if !ok || size <= 0 || root == nil || root.kind != 'r' {
		return nil, skip("invalid-format", "invalid PDF: newest trailer lacks /Size or /Root")
	}
	keys := []byte(" /Root " + string(root.raw))
	if info := t.get("Info"); info != nil && info.kind == 'r' {
		keys = append(keys, " /Info "+string(info.raw)...)
	}
	if id := t.get("ID"); id != nil && id.kind == 'a' {
		keys = append(keys, " /ID "+string(id.raw)...)
	}
	if len(keys) > 640 {
		return nil, skip("invalid-format", "invalid PDF: trailer keys are unusually long")
	}
	return &pdfPlan{data: data, chain: chain, size: size, keys: keys}, nil
}

func (pdfHandler) writeAndVerify(_ context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*pdfPlan)
	var b bytes.Buffer
	b.Write(plan.data)
	if c := plan.data[len(plan.data)-1]; c != '\n' && c != '\r' {
		b.WriteByte('\n')
	}
	n := plan.size
	objOff := int64(b.Len())
	fmt.Fprintf(&b, "%d 0 obj\n(%s%s)\nendobj\n", n, tagPrefix, nonce)
	xrefOff := int64(b.Len())
	if xrefOff > 0xFFFFFFFF {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PDF too large for the update section")
	}
	if plan.chain.newest.stream {
		var rows [12]byte
		rows[0] = 1
		binary.BigEndian.PutUint32(rows[1:5], uint32(objOff))
		rows[6] = 1
		binary.BigEndian.PutUint32(rows[7:11], uint32(xrefOff))
		fmt.Fprintf(&b, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 1] /Index [%d 2] /Length 12%s /Prev %d >>\nstream\n",
			n+1, n+2, n, plan.keys, plan.chain.startxrf)
		b.Write(rows[:])
		fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xrefOff)
	} else {
		fmt.Fprintf(&b, "xref\n%d 1\n%010d 00000 n\r\ntrailer\n<< /Size %d%s /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
			n, objOff, n+1, plan.keys, plan.chain.startxrf, xrefOff)
	}
	out := b.Bytes()

	chain, err := pdfResolveChain(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PDF output failed strict xref verification: %w", err)
	}
	if chain.sections != plan.chain.sections+1 || chain.newest.objects[n] != objOff || chain.startxrf != xrefOff {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PDF output update section mismatch")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}
