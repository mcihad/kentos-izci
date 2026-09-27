package sentry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	sentrygo "github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
)

func newEnv(t *testing.T, rate int32) *ingesttest.Env {
	return ingesttest.New(t, ingesttest.Options{
		KeyRateLimit:   rate,
		AllowedOrigins: []string{"https://kalem.example.com"},
		Mount: func(mux *http.ServeMux, svc *ingest.Service) {
			sentry.NewHandler(svc, slog.New(slog.DiscardHandler)).Register(mux.Handle)
		},
	})
}

func (s storedEvent) String() string { return fmt.Sprintf("%+v", map[string]any(s)) }

type storedEvent map[string]any

// lastEvent returns the payload of the most recent stored event.
func lastEvent(t *testing.T, e *ingesttest.Env) (storedEvent, map[string]string) {
	t.Helper()
	ctx := context.Background()
	var payload []byte
	cols := map[string]string{}
	var customer, tenant, browser, platform, ip string
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload, customer_key, tenant_key, browser, platform, coalesce(host(client_ip), '')
			FROM events ORDER BY received_at DESC LIMIT 1`).Scan(&payload, &customer, &tenant, &browser, &platform, &ip)
	})
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	cols["customer"], cols["tenant"], cols["browser"], cols["platform"], cols["ip"] = customer, tenant, browser, platform, ip
	var m storedEvent
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	return m, cols
}

func TestRealSentryGoSDK(t *testing.T) {
	e := newEnv(t, 0)
	host := strings.TrimPrefix(e.Server.URL, "http://")
	dsn := fmt.Sprintf("http://%s@%s/%d", e.Key, host, e.Project.PublicID)

	client, err := sentrygo.NewClient(sentrygo.ClientOptions{
		Dsn: dsn, Environment: "staging", Release: "kalem@2.1.0", AttachStacktrace: true,
	})
	if err != nil {
		t.Fatalf("sentry client: %v", err)
	}
	hub := sentrygo.NewHub(client, sentrygo.NewScope())
	hub.ConfigureScope(func(s *sentrygo.Scope) {
		s.SetTag("customer", "sivas-belediyesi")
		s.SetTag("customer_name", "Sivas Belediyesi")
		s.SetTag("tenant", "t-7")
		s.SetUser(sentrygo.User{ID: "42", Email: "ayse@example.com"})
	})
	id := hub.CaptureException(fmt.Errorf("approve order 17: %w", errors.New("owner missing")))
	if id == nil {
		t.Fatal("sentry-go did not capture the event")
	}
	if !hub.Flush(10 * time.Second) {
		t.Fatal("sentry-go could not flush the event to izci")
	}

	ev, cols := lastEvent(t, e)
	if cols["customer"] != "sivas-belediyesi" || cols["tenant"] != "t-7" || cols["platform"] != "go" {
		t.Fatalf("customer/tenant/platform mapping: %v", cols)
	}
	if ev["event_id"] != strings.ToLower(string(*id))[:8]+"-"+string(*id)[8:12]+"-"+string(*id)[12:16]+"-"+string(*id)[16:20]+"-"+string(*id)[20:] {
		t.Errorf("event id not preserved: %v vs %s", ev["event_id"], *id)
	}
	if ev["environment"] != "staging" || ev["release"] != "kalem@2.1.0" {
		t.Errorf("environment/release: %v %v", ev["environment"], ev["release"])
	}
	excs, _ := ev["exceptions"].([]any)
	if len(excs) == 0 {
		t.Fatalf("no exceptions stored: %v", ev)
	}
	last := excs[len(excs)-1].(map[string]any)
	if !strings.Contains(fmt.Sprint(last["value"]), "owner missing") && !strings.Contains(fmt.Sprint(last["value"]), "approve order") {
		t.Errorf("exception value: %v", last["value"])
	}
	tags, _ := ev["tags"].(map[string]any)
	if _, leaked := tags["customer"]; leaked {
		t.Error("customer tag must be consumed by the customer mapping")
	}
}

// envelope builds a Sentry envelope with one item per payload.
func envelope(header string, items ...[2]string) []byte {
	var b bytes.Buffer
	b.WriteString(header + "\n")
	for _, it := range items {
		b.WriteString(fmt.Sprintf(`{"type":%q,"length":%d}`+"\n", it[0], len(it[1])))
		b.WriteString(it[1] + "\n")
	}
	return b.Bytes()
}

func post(t *testing.T, url, contentType, encoding string, body []byte, hdr map[string]string) *http.Response {
	t.Helper()
	switch encoding {
	case "gzip":
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(body)
		zw.Close()
		body = buf.Bytes()
	case "deflate":
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		zw.Write(body)
		zw.Close()
		body = buf.Bytes()
	case "br":
		var buf bytes.Buffer
		bw := brotli.NewWriter(&buf)
		bw.Write(body)
		bw.Close()
		body = buf.Bytes()
	}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func expectOK(t *testing.T, res *http.Response) map[string]string {
	t.Helper()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	var out map[string]string
	_ = json.Unmarshal(body, &out)
	return out
}

func TestBrowserEnvelopeWithQueryKey(t *testing.T) {
	e := newEnv(t, 0)
	event := `{
	  "event_id": "2c1f8a2b9d0e4f5a8b7c6d5e4f3a2b1c",
	  "timestamp": 1790000000.123,
	  "platform": "javascript",
	  "level": "error",
	  "environment": "production",
	  "release": "kalem-web@3.0.0",
	  "exception": {"values": [{
	    "type": "TypeError",
	    "value": "Cannot read properties of null (reading 'id')",
	    "mechanism": {"type": "onunhandledrejection", "handled": false},
	    "stacktrace": {"frames": [
	      {"filename": "https://kalem.example.com/assets/vendor-Bx1c9Kq2.js", "function": "r", "lineno": 1, "colno": 4412, "in_app": false},
	      {"filename": "https://kalem.example.com/assets/index-CeX-bC8z.js", "function": "submitForm", "lineno": 1, "colno": 88120, "in_app": true}
	    ]}
	  }]},
	  "breadcrumbs": [{"timestamp": 1789999999.5, "category": "ui.click", "message": "button#submit", "level": "info"}],
	  "request": {"url": "https://kalem.example.com/basvuru?step=2", "headers": {"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "Referer": "https://kalem.example.com/"}},
	  "user": {"ip_address": "{{auto}}", "id": "u-9"},
	  "tags": {"customer": "tokat-belediyesi"},
	  "sdk": {"name": "sentry.javascript.react", "version": "9.1.0"}
	}`
	body := envelope(`{"event_id":"2c1f8a2b9d0e4f5a8b7c6d5e4f3a2b1c","sent_at":"2026-09-26T12:00:00Z"}`,
		[2]string{"event", event}, [2]string{"client_report", `{"discarded_events":[]}`})
	url := fmt.Sprintf("%s/api/%d/envelope/?sentry_key=%s&sentry_version=7", e.Server.URL, e.Project.PublicID, e.Key)
	out := expectOK(t, post(t, url, "text/plain;charset=UTF-8", "", body, map[string]string{"Origin": "https://kalem.example.com"}))
	if out["id"] != "2c1f8a2b9d0e4f5a8b7c6d5e4f3a2b1c" {
		t.Errorf("response id = %q", out["id"])
	}
	ev, cols := lastEvent(t, e)
	if cols["platform"] != "javascript" || cols["customer"] != "tokat-belediyesi" || cols["browser"] != "Safari 17" {
		t.Errorf("columns: %v", cols)
	}
	if cols["ip"] != "127.0.0.1" {
		t.Errorf("{{auto}} must resolve to the sender address for browser events, got %q", cols["ip"])
	}
	req := ev["request"].(map[string]any)
	if req["query_string"] != "step=2" || req["url"] != "https://kalem.example.com/basvuru" {
		t.Errorf("request: %v", req)
	}

	// Origins not allowed for the key are rejected.
	res := post(t, url, "text/plain", "", body, map[string]string{"Origin": "https://evil.example.com"})
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %d", res.StatusCode)
	}
}

func TestPythonGzipChainedException(t *testing.T) {
	e := newEnv(t, 0)
	event := `{
	  "event_id": "9a3e6c7b1f2d4e5f8a9b0c1d2e3f4a5b",
	  "timestamp": "2026-09-26T11:59:58.412Z",
	  "platform": "python",
	  "level": "error",
	  "logger": "raporlar.tasks",
	  "logentry": {"message": "Rapor %s oluşturulamadı", "params": ["R-2291"], "formatted": "Rapor R-2291 oluşturulamadı"},
	  "exception": {"values": [
	    {"type": "KeyError", "value": "'district'", "module": "builtins",
	     "stacktrace": {"frames": [{"filename": "raporlar/tasks.py", "function": "load_row", "lineno": 88, "in_app": true, "vars": {"row": "{'id': 3}", "api_token": "abc123"}}]}},
	    {"type": "ReportError", "value": "Rapor R-2291 oluşturulamadı", "module": "raporlar.errors",
	     "mechanism": {"type": "celery", "handled": false},
	     "stacktrace": {"frames": [
	       {"filename": "celery/app/trace.py", "abs_path": "/usr/lib/python3.12/site-packages/celery/app/trace.py", "function": "__call__", "lineno": 453},
	       {"filename": "raporlar/tasks.py", "function": "build_report", "lineno": 41, "context_line": "    rows = [load_row(r) for r in source]", "in_app": true}
	     ]}}
	  ]},
	  "request": {"method": "POST", "url": "https://api.example.com/raporlar", "query_string": [["format", "pdf"], ["token", "zzz"]],
	              "headers": [["Content-Type", "application/json"], ["Authorization", "Token secret"]], "data": {"sifre": "1234", "yil": 2026}},
	  "contexts": {"runtime": {"type": "runtime", "name": "CPython", "version": "3.12.4"}, "os": {"type": "os", "name": "Linux"},
	               "celery": {"task_id": "c-77", "retries": 2}},
	  "tags": [["izci.tenant", "t-12"], ["queue", "reports"]],
	  "extra": {"sys.argv": ["worker"], "password": "x"},
	  "sdk": {"name": "sentry.python.celery", "version": "2.19.0"}
	}`
	url := fmt.Sprintf("%s/api/%d/envelope/", e.Server.URL, e.Project.PublicID)
	auth := fmt.Sprintf("Sentry sentry_version=7, sentry_client=sentry.python/2.19.0, sentry_key=%s", e.Key)
	expectOK(t, post(t, url, "application/x-sentry-envelope", "gzip",
		envelope(`{"event_id":"9a3e6c7b1f2d4e5f8a9b0c1d2e3f4a5b"}`, [2]string{"event", event}),
		map[string]string{"X-Sentry-Auth": auth}))

	ev, cols := lastEvent(t, e)
	if cols["tenant"] != "t-12" || cols["platform"] != "python" {
		t.Errorf("columns: %v", cols)
	}
	excs := ev["exceptions"].([]any)
	if len(excs) != 2 || excs[1].(map[string]any)["type"] != "ReportError" {
		t.Fatalf("exception chain order must be preserved (root cause first): %v", excs)
	}
	msg := ev["message"].(map[string]any)
	if msg["template"] != "Rapor %s oluşturulamadı" {
		t.Errorf("message template: %v", msg)
	}
	blob, _ := json.Marshal(ev)
	for _, leak := range []string{"abc123", `"1234"`, "Token secret", "zzz"} {
		if strings.Contains(string(blob), leak) {
			t.Errorf("secret %s leaked into the stored event", leak)
		}
	}
	custom := ev["contexts"].(map[string]any)["custom"].(map[string]any)
	if _, ok := custom["celery"]; !ok {
		t.Errorf("unknown contexts must be kept as custom blocks: %v", custom)
	}
}

func TestDotnetDeflateAndDartBrotli(t *testing.T) {
	e := newEnv(t, 0)
	dotnet := `{
	  "event_id": "11112222333344445555666677778888",
	  "platform": "csharp", "level": "fatal",
	  "exception": {"values": [{"type": "System.InvalidOperationException", "value": "Sequence contains no elements", "module": "System.Linq",
	    "mechanism": {"type": "AppDomain.UnhandledException", "handled": false},
	    "stacktrace": {"frames": [
	      {"function": "First", "module": "System.Linq.Enumerable", "in_app": false},
	      {"function": "GetActiveTariff", "module": "KentOS.Kalem.Tariffs.TariffService", "filename": "TariffService.cs", "abs_path": "C:\\build\\src\\Tariffs\\TariffService.cs", "lineno": 57, "in_app": true}
	    ]}}]},
	  "contexts": {"device": {"type": "device", "processor_count": 16, "cpu_description": "AMD EPYC", "memory_size": 34359738368, "usable_memory": 8589934592},
	               "Current Culture": {"name": "tr-TR", "display_name": "Turkish (Türkiye)"}},
	  "sdk": {"name": "sentry.dotnet.aspnetcore", "version": "5.0.0"}
	}`
	url := fmt.Sprintf("%s/api/%d/store/", e.Server.URL, e.Project.PublicID)
	expectOK(t, post(t, url, "application/json", "deflate", []byte(dotnet),
		map[string]string{"X-Sentry-Auth": "Sentry sentry_key=" + e.Key + ", sentry_version=7"}))
	ev, cols := lastEvent(t, e)
	if cols["platform"] != "dotnet" || ev["level"] != "LEVEL_FATAL" {
		t.Errorf("dotnet mapping: %v level=%v", cols, ev["level"])
	}
	mem := ev["contexts"].(map[string]any)["device"].(map[string]any)["memory"].(map[string]any)
	if mem["total_bytes"] != "34359738368" {
		t.Errorf("device memory mapping: %v", mem)
	}

	dart := `{
	  "event_id": "aaaabbbbccccddddeeeeffff00001111", "platform": "dart", "level": "error",
	  "exception": {"values": [{"type": "StateError", "value": "Bad state: No element",
	    "mechanism": {"type": "FlutterError", "handled": true},
	    "stacktrace": {"frames": [
	      {"function": "ListMixin.first", "abs_path": "dart:collection/list.dart", "package": "dart:collection", "lineno": 43},
	      {"function": "OdemeEkrani.build", "abs_path": "package:sbmobil/ekranlar/odeme.dart", "filename": "odeme.dart", "lineno": 118, "in_app": true}
	    ]}}]},
	  "contexts": {"device": {"type": "device", "model": "SM-S918B", "manufacturer": "samsung", "screen_density": 3.5, "battery_level": 62.0, "simulator": false},
	               "os": {"type": "os", "name": "Android", "version": "14"}, "app": {"type": "app", "app_name": "SB Mobil", "app_version": "4.2.0", "app_build": "420"}},
	  "tags": {"customer": "sivas-belediyesi"}
	}`
	env := envelope(fmt.Sprintf(`{"dsn":"http://%s@izci.test/%d"}`, e.Key, e.Project.PublicID), [2]string{"event", dart},
		[2]string{"session", `{"sid":"x","status":"ok"}`})
	expectOK(t, post(t, fmt.Sprintf("%s/api/%d/envelope/", e.Server.URL, e.Project.PublicID), "application/x-sentry-envelope", "br", env, nil))
	ev, cols = lastEvent(t, e)
	if cols["platform"] != "dart" || cols["customer"] != "sivas-belediyesi" {
		t.Errorf("dart mapping: %v", cols)
	}
	app := ev["contexts"].(map[string]any)["app"].(map[string]any)
	if app["name"] != "SB Mobil" || app["version"] != "4.2.0" {
		t.Errorf("app context: %v", app)
	}
}

func TestAuthAndRateLimits(t *testing.T) {
	e := newEnv(t, 60)
	event := `{"platform":"php","exception":{"values":[{"type":"Exception","value":"boom"}]}}`
	url := fmt.Sprintf("%s/api/%d/store/", e.Server.URL, e.Project.PublicID)

	res := post(t, url, "application/json", "", []byte(event), map[string]string{"X-Sentry-Auth": "Sentry sentry_key=izk_unknownunknownunknown000"})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown key: %d", res.StatusCode)
	}
	wrong := fmt.Sprintf("%s/api/%d/store/", e.Server.URL, e.Project.PublicID+100)
	res = post(t, wrong, "application/json", "", []byte(event), map[string]string{"X-Sentry-Auth": "Sentry sentry_key=" + e.Key})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("key used with another project id: %d", res.StatusCode)
	}

	var limited *http.Response
	for range 40 {
		res := post(t, url, "application/json", "", []byte(event), map[string]string{"X-Sentry-Auth": "Sentry sentry_key=" + e.Key})
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests {
			limited = res
			break
		}
	}
	if limited == nil {
		t.Fatal("expected 429 after the burst")
	}
	if limited.Header.Get("Retry-After") == "" || !strings.Contains(limited.Header.Get("X-Sentry-Rate-Limits"), ":error;default;attachment:project") {
		t.Errorf("rate limit headers: %v", limited.Header)
	}

	// Envelopes with only non-event items are accepted and dropped.
	only := envelope(`{}`, [2]string{"transaction", `{"type":"transaction"}`})
	expectOK(t, post(t, fmt.Sprintf("%s/api/%d/envelope/?sentry_key=%s", e.Server.URL, e.Project.PublicID, e.Key), "application/x-sentry-envelope", "", only, nil))
}
