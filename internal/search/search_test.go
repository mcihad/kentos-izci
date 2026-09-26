package search

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	q, err := Parse(`is:unresolved level:error,fatal customer:"Sivas Belediyesi" !env:staging times_seen:>=100 first_seen:-24h "connection reset" timeout`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Term{
		{Key: "is", Values: []string{"unresolved"}},
		{Key: "level", Values: []string{"error", "fatal"}},
		{Key: "customer", Values: []string{"Sivas Belediyesi"}},
		{Key: "environment", Values: []string{"staging"}, Negate: true},
		{Key: "times_seen", Op: OpGe, Values: []string{"100"}},
		{Key: "first_seen", Values: []string{"-24h"}},
	}
	for i := range q.Terms {
		q.Terms[i].Pos = 0
	}
	if !reflect.DeepEqual(q.Terms, want) {
		t.Errorf("terms:\n got %+v\nwant %+v", q.Terms, want)
	}
	if !reflect.DeepEqual(q.Text, []string{"connection reset", "timeout"}) {
		t.Errorf("text: %v", q.Text)
	}
	if !q.Has("customer") || q.Has("release") {
		t.Error("Has")
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{`"unterminated`, `level:`, `:value`, `!free`, `customer:"open`} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) must fail", bad)
		} else if _, ok := err.(*Error); !ok {
			t.Errorf("Parse(%q) error type %T", bad, err)
		}
	}
	if q, err := Parse("   "); err != nil || len(q.Terms)+len(q.Text) != 0 {
		t.Errorf("blank query: %v %v", q, err)
	}
}

func TestTermAt(t *testing.T) {
	q := "is:unresolved cust"
	if s, e := TermAt(q, len(q)); q[s:e] != "cust" {
		t.Errorf("TermAt end = %q", q[s:e])
	}
	if s, e := TermAt(q, 3); q[s:e] != "is:unresolved" {
		t.Errorf("TermAt middle = %q", q[s:e])
	}
}
