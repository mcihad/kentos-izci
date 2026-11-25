package search

import "testing"

// FuzzParse feeds arbitrary queries to the parser: it returns a query or a
// syntax error, and terms point into the query.
func FuzzParse(f *testing.F) {
	for _, q := range []string{
		`is:unresolved level:error,fatal customer:"Sivas Belediyesi" !env:staging times_seen:>=100 first_seen:-24h "connection reset" timeout`,
		`"unterminated`, `level:`, `:value`, `!free`, `customer:"open`, `a:"b\"c" d:e,"f,g"`, `   `,
	} {
		f.Add(q)
	}
	f.Fuzz(func(t *testing.T, q string) {
		parsed, err := Parse(q)
		if err != nil {
			if _, ok := err.(*Error); !ok {
				t.Fatalf("error type %T", err)
			}
			return
		}
		for _, term := range parsed.Terms {
			if term.Pos < 0 || term.Pos > len(q) {
				t.Fatalf("term position %d outside %q", term.Pos, q)
			}
		}
		for pos := 0; pos <= len(q); pos++ {
			start, end := TermAt(q, pos)
			if start < 0 || end > len(q) || start > end {
				t.Fatalf("TermAt(%q, %d) = %d, %d", q, pos, start, end)
			}
		}
	})
}
