package devices

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMercuryLiteralSubset(t *testing.T) {
	page := `<script>var data={unquoted:'single \' quote',hex:0x1D7,list:[1,2,],nested:{ok:true,none:null,},unicode:'\uD83D\uDE00',esc:'\x41\n',};</script>`
	v, e := mercuryLiteral(page, "data")
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"unquoted": "single ' quote", "hex": json.Number("471"), "list": []any{json.Number("1"), json.Number("2")}, "nested": map[string]any{"ok": true, "none": nil}, "unicode": "😀", "esc": "A\n"}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got %#v want %#v", v, want)
	}
	// Exact uint64 integers survive parsing without a float conversion.
	v, e = mercuryLiteral(`var data=18446744073709551615;`, "data")
	if e != nil {
		t.Fatal(e)
	}
	u, e := mercuryUint(v)
	if e != nil || u != ^uint64(0) {
		t.Fatal("uint64 precision lost")
	}
}
func TestMercuryLiteralRejectsExecutableAndMalformedData(t *testing.T) {
	for _, literal := range []string{
		`function(){return 1}`, `(()=>1)()`, `new Array(1,2)`, `window.secret`, `[alert('x')]`, `{get value(){return 1}}`, `{a:1,a:2}`, `{['a']:1}`,
		`{a:1}+fetch('/leak')`, `{a:1}['a']`, `[,,]`, `[1,,2]`, `{a:undefined}`, `{a:NaN}`, `{a:Infinity}`, `{a:1e999}`, `{a:0x}`, `{a:01}`, `{a:-}`, `{a:1foo}`,
		`{'a':'unterminated}`, `{a:'\uD800'}`, `{a:'\uDC00'}`, `{a:'\uD800\u0000'}`, `{a:'\xGG'}`, `{a:'\q'}`, "{a:'line\nbreak'}", `{a:[1,2}`, `{a:1`,
	} {
		t.Run(literal, func(t *testing.T) {
			if v, e := mercuryLiteral("var data="+literal+";", "data"); e == nil {
				t.Fatalf("accepted %#v", v)
			}
		})
	}
}
func TestMercuryLiteralBounds(t *testing.T) {
	for name, literal := range map[string]string{
		"depth":  strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
		"nodes":  "[" + strings.Repeat("0,", 16385) + "]",
		"string": "'" + strings.Repeat("x", 65538) + "'",
		"body":   strings.Repeat(" ", mercuryBodyLimit) + "0",
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := mercuryLiteral("var data="+literal+";", "data"); e == nil {
				t.Fatal("bound not enforced")
			}
		})
	}
}
func TestMercuryEncodeCapturedFirmware(t *testing.T) {
	script := mercuryFixture(t, "crypto.js")
	encoded, e := mercuryEncode("test-pass", script)
	if e != nil {
		t.Fatal(e)
	}
	// Independently calculated from the public salt/alphabet in the capture.
	if encoded != "77VtUtxvyTefbwK" {
		t.Fatalf("encoding %q", encoded)
	}
	if _, e := mercuryEncode("密码", script); e == nil {
		t.Fatal("unsupported character encoding accepted")
	}
	if _, e := mercuryEncode("test-pass", `function hex_md5(c){return c}`); e == nil {
		t.Fatal("unknown firmware accepted")
	}
}
func FuzzMercuryLiteral(f *testing.F) {
	for _, seed := range []string{`{a:[1,0xff,'x',],}`, `[1]`, `'\uD800'`, `(()=>{})()`, `{a:1e999}`, `{a:1,a:2}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > mercuryBodyLimit+1 {
			return
		}
		_, _ = mercuryLiteral("var data="+input+";", "data")
	})
}
