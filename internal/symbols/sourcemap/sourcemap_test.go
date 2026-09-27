package sourcemap

import (
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// position returns the zero-based line and column of the first occurrence
// of needle in the generated file. Columns count UTF-16 code units, like
// JavaScript engines and source maps.
func position(t *testing.T, src, needle string) (uint32, uint32) {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("%q not in generated code", needle)
	}
	line := strings.Count(src[:i], "\n")
	start := strings.LastIndex(src[:i], "\n") + 1
	return uint32(line), uint32(len(utf16.Encode([]rune(src[start:i]))))
}

// testdata/index.js and its map come from `vite build` (Vite 8, minified)
// of a two-module TypeScript app.
func TestViteBundle(t *testing.T) {
	js, _ := os.ReadFile("testdata/index.js")
	raw, _ := os.ReadFile("testdata/index.js.map")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.File != "index-BQVxLPGT.js" || len(m.Sources) != 2 || m.Tokens() == 0 {
		t.Fatalf("map: file=%q sources=%v tokens=%d", m.File, m.Sources, m.Tokens())
	}
	src := string(js)

	// throw new e(`Fatura satırı yok`) is line 15 of invoice.ts.
	line, col := position(t, src, "new e(`Fatura")
	tok, ok := m.Lookup(line, col)
	if !ok || m.Source(tok) != "../../src/invoice.ts" || tok.SrcLine != 14 {
		t.Fatalf("throw site: %+v %q", tok, m.Source(tok))
	}
	pre, ctx, post, ok := m.Context(tok, 3)
	if !ok || !strings.Contains(ctx, `throw new InvoiceError("Fatura satırı yok")`) || len(pre) != 3 || len(post) != 3 {
		t.Fatalf("context: %q %q %q", pre, ctx, post)
	}

	// The call t(e) in formatTotal names the callee: totalWithTax.
	line, col = position(t, src, "t(e);return")
	tok, ok = m.Lookup(line, col)
	if !ok || m.Name(tok) != "totalWithTax" || m.Source(tok) != "../../src/invoice.ts" || tok.SrcLine != 20 {
		t.Fatalf("call site: %+v name=%q", tok, m.Name(tok))
	}
	line, col = position(t, src, "i())")
	tok, _ = m.Lookup(line, col)
	if m.Name(tok) != "saveInvoice" || m.Source(tok) != "../../src/main.ts" {
		t.Fatalf("listener: %+v name=%q", tok, m.Name(tok))
	}
	if m.Ignored(tok) || !m.HasContent(tok) {
		t.Fatal("main.ts is application code with embedded content")
	}
	if _, ok := m.Lookup(0, 0); ok {
		// The module preload polyfill has no original source.
		t.Log("first token maps to a source")
	}
}

