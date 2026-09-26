// Package search parses the izci search language used by the issue list,
// the MCP tools and saved views:
//
//	is:unresolved level:error,fatal customer:sivas-belediyesi release:1.4.2
//	!environment:staging first_seen:-24h times_seen:>100 "connection reset"
//
// A query is a sequence of terms separated by whitespace. A term is either
// free text (optionally quoted) or key:value. Values may be quoted, may list
// alternatives separated by commas, and may carry a comparison operator
// (>, >=, <, <=) or a relative time (-24h = within the last 24 hours, +7d =
// longer ago than 7 days). A leading "!" negates a key term.
package search

import (
	"fmt"
	"strings"
	"unicode"
)

// Op is a comparison operator.
type Op string

// Comparison operators.
const (
	OpEq Op = ""
	OpGt Op = ">"
	OpGe Op = ">="
	OpLt Op = "<"
	OpLe Op = "<="
)

// Term is one key:value filter.
type Term struct {
	Key    string
	Op     Op
	Values []string
	Negate bool
	// Pos is the byte offset of the term in the query (for error messages).
	Pos int
}

// Query is a parsed search.
type Query struct {
	Terms []Term
	Text  []string
}

// Error describes a syntax error at a position.
type Error struct {
	Pos   int
	Token string
	Msg   string
}

func (e *Error) Error() string { return fmt.Sprintf("%s at position %d (%q)", e.Msg, e.Pos, e.Token) }

// aliases map alternative key spellings to canonical keys.
var aliases = map[string]string{
	"env": "environment", "firstseen": "first_seen", "lastseen": "last_seen", "timesseen": "times_seen",
	"assignee": "assigned", "server_name": "server", "error.type": "type", "filename": "file",
}

// Parse parses a query.
func Parse(q string) (*Query, error) {
	out := &Query{}
	i := 0
	for i < len(q) {
		for i < len(q) && unicode.IsSpace(rune(q[i])) {
			i++
		}
		if i >= len(q) {
			break
		}
		start := i
		negate := false
		if q[i] == '!' {
			negate = true
			i++
		}
		if i < len(q) && q[i] == '"' {
			text, next, err := quoted(q, i)
			if err != nil {
				return nil, err
			}
			if negate {
				return nil, &Error{Pos: start, Token: q[start:next], Msg: "free text cannot be negated"}
			}
			if text != "" {
				out.Text = append(out.Text, text)
			}
			i = next
			continue
		}
		// Read a word up to whitespace, honoring a quoted value after ':'.
		j := i
		for j < len(q) && !unicode.IsSpace(rune(q[j])) && q[j] != ':' && q[j] != '"' {
			j++
		}
		if j < len(q) && q[j] == ':' {
			key := strings.ToLower(q[i:j])
			if key == "" {
				return nil, &Error{Pos: start, Token: q[start : j+1], Msg: "missing key before ':'"}
			}
			if a, ok := aliases[key]; ok {
				key = a
			}
			j++
			var raw string
			if j < len(q) && q[j] == '"' {
				text, next, err := quoted(q, j)
				if err != nil {
					return nil, err
				}
				raw, j = text, next
				out.Terms = append(out.Terms, Term{Key: key, Values: []string{raw}, Negate: negate, Pos: start})
				i = j
				continue
			}
			k := j
			for k < len(q) && !unicode.IsSpace(rune(q[k])) {
				k++
			}
			raw = q[j:k]
			if raw == "" {
				return nil, &Error{Pos: start, Token: q[start:k], Msg: "missing value after ':'"}
			}
			term := Term{Key: key, Negate: negate, Pos: start}
			for _, op := range []Op{OpGe, OpLe, OpGt, OpLt} {
				if strings.HasPrefix(raw, string(op)) {
					term.Op, raw = op, raw[len(op):]
					break
				}
			}
			for _, v := range strings.Split(raw, ",") {
				if v = strings.TrimSpace(v); v != "" {
					term.Values = append(term.Values, v)
				}
			}
			if len(term.Values) == 0 {
				return nil, &Error{Pos: start, Token: q[start:k], Msg: "missing value after ':'"}
			}
			out.Terms = append(out.Terms, term)
			i = k
			continue
		}
		if negate {
			return nil, &Error{Pos: start, Token: q[start:j], Msg: "free text cannot be negated"}
		}
		if j == i {
			j++ // a stray quote
		}
		out.Text = append(out.Text, q[i:j])
		i = j
	}
	return out, nil
}

func quoted(q string, i int) (string, int, error) {
	var b strings.Builder
	j := i + 1
	for j < len(q) {
		switch q[j] {
		case '\\':
			if j+1 < len(q) {
				b.WriteByte(q[j+1])
				j += 2
				continue
			}
		case '"':
			return b.String(), j + 1, nil
		}
		b.WriteByte(q[j])
		j++
	}
	return "", 0, &Error{Pos: i, Token: q[i:], Msg: "unterminated quote"}
}

// Has reports whether the query filters on key.
func (q *Query) Has(key string) bool {
	for _, t := range q.Terms {
		if t.Key == key {
			return true
		}
	}
	return false
}

// TermAt returns the term (or free-text word) around cursor position pos as
// [start, end) byte offsets, for autocompletion.
func TermAt(q string, pos int) (start, end int) {
	if pos > len(q) {
		pos = len(q)
	}
	start = pos
	for start > 0 && !unicode.IsSpace(rune(q[start-1])) {
		start--
	}
	end = pos
	for end < len(q) && !unicode.IsSpace(rune(q[end])) {
		end++
	}
	return start, end
}
