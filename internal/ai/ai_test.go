package ai

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func TestOpencodeConfig(t *testing.T) {
	var cfg struct {
		Permission map[string]json.RawMessage `json:"permission"`
		Formatter  bool                       `json:"formatter"`
		LSP        bool                       `json:"lsp"`
		Snapshot   bool                       `json:"snapshot"`
	}
	locked := opencodeConfig(nil)
	if err := json.Unmarshal([]byte(locked), &cfg); err != nil {
		t.Fatal(err)
	}
	if string(cfg.Permission["bash"]) != `"deny"` || string(cfg.Permission["webfetch"]) != `"deny"` ||
		string(cfg.Permission["external_directory"]) != `"deny"` || cfg.Formatter || cfg.LSP || cfg.Snapshot {
		t.Fatalf("default config is not locked down: %s", locked)
	}

	// opencode applies the last matching rule: the catch-all must come first.
	allowed := opencodeConfig([]string{"go test", " ", `make "lint"`})
	if err := json.Unmarshal([]byte(allowed), &cfg); err != nil {
		t.Fatal(err)
	}
	bash := string(cfg.Permission["bash"])
	if want := `{"*":"deny","go test*":"allow","make \"lint\"*":"allow"}`; bash != want {
		t.Fatalf("bash rules %s, want %s", bash, want)
	}
}