func TestDecodeMappings(t *testing.T) {
	// Two lines: "AAAA,EAAE" and "AACA,IAAIC" with a name.
	m, err := Parse([]byte(`{"version":3,"sources":["a.js"],"names":["foo"],"mappings":"AAAA,EAAE;AACA,IAAIA","sourcesContent":["x\ny\nz"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Token{
		{GenLine: 0, GenCol: 0, Source: 0, SrcLine: 0, SrcCol: 0, Name: -1},
		{GenLine: 0, GenCol: 2, Source: 0, SrcLine: 0, SrcCol: 2, Name: -1},
		{GenLine: 1, GenCol: 0, Source: 0, SrcLine: 1, SrcCol: 2, Name: -1},
		{GenLine: 1, GenCol: 4, Source: 0, SrcLine: 1, SrcCol: 6, Name: 0},
	}
	if len(m.tokens) != len(want) {
		t.Fatalf("tokens: %+v", m.tokens)
	}
	for i := range want {
		if m.tokens[i] != want[i] {
			t.Fatalf("token %d: %+v, want %+v", i, m.tokens[i], want[i])
		}
	}
	if tok, ok := m.Lookup(1, 9); !ok || m.Name(tok) != "foo" {
		t.Fatalf("lookup: %+v", tok)
	}
	// Past the end of line 0 the last token of the line still applies.
	if tok, ok := m.Lookup(0, 50); !ok || tok.GenCol != 2 {
		t.Fatalf("end of line: %+v", tok)
	}
	if _, line, _, ok := m.Context(Token{Source: 0, SrcLine: 1}, 5); !ok || line != "y" {
		t.Fatal("context")
	}
}

func TestVLQ(t *testing.T) {
	for in, want := range map[string]int64{"A": 0, "C": 1, "D": -1, "gB": 16, "hB": -16, "2H": 123, "+/////D": 2147483647} {
		v, next, err := vlq(in, 0)
		if err != nil || v != want || next != len(in) {
			t.Errorf("vlq(%q) = %d, %d, %v; want %d", in, v, next, err, want)
		}
	}
	for _, bad := range []string{"g", "!", "AA!"} {
		if _, err := decodeMappings(bad, 1, 0); err == nil {
			t.Errorf("decodeMappings(%q) accepted", bad)
		}
	}
}

func TestIndexMap(t *testing.T) {
	m, err := Parse([]byte(`{"version":3,"sections":[
		{"offset":{"line":0,"column":0},"map":{"version":3,"sources":["a.js"],"names":[],"mappings":"AAAA"}},
		{"offset":{"line":0,"column":100},"map":{"version":3,"sources":["b.js"],"names":["bar"],"mappings":"AACAA;AACA"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if tok, ok := m.Lookup(0, 120); !ok || m.Source(tok) != "b.js" || tok.SrcLine != 1 || m.Name(tok) != "bar" {
		t.Fatalf("section 2: %+v %q", tok, m.Source(tok))
	}
	if tok, ok := m.Lookup(0, 99); !ok || m.Source(tok) != "a.js" {
		t.Fatalf("section 1: %+v", tok)
	}
	if tok, ok := m.Lookup(1, 0); !ok || m.Source(tok) != "b.js" || tok.SrcLine != 2 {
		t.Fatalf("section 2 line 1: %+v", tok)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{`{"version":2,"mappings":""}`, `not json`, `{"version":3,"sources":[],"mappings":"AAAA"}`,
		`{"version":3,"sections":[{"offset":{"line":0,"column":0},"url":"x.map"}]}`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%s) accepted", in)
		}
	}
	if _, err := Parse([]byte(`{"version":2}`)); !errors.Is(err, ErrNotSourceMap) {
		t.Errorf("version 2: %v", err)
	}
	m, err := Parse([]byte(")]}'\n" + `{"version":3,"sourceRoot":"src","sources":["a.js"],"names":[],"mappings":"AAAA","x_google_ignoreList":[0],"debugId":"85314830-023F-4CF1-A267-535F4E37BB17"}`))
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := m.Lookup(0, 0)
	if m.Source(tok) != "src/a.js" || !m.Ignored(tok) || m.DebugID != "85314830-023F-4CF1-A267-535F4E37BB17" || m.HasContent(tok) {
		t.Fatalf("root/ignore/debug id: %q %v %q", m.Source(tok), m.Ignored(tok), m.DebugID)
	}
}

func TestResolve(t *testing.T) {
	for _, c := range []struct{ base, ref, want string }{
		{"https://x.test/assets/app.js", "app.js.map", "https://x.test/assets/app.js.map"},
		{"https://x.test/assets/app.js?v=2", "../../src/a.ts", "https://x.test/src/a.ts"},
		{"https://x.test/assets/app.js", "/maps/app.js.map", "https://x.test/maps/app.js.map"},
		{"~/assets/app.js", "../src/a.ts", "~/src/a.ts"},
		{"https://x.test/app.js", "webpack://kalem/./src/a.ts", "webpack://kalem/./src/a.ts"},
		{"assets/app.js", "../src/a.ts", "src/a.ts"},
		{"https://x.test", "a.js.map", "https://x.test/a.js.map"},
	} {
		if got := Resolve(c.base, c.ref); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.base, c.ref, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"webpack://kalem/./src/a.ts":        "src/a.ts",
		"webpack:///./src/a.ts":             "src/a.ts",
		"https://x.test/src/a.ts":           "src/a.ts",
		"../../node_modules/react/index.js": "node_modules/react/index.js",
		"~/src/a.ts":                        "src/a.ts",
		"src/a.ts":                          "src/a.ts",
	} {
		if got := DisplayPath(in); got != want {
			t.Errorf("DisplayPath(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"https://x.test/assets/app.js?v=1": "~/assets/app.js",
		"~/assets/app.js":                  "~/assets/app.js",
		"https://x.test":                   "",
		"app.js":                           "",
	} {
		if got := HostlessName(in); got != want {
			t.Errorf("HostlessName(%q) = %q, want %q", in, got, want)
		}
	}
}
