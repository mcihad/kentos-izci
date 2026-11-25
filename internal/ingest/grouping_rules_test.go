package ingest

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func mustRules(t *testing.T, fingerprint, stacktrace string) *GroupingRules {
	t.Helper()
	r, errs := ParseGroupingRules(fingerprint, stacktrace)
	if len(errs) > 0 {
		t.Fatalf("rules: %v", errs)
	}
	return r
}

func TestParseGroupingRulesReportsLines(t *testing.T) {
	_, errs := ParseGroupingRules(`
# yorum
error.type:TimeoutException -> zaman-asimi
error.type:Foo
nope:x -> y
error.type:"unterminated -> y
message:x -> {{ bilinmez }}
message:x -> a-{{ default }}
message:x -> ,
`, `
stack.function:*Log* -group
stack.module:Kalem.*
stack.function:x +grup
error.type:X -group
`)
	got := map[string][]int{}
	for _, e := range errs {
		got[e.Field] = append(got[e.Field], e.Line)
	}
	if !slices.Equal(got["fingerprint_rules"], []int{4, 5, 6, 7, 8, 9}) || !slices.Equal(got["stacktrace_rules"], []int{3, 4, 5}) {
		t.Fatalf("problems: %v", errs)
	}
}

func TestFingerprintRules(t *testing.T) {
	rules := mustRules(t, `
error.type:*TimeoutException transaction:/api/odeme* -> odeme-zaman-asimi, {{ transaction }}
error.value:"*bağlantı havuzu*" -> baglanti-havuzu
tags.modul:imar !level:warning -> {{ default }}, imar
message:"Rapor * oluşturulamadı" -> rapor
`, "")
	timeout := func(txn string, line uint32) *ingestv1.Event {
		ev := excEvent("System.TimeoutException", "deadline", frame("Kalem.Odeme", "Pay", "Pay.cs", line, true))
		ev.Transaction = txn
		return ev
	}
	a, b := ComputeGroupingWith(timeout("/api/odeme/kart", 10), "dotnet", rules), ComputeGroupingWith(timeout("/api/odeme/kart", 99), "dotnet", rules)
	if a.Strategy != StrategyRule || a.Hash != b.Hash || !strings.HasPrefix(a.Rule, "error.type:*TimeoutException") ||
		!slices.Equal(a.Components, []string{"rule:odeme-zaman-asimi", "rule:/api/odeme/kart"}) {
		t.Fatalf("timeout: %+v", a)
	}
	if c := ComputeGroupingWith(timeout("/api/odeme/havale", 10), "dotnet", rules); c.Hash == a.Hash {
		t.Fatal("{{ transaction }} must split by transaction")
	}
	if c := ComputeGroupingWith(timeout("/api/rapor", 10), "dotnet", rules); c.Strategy != StrategyStacktrace {
		t.Fatalf("every matcher must match: %+v", c)
	}
	// A rule wins over the SDK's fingerprint; values match case-insensitively.
	pool := excEvent("NpgsqlException", "Bağlantı HAVUZU doldu")
	pool.Fingerprint = []string{"sdk"}
	if g := ComputeGroupingWith(pool, "dotnet", rules); !slices.Equal(g.Components, []string{"rule:baglanti-havuzu"}) {
		t.Fatalf("pool: %+v", g)
	}
	// {{ default }} keeps the default grouping and adds to it; negation.
	imar := excEvent("ArgumentException", "x", frame("Kalem.Imar", "Ruhsat", "Ruhsat.cs", 1, true))
	imar.Tags = map[string]string{"modul": "imar"}
	imar.Level = ingestv1.Level_LEVEL_ERROR
	plain := ComputeGrouping(imar, "dotnet")
	g := ComputeGroupingWith(imar, "dotnet", rules)
	if !slices.Equal(g.Components, append(slices.Clone(plain.Components), "rule:imar")) {
		t.Fatalf("default: %+v / %+v", g, plain)
	}
	imar.Level = ingestv1.Level_LEVEL_WARNING
	if g := ComputeGroupingWith(imar, "dotnet", rules); g.Strategy == StrategyRule {
		t.Fatalf("negated matcher: %+v", g)
	}
	msg := &ingestv1.Event{Message: &ingestv1.Message{Formatted: "Rapor 2024-05 oluşturulamadı"}}
	if g := ComputeGroupingWith(msg, "dotnet", rules); g.Strategy != StrategyRule {
		t.Fatalf("message glob: %+v", g)
	}
}

func TestStacktraceRules(t *testing.T) {
	logger := frame("Kalem.Ortak", "Logger.Error", "Logger.cs", 5, true)
	service := frame("Kalem.Siparis", "Siparis.Olustur", "Siparis.cs", 42, true)
	lib := frame("Polly", "Policy.Execute", "Policy.cs", 1, false)
	ev := func() *ingestv1.Event {
		return excEvent("InvalidOperationException", "x", clone(lib), clone(service), clone(logger))
	}
	base := ComputeGrouping(ev(), "dotnet")
	rules := mustRules(t, "", "stack.function:*Logger.* -group\nstack.module:Polly +group")
	g := ComputeGroupingWith(ev(), "dotnet", rules)
	want := []string{"type:InvalidOperationException", "frame:Polly|Policy.cs|Policy.Execute", "frame:Kalem.Siparis|Siparis.cs|Siparis.Olustur"}
	if !slices.Equal(g.Components, want) || g.Hash == base.Hash {
		t.Fatalf("components: %v", g.Components)
	}
	// +app and -app override the SDK while frames are normalized.
	e := ev()
	r := mustRules(t, "", "stack.module:Kalem.Ortak -app\nstack.module:Polly* +app")
	resolveInApp(e.GetExceptions()[0].GetStacktrace().GetFrames(), "dotnet", InAppRules{Stack: r})
	frames := e.GetExceptions()[0].GetStacktrace().GetFrames()
	if !frames[0].GetInApp() || frames[2].GetInApp() {
		t.Fatalf("in-app: %v %v", frames[0].GetInApp(), frames[2].GetInApp())
	}
}

func clone(f *ingestv1.Frame) *ingestv1.Frame {
	return proto.CloneOf(f)
}
