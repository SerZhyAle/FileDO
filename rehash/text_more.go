package rehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// ---- Plain text, Markdown, HTML, CSS, JavaScript, INI ----------------------
//
// Each kind ends with what its own syntax ignores: a line ending for plain
// text, an HTML comment for Markdown and HTML, a block comment for CSS and
// JavaScript, a ";" comment line for INI. The file's own encoding (UTF-8,
// legacy 8-bit, or UTF-16 with a BOM) and its line-ending style are kept.

type textKind int

const (
	textPlain textKind = iota
	textMarkup
	textCode
	textINI
)

type textEncoding int

const (
	enc8 textEncoding = iota
	encUTF16LE
	encUTF16BE
)

type textHandler struct{ kind textKind }

func (h textHandler) spec() FormatSpec {
	switch h.kind {
	case textMarkup:
		return FormatSpec{Name: "Markdown/HTML", Extensions: []string{".md", ".markdown", ".html", ".htm"}, MinDelta: 40, MaxDelta: 86,
			Mechanism: "trailing HTML comment line"}
	case textCode:
		return FormatSpec{Name: "CSS/JS", Extensions: []string{".css", ".js"}, MinDelta: 37, MaxDelta: 80,
			Mechanism: "trailing block comment line"}
	case textINI:
		return FormatSpec{Name: "INI", Extensions: []string{".ini"}, MinDelta: 33, MaxDelta: 72,
			Mechanism: "trailing ; comment line"}
	}
	return FormatSpec{Name: "Text", Extensions: []string{".txt", ".log"}, MinDelta: 1, MaxDelta: 4,
		Mechanism: "one trailing line ending in the file's own style"}
}

type textPlan struct {
	data   []byte
	enc    textEncoding
	eol    string
	endsNL bool
	isMD   bool
}

// decodeText returns the file as a Go string plus its encoding.
func decodeText(data []byte) (string, textEncoding, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0, 0}) || bytes.HasPrefix(data, []byte{0, 0, 0xFE, 0xFF}):
		return "", 0, fmt.Errorf("UTF-32 text is not supported")
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		if len(data)%2 != 0 {
			return "", 0, fmt.Errorf("UTF-16 text has an odd byte count")
		}
		enc := encUTF16LE
		if data[0] == 0xFE {
			enc = encUTF16BE
		}
		u := make([]uint16, 0, len(data)/2-1)
		for i := 2; i < len(data); i += 2 {
			if enc == encUTF16LE {
				u = append(u, uint16(data[i])|uint16(data[i+1])<<8)
			} else {
				u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
			}
		}
		return string(utf16.Decode(u)), enc, nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", 0, fmt.Errorf("file holds NUL bytes (binary data or UTF-16 without a BOM)")
	}
	return string(data), enc8, nil
}

func encodeText(s string, enc textEncoding) []byte {
	if enc == enc8 {
		return []byte(s)
	}
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2*len(u))
	for _, c := range u {
		if enc == encUTF16LE {
			b = append(b, byte(c), byte(c>>8))
		} else {
			b = append(b, byte(c>>8), byte(c))
		}
	}
	return b
}

// markdownFenceOpen reports a fenced code block left open at the end, where
// a trailing comment would render as visible text.
func markdownFenceOpen(s string) bool {
	open := ""
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		t := strings.TrimLeft(line, " ")
		if len(line)-len(t) > 3 {
			continue
		}
		for _, f := range []string{"```", "~~~"} {
			if strings.HasPrefix(t, f) {
				if open == "" {
					open = f
				} else if open == f {
					open = ""
				}
			}
		}
	}
	return open != ""
}

