package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGroup(t *testing.T) {
	for path, want := range map[string]string{
		"/izci.ingest.v1.IngestService/SendEvents": "ingest",
		"/izci.api.v1.IssueService/ListIssues":     "api",
		"/izci.api.v1.DashboardService/WatchLive":  "stream",
		"/izci.api.v1.AiService/WatchRun":          "stream",
		"/grpc.health.v1.Health/Check":             "grpc",
		"/grpc.health.v1.Health/Watch":             "stream",
		"/api/12/envelope/":                        "sentry",
		"/mcp":                                     "mcp",
		"/auth/oidc/kimlik/callback":               "auth",
		"/docs/raw/kurulum":                        "docs",
		"/llms.txt":                                "docs",
		"/healthz":                                 "health",
		"/metrics":                                 "metrics",
		"/telegram/webhook":                        "telegram",
		"/integrations/github/callback":            "integrations",
		"/o/kentos/issues/KALEM-1":                 "ui",
		"/assets/index-abc.js":                     "ui",
		"/":                                        "ui",
	} {
		if got := Group(path); got != want {
			t.Errorf("Group(%q) = %q, want %q", path, got, want)
		}
	}
}

func scrape(h http.Handler, remote string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.RemoteAddr = remote
	for k, v := range header {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandlerAccess(t *testing.T) {
	open := NewInstance().Handler("")
	for _, c := range []struct {
		remote string
		header http.Header
		want   int
	}{
		{"127.0.0.1:5000", nil, http.StatusOK},
		{"[::1]:5000", nil, http.StatusOK},
		{"10.0.3.7:5000", nil, http.StatusOK},
		{"172.18.0.4:5000", nil, http.StatusOK},
		{"[fd00::4]:5000", nil, http.StatusOK},
		{"[::ffff:192.168.1.9]:5000", nil, http.StatusOK},
		{"203.0.113.9:5000", nil, http.StatusForbidden},
		{"172.18.0.2:5000", http.Header{"X-Forwarded-For": {"203.0.113.9"}}, http.StatusForbidden},
		{"172.18.0.2:5000", http.Header{"Forwarded": {"for=203.0.113.9"}}, http.StatusForbidden},
		{"172.18.0.2:5000", http.Header{"X-Real-Ip": {"203.0.113.9"}}, http.StatusForbidden},
		{"garbage", nil, http.StatusForbidden},
	} {
		if got := scrape(open, c.remote, c.header).Code; got != c.want {
			t.Errorf("no token, %s %v: status %d, want %d", c.remote, c.header, got, c.want)
		}
	}

	guarded := NewInstance().Handler("s3cret-token")
	for _, c := range []struct {
		auth string
		want int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Basic s3cret-token", http.StatusUnauthorized},
		{"Bearer s3cret-token", http.StatusOK},
	} {
		// A token is required even from loopback, and suffices from anywhere.
		for _, remote := range []string{"127.0.0.1:5000", "203.0.113.9:5000"} {
			h := http.Header{}
			if c.auth != "" {
				h.Set("Authorization", c.auth)
			}
			if got := scrape(guarded, remote, h).Code; got != c.want {
				t.Errorf("token, %s %q: status %d, want %d", remote, c.auth, got, c.want)
			}
		}
	}
}

// TestExposition checks that the output is valid for Prometheus: one TYPE
// line per family, the samples of a family together and after it.
func TestExposition(t *testing.T) {
	inst := NewInstance()
	inst.Gauge("izci_ingest_queue_depth", func() float64 { return 7 })
	EventsAccepted.Add(3)
	SentryItemDropped("transaction")
	SentryItemDropped("something-new")
	RequestStarted()
	RequestFinished("/izci.api.v1.IssueService/ListIssues", 200, 30*time.Millisecond)
	RequestStarted()
	RequestFinished("/izci.api.v1.DashboardService/WatchLive", 200, time.Hour)
	AIRunFinished("succeeded", 2*time.Minute, 1200, 300, 0.05)

	w := scrape(inst.Handler(""), "127.0.0.1:1", nil)
	body := w.Body.String()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content type %q", ct)
	}
	for _, want := range []string{
		`izci_build_info{version=`,
		`izci_ingest_events_total{outcome="accepted"} `,
		`izci_ingest_queue_depth 7`,
		`izci_sentry_items_dropped_total{type="transaction"} `,
		`izci_sentry_items_dropped_total{type="other"} `,
		`izci_http_requests_total{handler="api",code="200"} `,
		`izci_http_requests_total{handler="stream",code="200"} `,
		`izci_http_request_duration_seconds_bucket{handler="api",le="0.05"} `,
		`izci_ai_runs_total{status="succeeded"} `,
		`izci_ai_tokens_total{kind="input"} `,
		"go_goroutines ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(body, `izci_http_request_duration_seconds_bucket{handler="stream"`) {
		t.Error("streaming calls must stay out of the duration histogram")
	}

	types := map[string]string{}
	done := map[string]bool{}
	current := ""
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if name, ok := strings.CutPrefix(line, "# TYPE "); ok {
			fam, typ, _ := strings.Cut(name, " ")
			if _, dup := types[fam]; dup {
				t.Errorf("second TYPE line for %s", fam)
			}
			types[fam] = typ
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, " ")
		name, _, _ = strings.Cut(name, "{")
		fam := name
		if _, ok := types[fam]; !ok {
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				if base, ok := strings.CutSuffix(name, suffix); ok && (types[base] == "histogram" || types[base] == "summary") {
					fam = base
				}
			}
		}
		if _, ok := types[fam]; !ok {
			t.Errorf("sample %q has no TYPE line", line)
		}
		if fam != current {
			if done[fam] {
				t.Errorf("samples of %s are not together", fam)
			}
			done[current] = true
			current = fam
		}
	}
}
