package symbols

import (
	"slices"
	"strings"
	"testing"
)

func TestCandidates(t *testing.T) {
	for url, want := range map[string][]string{
		"https://kalem.example.com/assets/app.js?v=2": {"https://kalem.example.com/assets/app.js", "~/assets/app.js"},
		"app:///dist/server.js":                       {"app:///dist/server.js", "~/dist/server.js"},
		"/app/dist/server.js":                         {"/app/dist/server.js", "~/app/dist/server.js"},
		"~/assets/app.js":                             {"~/assets/app.js"},
		"":                                            nil,
	} {
		if got := candidates(url); !slices.Equal(got, want) {
			t.Errorf("candidates(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestSizedCache(t *testing.T) {
	c := newSizedCache(100)
	c.add("a", 1, 40)
	c.add("b", 2, 40)
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.add("c", 3, 40) // evicts b, the least recently used
	if _, ok := c.get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := c.get("a"); !ok || c.bytes() != 80 {
		t.Fatalf("a missing or wrong size %d", c.bytes())
	}
	c.add("huge", 4, 1000) // larger than the cache: not kept
	if _, ok := c.get("huge"); ok || c.bytes() != 80 {
		t.Fatal("oversized value kept")
	}
}

func TestSafeParse(t *testing.T) {
	_, _, err := safeParse(func([]byte) (any, int, error) { panic("index out of range") }, nil)
	if err == nil || !strings.Contains(err.Error(), "index out of range") {
		t.Fatalf("panic: %v", err)
	}
	v, n, err := safeParse(func(b []byte) (any, int, error) { return string(b), len(b), nil }, []byte("ok"))
	if err != nil || v != "ok" || n != 2 {
		t.Fatalf("parse: %v %d %v", v, n, err)
	}
}