func (h textHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, h.spec().Name)
	if err != nil {
		return nil, err
	}
	s, enc, err := decodeText(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid %s: %v", h.spec().Name, err)}
	}
	if strings.Contains(s, "-----BEGIN PGP SIGNATURE-----") {
		return nil, &SkipError{Code: "signed-file", Reason: "text carries a PGP signature"}
	}
	ext := strings.ToLower(filepath.Ext(src.Name()))
	isMD := ext == ".md" || ext == ".markdown"
	if isMD && markdownFenceOpen(s) {
		return nil, &SkipError{Code: "invalid-format", Reason: "Markdown ends inside an open code fence; a comment would show"}
	}
	eol := "\r\n"
	switch {
	case strings.Contains(s, "\r\n"):
	case strings.Contains(s, "\n"):
		eol = "\n"
	case strings.Contains(s, "\r"):
		eol = "\r"
	}
	return &textPlan{data: data, enc: enc, eol: eol, endsNL: strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r"), isMD: isMD}, nil
}

func (h textHandler) suffix(plan *textPlan, nonce string) string {
	lead := ""
	if !plan.endsNL {
		lead = plan.eol
	}
	tag := tagPrefix + nonce
	switch h.kind {
	case textMarkup:
		return lead + "<!-- " + tag + " -->" + plan.eol
	case textCode:
		return lead + "/* " + tag + " */" + plan.eol
	case textINI:
		return lead + "; " + tag + plan.eol
	}
	return plan.eol
}

func (h textHandler) writeAndVerify(_ context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*textPlan)
	add := encodeText(h.suffix(plan, nonce), plan.enc)
	out := append(append(make([]byte, 0, len(plan.data)+len(add)), plan.data...), add...)
	s, enc, err := decodeText(out)
	if err != nil || enc != plan.enc {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("%s output failed strict decode", h.spec().Name)
	}
	if plan.isMD && markdownFenceOpen(s) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("Markdown output ends inside a code fence")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}

// ---- RTF (ignorable destination {\*\filedorehash ..} before the last brace) -

// scanRTF returns the offset of the brace that closes the document group.
func scanRTF(d []byte) (int, error) {
	if !bytes.HasPrefix(d, []byte(`{\rtf`)) {
		return 0, fmt.Errorf("missing {\\rtf header")
	}
	depth := 0
	for i := 0; i < len(d); i++ {
		switch d[i] {
		case '\\':
			if i+1 >= len(d) {
				return 0, fmt.Errorf("truncated control symbol at EOF")
			}
			if bytes.HasPrefix(d[i+1:], []byte("bin")) {
				j := i + 4
				n := 0
				for j < len(d) && d[j] >= '0' && d[j] <= '9' && n < 1<<30 {
					n = n*10 + int(d[j]-'0')
					j++
				}
				if j > i+4 {
					if j < len(d) && d[j] == ' ' {
						j++
					}
					if j+n > len(d) {
						return 0, fmt.Errorf("\\bin data extends past EOF")
					}
					i = j + n - 1
					continue
				}
			}
			i++ // skip the escaped byte, so \{ \} \\ never count
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return 0, fmt.Errorf("unbalanced closing brace at offset %d", i)
			}
			if depth == 0 {
				if len(bytes.Trim(d[i+1:], "\x00\t\n\r ")) != 0 {
					return 0, fmt.Errorf("data after the RTF document group")
				}
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("RTF document group is not closed")
}

type rtfPlan struct {
	data []byte
	end  int
}

type rtfHandler struct{}

func (rtfHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "RTF",
		Extensions: []string{".rtf"},
		MinDelta:   48,
		MaxDelta:   48,
		Mechanism:  `ignorable destination {\*\filedorehash ..} before the closing brace`,
	}
}

func (rtfHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "RTF")
	if err != nil {
		return nil, err
	}
	end, err := scanRTF(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid RTF: %v", err)}
	}
	return &rtfPlan{data: data, end: end}, nil
}

func (rtfHandler) writeAndVerify(_ context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*rtfPlan)
	group := `{\*\filedorehash ` + tagPrefix + nonce + `}`
	out := make([]byte, 0, len(plan.data)+len(group))
	out = append(out, plan.data[:plan.end]...)
	out = append(out, group...)
	out = append(out, plan.data[plan.end:]...)
	end, err := scanRTF(out)
	if err != nil || end != plan.end+len(group) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("RTF output failed strict group verification")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}