func TestEventParser(t *testing.T) {
	type entry struct {
		kind      int16
		tool, msg string
	}
	var got []entry
	var out agentOutcome
	p := &eventParser{dir: "/w/run", outcome: &out, emit: func(kind int16, tool, msg string) { got = append(got, entry{kind, tool, msg}) }}
	w := &lineWriter{max: 1 << 10, line: p.handle}
	stream := strings.Join([]string{
		`{"type":"step_start","part":{}}`,
		`{"type":"tool_use","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/w/run/internal/calc.go"}}}}`,
		`{"type":"tool_use","part":{"tool":"bash","state":{"status":"error","input":{"command":"rm -rf /"},"error":"denied"}}}`,
		`not json`,
		`{"type":"text","part":{"text":"  Root cause found.  "}}`,
		`{"type":"step_finish","part":{"cost":0.25,"tokens":{"input":100,"output":20,"reasoning":5,"cache":{"read":50,"write":10}}}}`,
		`{"type":"future_event","part":{"x":1}}`,
		`{"type":"error","error":{"name":"ProviderAuthError","data":{"message":"missing API key"}}}`,
	}, "\n")
	// Deliver in awkward chunks to exercise line assembly.
	for i := 0; i < len(stream); i += 7 {
		_, _ = w.Write([]byte(stream[i:min(i+7, len(stream))]))
	}
	w.flush()
	want := []entry{
		{LogTool, "read", "internal/calc.go"},
		{LogError, "bash", "rm -rf /: denied"},
		{LogText, "", "Root cause found."},
		{LogStep, "", "Adım tamamlandı · 160 giriş, 25 çıkış token"},
		{LogError, "", "missing API key"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if out.usage != (store.AiUsage{InputTokens: 160, OutputTokens: 25, CostUSD: 0.25}) || len(out.errors) != 1 {
		t.Fatalf("outcome %+v", out)
	}

	// Lines over the limit are dropped without breaking the next line.
	got = nil
	w = &lineWriter{max: 64, line: p.handle}
	_, _ = w.Write([]byte(`{"type":"text","part":{"text":"` + strings.Repeat("x", 100) + `"}}` + "\n"))
	_, _ = w.Write([]byte(`{"type":"text","part":{"text":"short"}}` + "\n"))
	if len(got) != 1 || got[0].msg != "short" {
		t.Fatalf("after a long line: %+v", got)
	}
}

func TestReadResults(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".izci"), 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, ".izci", name), []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write("result.json", `{"summary":" Sıfıra bölme ","confidence":85,"risk":"Yüksek","test_suggestions":["a","",  "b"]}`)
	analysis, res, found, err := readResults(dir)
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if res.Summary != "Sıfıra bölme" || res.Confidence != 0.85 || res.Risk != "high" || len(res.TestSuggestions) != 2 {
		t.Fatalf("result %+v", res)
	}
	if !strings.Contains(analysis, "## Özet") || !strings.Contains(analysis, "- b") {
		t.Fatalf("synthesized analysis %q", analysis)
	}

	// A symlink out of the checkout is not followed: the agent must not be
	// able to make izci publish files of the server.
	secret := filepath.Join(t.TempDir(), "secret.env")
	if err := os.WriteFile(secret, []byte("DATABASE_URL=postgres://secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dir, ".izci", "result.json"))
	if err := os.Symlink(secret, filepath.Join(dir, ".izci", "analysis.md")); err != nil {
		t.Fatal(err)
	}
	analysis, _, found, err = readResults(dir)
	if err != nil || found || strings.Contains(analysis, "secret") {
		t.Fatalf("symlink followed: found %v, analysis %q, err %v", found, analysis, err)
	}
}

func TestShareable(t *testing.T) {
	ev := &ingestv1.Event{
		User: &ingestv1.User{Id: "42", Email: "ayse@example.com", IpAddress: "10.0.0.1", Name: "Ayşe", Segment: "belediye"},
		Request: &ingestv1.Request{Url: "https://kalem.test/api", RemoteAddr: "10.0.0.1", Headers: []*ingestv1.Header{
			{Name: "Accept", Value: "application/json"}, {Name: "X-Forwarded-For", Value: "10.0.0.1"}, {Name: "Cookie", Value: "sid=1"}}},
	}
	out := shareable(ev)
	if out.GetUser().GetEmail() != "" || out.GetUser().GetIpAddress() != "" || out.GetUser().GetName() != "" ||
		out.GetUser().GetId() != "42" || out.GetUser().GetSegment() != "belediye" {
		t.Fatalf("user %v", out.GetUser())
	}
	if out.GetRequest().GetRemoteAddr() != "" || len(out.GetRequest().GetHeaders()) != 1 {
		t.Fatalf("request %v", out.GetRequest())
	}
	if ev.GetUser().GetEmail() == "" || len(ev.GetRequest().GetHeaders()) != 3 {
		t.Fatal("the original event was modified")
	}
}

func TestBranchNames(t *testing.T) {
	id := uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")
	got := defaultBranch("KALEM-7", "Çağrı sırasında sıfıra bölme: Divide()", id)
	if got != "izci/fix/kalem-7-cagri-sirasinda-sifira-bolme-divide-0199a1b2" || !validBranch(got) {
		t.Fatalf("default branch %q", got)
	}
	for _, bad := range []string{"", "-x", "a..b", "a b", "a~1", "x.lock", "a/.hidden", "a//b", "a@{1}", "a:b", "ends/"} {
		if validBranch(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"izci/fix/x", "feature/ABC-12_fix.v2"} {
		if !validBranch(good) {
			t.Errorf("%q rejected", good)
		}
	}
}

func TestDiffParsing(t *testing.T) {
	paths, status := nameStatus([]byte("M\x00calc.go\x00A\x00calc_test.go\x00D\x00old name.go\x00"))
	if strings.Join(paths, ",") != "calc.go,calc_test.go,old name.go" || status["calc_test.go"] != "added" || status["old name.go"] != "deleted" {
		t.Fatalf("name-status %v %v", paths, status)
	}
	counts := numstat([]byte("3\t1\tcalc.go\x00-\t-\tlogo.png\x00"))
	if counts["calc.go"] != [2]int{3, 1} || counts["logo.png"] != [2]int{0, 0} {
		t.Fatalf("numstat %v", counts)
	}
}

func TestWorkspaceIsAbsolute(t *testing.T) {
	// opencode resolves --dir against the checkout it runs in: a relative
	// IZCI_DATA_DIR must not leak into its arguments.
	r := New(nil, slog.New(slog.DiscardHandler), Options{DataDir: "./data"})
	if !filepath.IsAbs(r.ws.root) || !filepath.IsAbs(r.agent.home) || !filepath.IsAbs(r.ws.runDir(uuid.New())) {
		t.Fatalf("relative paths: %s, %s", r.ws.root, r.agent.home)
	}
}
