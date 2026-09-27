package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mcihad/kentos-izci/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Env:       config.EnvTest,
		PublicURL: "https://izci.example.com",
		HTTP:      config.HTTPConfig{Addr: "127.0.0.1:0"},
	}
}

func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><title>izci</title>")},
		"assets/app-3f2a1c.js":    {Data: []byte("console.log('app')")},
		"assets/app-3f2a1c.js.br": {Data: []byte("BROTLI")},
		"assets/app-3f2a1c.js.gz": {Data: []byte("GZIP")},
		"favicon.svg":             {Data: []byte("<svg/>")},
		".gitkeep":                {Data: nil},
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Options{Config: testConfig(), Logger: slog.New(slog.DiscardHandler), UI: testUI()})
}

func do(t *testing.T, h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	h := newTestServer(t).Handler()
	rec := do(t, h, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("every response must carry X-Request-Id")
	}
	rec = do(t, h, http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz without db: %d", rec.Code)
	}
}

func TestRequestIDPropagation(t *testing.T) {
	h := newTestServer(t).Handler()
	rec := do(t, h, http.MethodGet, "/healthz", map[string]string{"X-Request-Id": "abc-123"})
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Fatalf("valid incoming request id must be kept, got %q", got)
	}
	rec = do(t, h, http.MethodGet, "/healthz", map[string]string{"X-Request-Id": "bad id <script>"})
	if got := rec.Header().Get("X-Request-Id"); got == "bad id <script>" || got == "" {
		t.Fatalf("invalid request id must be replaced, got %q", got)
	}
}

func TestSPARouting(t *testing.T) {
	h := newTestServer(t).Handler()

	rec := do(t, h, http.MethodGet, "/o/kentos/issues", map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>izci</title>") {
		t.Fatalf("client route must render index.html: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index.html must be no-cache, got %q", rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("documents must carry the CSP")
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("https public URL must enable HSTS")
	}

	rec = do(t, h, http.MethodGet, "/assets/missing.js", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset must 404, got %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/.gitkeep", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf(".gitkeep must not be served, got %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/o/kentos", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to the SPA must be 405, got %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/api/0/organizations/kentos/releases/1.0/previous-with-commits/", nil)
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unknown API paths must be JSON 404s, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestSPAAssetsPrecompressedAndCached(t *testing.T) {
	h := newTestServer(t).Handler()

	rec := do(t, h, http.MethodGet, "/assets/app-3f2a1c.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})
	if rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != "BROTLI" {
		t.Fatalf("brotli variant expected, got encoding=%q body=%q", rec.Header().Get("Content-Encoding"), rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("hashed assets must be immutable, got %q", rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("content type must come from the original extension, got %q", rec.Header().Get("Content-Type"))
	}

	rec = do(t, h, http.MethodGet, "/assets/app-3f2a1c.js", map[string]string{"Accept-Encoding": "gzip, br;q=0"})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("br;q=0 must fall back to gzip, got %q", rec.Header().Get("Content-Encoding"))
	}

	rec = do(t, h, http.MethodGet, "/assets/app-3f2a1c.js", nil)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("no Accept-Encoding must serve identity, got %q", rec.Header().Get("Content-Encoding"))
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "console.log('app')" {
		t.Fatalf("identity body mismatch: %q", body)
	}

	etag := rec.Header().Get("ETag")
	rec = do(t, h, http.MethodGet, "/assets/app-3f2a1c.js", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("matching ETag must return 304, got %d", rec.Code)
	}
}

func TestSPAPlaceholderWithoutUI(t *testing.T) {
	s := New(Options{Config: testConfig(), Logger: slog.New(slog.DiscardHandler)})
	rec := do(t, s.Handler(), http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "make web") {
		t.Fatalf("placeholder expected, got %d %s", rec.Code, rec.Body)
	}
}

type recordHandler struct {
	slog.Handler
	records *[]slog.Record
}

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}

func explode(xs []int, i int) int { return xs[i] }

func TestPanicIsLoggedWithItsStack(t *testing.T) {
	var records []slog.Record
	s := New(Options{Config: testConfig(), Logger: slog.New(recordHandler{slog.DiscardHandler, &records}), UI: testUI()})
	s.Handle("GET /boom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = explode(nil, 3)
	}))
	if rec := do(t, s.Handler(), http.MethodGet, "/boom", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	var pe *PanicError
	for _, r := range records {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				pe, _ = a.Value.Any().(*PanicError)
			}
			return true
		})
	}
	if pe == nil {
		t.Fatal("the panic was not logged as a *PanicError")
	}
	var rt runtime.Error
	if !errors.As(pe, &rt) || !strings.Contains(pe.Error(), "index out of range") {
		t.Fatalf("panic error %q does not wrap the runtime error", pe)
	}
	frames := runtime.CallersFrames(pe.Callers())
	top, _ := frames.Next()
	if !strings.HasSuffix(top.Function, ".explode") {
		t.Fatalf("innermost frame is %s, want the function that panicked", top.Function)
	}
}
