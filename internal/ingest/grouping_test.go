package ingest

import (
	"testing"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func ptr[T any](v T) *T { return &v }

func frame(module, fn, file string, line uint32, inApp bool) *ingestv1.Frame {
	return &ingestv1.Frame{Module: module, Function: fn, Filename: file, Lineno: line, InApp: ptr(inApp)}
}

func excEvent(typ, value string, frames ...*ingestv1.Frame) *ingestv1.Event {
	return &ingestv1.Event{Exceptions: []*ingestv1.Exception{{
		Type: typ, Value: value, Stacktrace: &ingestv1.Stacktrace{Frames: frames},
	}}}
}

func TestGroupingStableAcrossLinesAndValues(t *testing.T) {
	a := excEvent("System.NullReferenceException", "Object reference not set (order 41)",
		frame("System.Private.CoreLib", "Invoke", "", 0, false),
		frame("KentOS.Kalem.Services", "OrderService.CreateOrder", "/build/src/Services/OrderService.cs", 142, true))
	b := excEvent("System.NullReferenceException", "Object reference not set (order 97)",
		frame("System.Private.CoreLib", "Invoke", "", 0, false),
		frame("KentOS.Kalem.Services", "OrderService.CreateOrder", "/home/runner/src/Services/OrderService.cs", 151, true))
	ga, gb := ComputeGrouping(a, "dotnet"), ComputeGrouping(b, "dotnet")
	if ga.Hash != gb.Hash {
		t.Fatalf("line numbers, build paths and message values must not split groups:\n%v\n%v", ga.Components, gb.Components)
	}
	if ga.Strategy != StrategyStacktrace {
		t.Errorf("strategy = %s", ga.Strategy)
	}
	for _, c := range ga.Components {
		if c == "frame:System.Private.CoreLib||Invoke" {
			t.Error("non-in-app frames must be ignored when in-app frames exist")
		}
	}

	c := excEvent("System.NullReferenceException", "x",
		frame("KentOS.Kalem.Services", "OrderService.CancelOrder", "OrderService.cs", 142, true))
	if ComputeGrouping(c, "dotnet").Hash == ga.Hash {
		t.Fatal("a different function must produce a different group")
	}
}

func TestGroupingNormalizesCompilerNames(t *testing.T) {
	cases := []struct {
		platform, a, b string
	}{
		{"go", "github.com/x/app/orders.(*Service).Create.func1", "github.com/x/app/orders.(*Service).Create.func2.1"},
		{"dotnet", "KentOS.Orders.<CreateAsync>d__5.MoveNext", "KentOS.Orders.<CreateAsync>d__12.MoveNext"},
		{"dotnet", "KentOS.Orders.<>c__DisplayClass3_0.<Handle>b__0", "KentOS.Orders.<>c__DisplayClass7_1.<Handle>b__2_1"},
		{"java", "com.kentos.Orders.lambda$create$0", "com.kentos.Orders.lambda$create$3"},
	}
	for _, c := range cases {
		fa, fb := normalizeFunction(c.a, c.platform), normalizeFunction(c.b, c.platform)
		if fa != fb {
			t.Errorf("%s: %q → %q, %q → %q must be equal", c.platform, c.a, fa, c.b, fb)
		}
	}
	if got := normalizeFunction("github.com/x/app/orders.(*Service).Create.func1", "go"); got != "github.com/x/app/orders.(*Service).Create" {
		t.Errorf("go closure: %q", got)
	}
	if got := normalizeFunction("KentOS.Orders.<CreateAsync>d__5.MoveNext", "dotnet"); got != "KentOS.Orders.CreateAsync" {
		t.Errorf("dotnet async: %q", got)
	}
	if normalizeFunction("e", "javascript") != "" {
		t.Error("minified JS names must be dropped")
	}
}

func TestNormalizeFilename(t *testing.T) {
	cases := map[[2]string]string{
		{"", "https://app.example.com/assets/index-CeX-bC8z.js?v=3"}:      "index.js",
		{"", "https://cdn.example.com/static/js/main.3f2a1c9d.chunk.js"}:  "main.chunk.js",
		{"internal/orders/service.go", "/app/internal/orders/service.go"}: "internal/orders/service.go",
		{`C:\build\src\Services\OrderService.cs`, ""}:                     "OrderService.cs",
		{"src/components/order-list.tsx", ""}:                             "src/components/order-list.tsx",
	}
	for in, want := range cases {
		if got := normalizeFilename(in[0], in[1]); got != want {
			t.Errorf("normalizeFilename(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestNormalizeMessage(t *testing.T) {
	a := normalizeMessage("Timeout after 30000ms calling http://svc:8080/api/users/42 for ali@example.com at 2026-09-26T10:00:00Z (req 6f1c1b5e-2c3d-4f6a-9b8e-0d1e2f3a4b5c, 10.0.4.17)")
	b := normalizeMessage("Timeout after 12000ms calling http://svc:8080/api/users/7 for veli@example.org at 2026-09-27T11:30:00Z (req 7a2d3c4b-1111-4f6a-9b8e-0d1e2f3a4b5c, 10.0.4.18)")
	if a != b {
		t.Fatalf("volatile tokens must be normalized:\n%s\n%s", a, b)
	}
	if got := normalizeMessage("user 42 not found"); got != "user <num> not found" {
		t.Errorf("got %q", got)
	}
	if got := normalizeMessage("hash deadbeefcafe1234 mismatch, code abc"); got != "hash <hex> mismatch, code abc" {
		t.Errorf("got %q", got)
	}
}

func TestGroupingStrategies(t *testing.T) {
	noFrames1 := &ingestv1.Event{Exceptions: []*ingestv1.Exception{{Type: "TimeoutError", Value: "upstream timed out after 30s"}}}
	noFrames2 := &ingestv1.Event{Exceptions: []*ingestv1.Exception{{Type: "TimeoutError", Value: "upstream timed out after 45s"}}}
	g1, g2 := ComputeGrouping(noFrames1, "node"), ComputeGrouping(noFrames2, "node")
	if g1.Strategy != StrategyException || g1.Hash != g2.Hash {
		t.Errorf("exception strategy: %v %v", g1, g2)
	}

	m1 := &ingestv1.Event{Message: &ingestv1.Message{Formatted: "User 42 not found", Template: "User {Id} not found"}}
	m2 := &ingestv1.Event{Message: &ingestv1.Message{Formatted: "User 43 not found", Template: "User {Id} not found"}}
	if ComputeGrouping(m1, "dotnet").Hash != ComputeGrouping(m2, "dotnet").Hash {
		t.Error("message templates must group")
	}

	base := excEvent("E", "x", frame("m", "f", "a.go", 1, true))
	custom := excEvent("E", "x", frame("m", "f", "a.go", 1, true))
	custom.Fingerprint = []string{"{{ default }}", "tenant-a"}
	gd, gc := ComputeGrouping(base, "go"), ComputeGrouping(custom, "go")
	if gc.Strategy != StrategyCustom || gc.Hash == gd.Hash {
		t.Errorf("custom fingerprint must split the default group: %v", gc)
	}
	if gc.Components[len(gc.Components)-1] != "custom:tenant-a" {
		t.Errorf("components: %v", gc.Components)
	}
	fixed := &ingestv1.Event{Fingerprint: []string{"db-down"}, Message: &ingestv1.Message{Formatted: "a"}}
	fixed2 := &ingestv1.Event{Fingerprint: []string{"db-down"}, Message: &ingestv1.Message{Formatted: "b"}}
	if ComputeGrouping(fixed, "go").Hash != ComputeGrouping(fixed2, "go").Hash {
		t.Error("identical custom fingerprints must group regardless of content")
	}
}
