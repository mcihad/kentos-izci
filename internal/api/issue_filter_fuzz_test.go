package api

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/search"
)

var placeholder = regexp.MustCompile(`\$(\d+)`)

// FuzzSearchSQL checks that search queries become SQL whose values are all
// parameters: user text never reaches the SQL itself, and every
// placeholder has its argument.
func FuzzSearchSQL(f *testing.F) {
	for _, q := range []string{
		`is:unresolved level:error,fatal customer:"Sivas Belediyesi" !env:staging times_seen:>=100 first_seen:-24h "connection reset" timeout`,
		`assigned:me project:kalem type:"Null; DROP TABLE issues" file:*.cs has:ai priority:>=high`,
		`release:"1.0\"; --" "x;y" tags.foo:"bar;baz" user:a\b level:<warning`,
	} {
		f.Add(q)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, text string) {
		q, err := search.Parse(text)
		if err != nil {
			return
		}
		// The SQL izci writes contains none of these characters, so text
		// with them that shows up in the SQL came from the query.
		var user []string
		keep := func(v string) {
			if len(v) >= 2 && strings.ContainsAny(v, `;"\`) {
				user = append(user, v)
			}
		}
		for _, v := range q.Text {
			keep(v)
		}
		for _, term := range q.Terms {
			keep(term.Key)
			for _, v := range term.Values {
				keep(v)
			}
		}
		for _, build := range []func(*sqlb) error{
			func(b *sqlb) error { return issueFilter(b, q, uuid.Nil, uuid.Nil, now) },
			func(b *sqlb) error { return eventFilter(b, q) },
		} {
			b := &sqlb{}
			if build(b) != nil {
				continue
			}
			sql := b.clause()
			for _, m := range placeholder.FindAllStringSubmatch(sql, -1) {
				if n, _ := strconv.Atoi(m[1]); n < 1 || n > len(b.args) {
					t.Fatalf("placeholder $%d with %d arguments: %s", n, len(b.args), sql)
				}
			}
			for _, v := range user {
				if strings.Contains(sql, v) {
					t.Fatalf("query text %q reached the SQL: %s", v, sql)
				}
			}
		}
	})
}
