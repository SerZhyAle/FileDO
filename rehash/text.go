package rehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"
)

// ---- JSON (RFC 8259: trailing insignificant space) -------------------------

func decodeStrictJSON(data []byte) (any, error) {
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		return nil, fmt.Errorf("UTF-8 BOM is not allowed in strict RFC 8259 JSON")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON syntax")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("multiple or trailing JSON values (NDJSON/JSONL not modified)")
	}
	return val, nil
}

type jsonPlan struct {
	data []byte
	val  any
}

type jsonHandler struct{}

func (jsonHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "JSON",
		Extensions: []string{".json"},
		MinDelta:   1,
		MaxDelta:   1,
		Mechanism:  "trailing RFC 8259 insignificant space",
	}
}

func (jsonHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "JSON")
	if err != nil {
		return nil, err
	}
	val, err := decodeStrictJSON(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid JSON: %v", err)}
	}
	return &jsonPlan{data: data, val: val}, nil
}

func (jsonHandler) writeAndVerify(_ context.Context, _ *os.File, _ int64, p any, dst *os.File, _ string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*jsonPlan)
	out := make([]byte, len(plan.data)+1)
	copy(out, plan.data)
	out[len(plan.data)] = ' '

	outVal, err := decodeStrictJSON(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JSON output failed strict decode: %w", err)
	}
	if !reflect.DeepEqual(plan.val, outVal) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JSON decoded value changed")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}

// ---- XML / SVG (W3C XML 1.0: trailing comment after root element) ----------

const xmlDSigNS = "http://www.w3.org/2000/09/xmldsig#"

func parseStrictXML(data []byte) ([]xml.Token, bool, error) {
	if len(data) == 0 {
		return nil, false, fmt.Errorf("empty XML document")
	}
	if (len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF))) ||
		!utf8.Valid(data) {
		return nil, false, fmt.Errorf("XML is not valid UTF-8")
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var tokens []xml.Token
	depth := 0
	roots := 0
	signed := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
			if strings.EqualFold(t.Name.Space, xmlDSigNS) || strings.EqualFold(t.Name.Local, "Signature") {
				signed = true
			}
			for _, a := range t.Attr {
				if strings.EqualFold(a.Value, xmlDSigNS) {
					signed = true
				}
			}
			tokens = append(tokens, xml.CopyToken(tok))
		case xml.EndElement:
			depth--
			tokens = append(tokens, xml.CopyToken(tok))
		case xml.Comment:
			// Comments are ignored when comparing semantic XML content.
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) == 0 {
				continue
			}
			if depth == 0 {
				return nil, false, fmt.Errorf("non-whitespace text outside XML root element")
			}
			tokens = append(tokens, xml.CopyToken(tok))
		default:
			tokens = append(tokens, xml.CopyToken(tok))
		}
	}
	if roots != 1 || depth != 0 {
		return nil, false, fmt.Errorf("XML must contain exactly one balanced root element")
	}
	return tokens, signed, nil
}

type xmlPlan struct {
	data   []byte
	tokens []xml.Token
}

type xmlHandler struct{}

func (xmlHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "XML",
		Extensions: []string{".xml", ".svg"},
		MinDelta:   37,
		MaxDelta:   37,
		Mechanism:  "XML 1.0 comment after root element",
	}
}

func (xmlHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "XML")
	if err != nil {
		return nil, err
	}
	tokens, signed, err := parseStrictXML(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid XML/SVG: %v", err)}
	}
	if signed {
		return nil, &SkipError{Code: "signed-file", Reason: "XML document contains digital signature metadata that would be invalidated"}
	}
	return &xmlPlan{data: data, tokens: tokens}, nil
}

func (xmlHandler) writeAndVerify(_ context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*xmlPlan)
	// "<!--" (4) + "FileDO-rehash:" (14) + nonce (16) + "-->" (3) = 37 bytes.
	comment := "<!--" + tagPrefix + nonce + "-->"
	out := make([]byte, 0, len(plan.data)+len(comment))
	out = append(out, plan.data...)
	out = append(out, comment...)

	outTokens, outSigned, err := parseStrictXML(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("XML output failed strict parse: %w", err)
	}
	if outSigned || !reflect.DeepEqual(plan.tokens, outTokens) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("XML token stream changed")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}
