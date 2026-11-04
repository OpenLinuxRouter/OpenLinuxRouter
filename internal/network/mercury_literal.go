package network

// This parser accepts data literals only. It deliberately has no JavaScript
// evaluator, property access, function calls, or executable expression support.
import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const mercuryBodyLimit = 1 << 20

var errMercuryLiteral = errors.New("mercury: invalid or unsupported page data")

type mercuryParser struct {
	text       string
	pos, nodes int
}

func mercuryLiteral(page, name string) (any, error) {
	if len(page) > mercuryBodyLimit {
		return nil, errMercuryLiteral
	}
	re := regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(name) + `\s*=\s*`)
	loc := re.FindStringIndex(page)
	if loc == nil {
		return nil, errMercuryLiteral
	}
	p := mercuryParser{text: page, pos: loc[1]}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	rest := p.text[p.pos:]
	if rest != "" && !strings.HasPrefix(rest, ";") && !strings.HasPrefix(rest, ",") && !strings.HasPrefix(rest, "</script>") && !strings.HasPrefix(rest, "var ") {
		return nil, errMercuryLiteral
	}
	return v, nil
}
func (p *mercuryParser) space() {
	for p.pos < len(p.text) && strings.ContainsRune(" \n\r\t", rune(p.text[p.pos])) {
		p.pos++
	}
}
func mercuryIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$'
}
func mercuryIdentMore(c byte) bool { return mercuryIdent(c) || c >= '0' && c <= '9' }
func (p *mercuryParser) take(c byte) bool {
	p.space()
	if p.pos < len(p.text) && p.text[p.pos] == c {
		p.pos++
		return true
	}
	return false
}
func (p *mercuryParser) value(depth int) (any, error) {
	p.nodes++
	p.space()
	if depth > 32 || p.nodes > 16384 || p.pos >= len(p.text) {
		return nil, errMercuryLiteral
	}
	switch p.text[p.pos] {
	case '{':
		p.pos++
		out := map[string]any{}
		if p.take('}') {
			return out, nil
		}
		for {
			p.space()
			if p.pos >= len(p.text) {
				return nil, errMercuryLiteral
			}
			var key string
			if p.text[p.pos] == '\'' || p.text[p.pos] == '"' {
				v, e := p.str()
				if e != nil {
					return nil, e
				}
				key = v
			} else {
				start := p.pos
				if !mercuryIdent(p.text[p.pos]) {
					return nil, errMercuryLiteral
				}
				p.pos++
				for p.pos < len(p.text) && mercuryIdentMore(p.text[p.pos]) {
					p.pos++
				}
				key = p.text[start:p.pos]
			}
			if _, ok := out[key]; ok || !p.take(':') {
				return nil, errMercuryLiteral
			}
			v, e := p.value(depth + 1)
			if e != nil {
				return nil, e
			}
			out[key] = v
			if p.take('}') {
				return out, nil
			}
			if !p.take(',') {
				return nil, errMercuryLiteral
			}
			if p.take('}') {
				return out, nil
			}
		}
	case '[':
		p.pos++
		out := []any{}
		if p.take(']') {
			return out, nil
		}
		for {
			v, e := p.value(depth + 1)
			if e != nil {
				return nil, e
			}
			out = append(out, v)
			if p.take(']') {
				return out, nil
			}
			if !p.take(',') {
				return nil, errMercuryLiteral
			}
			if p.take(']') {
				return out, nil
			}
		}
	case '\'', '"':
		return p.str()
	}
	start := p.pos
	for p.pos < len(p.text) && !strings.ContainsRune(" \t\r\n,;]}<", rune(p.text[p.pos])) {
		p.pos++
	}
	s := p.text[start:p.pos]
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n, e := strconv.ParseUint(s[2:], 16, 64)
		if e != nil {
			return nil, errMercuryLiteral
		}
		return json.Number(strconv.FormatUint(n, 10)), nil
	}
	if s == "" || !(s[0] >= '0' && s[0] <= '9' || s[0] == '-') {
		return nil, errMercuryLiteral
	}
	// json.Valid enforces the decimal number grammar; ParseFloat excludes overflow.
	if !json.Valid([]byte(s)) {
		return nil, errMercuryLiteral
	}
	f, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, errMercuryLiteral
	}
	return json.Number(s), nil
}
func (p *mercuryParser) hex(n int) (uint64, error) {
	if p.pos+n > len(p.text) {
		return 0, errMercuryLiteral
	}
	v, e := strconv.ParseUint(p.text[p.pos:p.pos+n], 16, 16)
	p.pos += n
	if e != nil {
		return 0, errMercuryLiteral
	}
	return v, nil
}
func (p *mercuryParser) str() (string, error) {
	quote := p.text[p.pos]
	p.pos++
	var b strings.Builder
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		p.pos++
		if b.Len() > 65536 {
			return "", errMercuryLiteral
		}
		if c == quote {
			return b.String(), nil
		}
		if c < 32 {
			return "", errMercuryLiteral
		}
		if c != '\\' {
			if c < utf8.RuneSelf {
				b.WriteByte(c)
			} else {
				p.pos--
				r, n := utf8.DecodeRuneInString(p.text[p.pos:])
				if r == utf8.RuneError && n == 1 {
					return "", errMercuryLiteral
				}
				p.pos += n
				b.WriteRune(r)
			}
			continue
		}
		if p.pos >= len(p.text) {
			return "", errMercuryLiteral
		}
		c = p.text[p.pos]
		p.pos++
		switch c {
		case '\\', '\'', '"', '/':
			b.WriteByte(c)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x':
			v, e := p.hex(2)
			if e != nil {
				return "", e
			}
			b.WriteRune(rune(v))
		case 'u':
			v, e := p.hex(4)
			if e != nil {
				return "", e
			}
			r := rune(v)
			if utf16.IsSurrogate(r) {
				if r > 0xdbff || p.pos+2 > len(p.text) || p.text[p.pos:p.pos+2] != "\\u" {
					return "", errMercuryLiteral
				}
				p.pos += 2
				low, e := p.hex(4)
				if e != nil || low < 0xdc00 || low > 0xdfff {
					return "", errMercuryLiteral
				}
				r = utf16.DecodeRune(r, rune(low))
			}
			b.WriteRune(r)
		default:
			return "", errMercuryLiteral
		}
	}
	return "", errMercuryLiteral
}
func mercuryObject(page, name string) (map[string]any, error) {
	v, e := mercuryLiteral(page, name)
	m, ok := v.(map[string]any)
	if e != nil || !ok {
		return nil, errMercuryLiteral
	}
	return m, nil
}
func mercuryUint(v any) (uint64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, errMercuryLiteral
	}
	u, e := strconv.ParseUint(string(n), 10, 64)
	if e != nil {
		return 0, errMercuryLiteral
	}
	return u, nil
}
func mercuryCount(v any, max int) (int, error) {
	u, e := mercuryUint(v)
	if e != nil || u > uint64(max) {
		return 0, errMercuryLiteral
	}
	return int(u), nil
}
func mercuryArray(m map[string]any, key string, n int) ([]any, error) {
	a, ok := m[key].([]any)
	if !ok || len(a) < n {
		return nil, errMercuryLiteral
	}
	return a, nil
}
func mercuryFirst(m map[string]any, key string) string {
	a, _ := m[key].([]any)
	if len(a) == 0 {
		return ""
	}
	s, _ := a[0].(string)
	return s
}
