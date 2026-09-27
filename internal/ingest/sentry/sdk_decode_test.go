package sentry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// The testdata/sdk envelopes were recorded from unmodified Sentry SDKs
// (Sentry .NET 6.11.1 console and ASP.NET Core, @sentry/node 11.0.0,
// @sentry/browser 11.0.0, sentry-python 2.70.0, sentry (Dart) 9.30.1,
// sentry (Rust) 0.49.3) pointed at izci, with keys, paths and host names
// replaced.

func fixture(t *testing.T, name string) (map[string]any, []item) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sdk", name))
	if err != nil {
		t.Fatal(err)
	}
	header, items, err := parseEnvelope(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return header, items
}

func itemOf(t *testing.T, items []item, typ string) item {
	t.Helper()
	for _, it := range items {
		if it.typ == typ {
			return it
		}
	}
	t.Fatalf("no %s item", typ)
	return item{}
}

// TestSDKFixturesDecode decodes every item of every recorded envelope.
func TestSDKFixturesDecode(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "sdk", "*.envelope"))
	if len(files) < 30 {
		t.Fatalf("expected the recorded SDK envelopes, found %d", len(files))
	}
	seen := map[string]int{}
	for _, f := range files {
		_, items := fixture(t, filepath.Base(f))
		for _, it := range items {
			seen[it.typ]++
			var err error
			switch it.typ {
			case "event":
				var ev *ingestv1.Event
				if ev, err = Decode(it.payload); err == nil && len(ev.GetExceptions()) == 0 && ev.GetMessage() == nil {
					t.Errorf("%s: event without exception or message", f)
				}
			case "feedback":
				_, err = DecodeFeedback(it.payload, 0)
			case "session":
				_, err = DecodeSession(it.payload, 0)
			case "sessions":
				_, err = DecodeSessionAggregates(it.payload, 0)
			}
			if err != nil {
				t.Errorf("%s: %s: %v", f, it.typ, err)
			}
		}
	}
	for _, typ := range []string{"event", "attachment", "feedback", "session", "client_report", "transaction", "span"} {
		if seen[typ] == 0 {
			t.Errorf("the fixtures contain no %s item", typ)
		}
	}
}

func TestDotnetEvent(t *testing.T) {
	_, items := fixture(t, "dotnet-02-event-session.envelope")
	ev, err := Decode(itemOf(t, items, "event").payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.GetPlatform() != "dotnet" || len(ev.GetExceptions()) != 2 {
		t.Fatalf("platform %q, %d exceptions", ev.GetPlatform(), len(ev.GetExceptions()))
	}
	primary := ev.GetExceptions()[1]
	frames := primary.GetStacktrace().GetFrames()
	if primary.GetType() != "System.ApplicationException" || len(frames) == 0 ||
		frames[len(frames)-1].GetFilename() != "Program.cs" || frames[len(frames)-1].GetLineno() == 0 {
		t.Fatalf("primary exception: %v", primary)
	}
	c := ev.GetContexts()
	if c.GetCulture().GetLocale() != "en-TR" || c.GetCulture().GetCalendar() != "GregorianCalendar" {
		t.Errorf("culture from \"Current Culture\": %v", c.GetCulture())
	}
	if c.GetRuntime().GetName() != ".NET" || c.GetOs().GetName() != "macOS" {
		t.Errorf("runtime %v, os %v", c.GetRuntime(), c.GetOs())
	}
	if _, ok := c.GetCustom()["Memory Info"]; !ok {
		t.Error("custom .NET contexts must be kept")
	}
	// Fields izci does not model stay visible under the context's name.
	if dev := c.GetCustom()["device"]; dev == nil || dev.GetFields()["timezone"].GetStringValue() == "" {
		t.Errorf("device leftovers: %v", c.GetCustom()["device"])
	}
	if rt := c.GetCustom()["runtime"]; rt == nil || rt.GetFields()["identifier"].GetStringValue() == "" {
		t.Errorf("runtime leftovers: %v", c.GetCustom()["runtime"])
	}
	if ev.GetRequest() != nil {
		t.Errorf("an empty request must be dropped: %v", ev.GetRequest())
	}
	if ev.GetUser().GetIpAddress() != "{{auto}}" {
		t.Errorf("{{auto}} is resolved by normalization, got %q", ev.GetUser().GetIpAddress())
	}
}

func TestAspNetCoreEvent(t *testing.T) {
	_, items := fixture(t, "aspnet-02-event-client_report.envelope")
	ev, err := Decode(itemOf(t, items, "event").payload)
	if err != nil {
		t.Fatal(err)
	}
	r := ev.GetRequest()
	if r.GetMethod() != "POST" || !strings.HasSuffix(r.GetUrl(), "/payments") || r.GetBody() == "" {
		t.Errorf("request: %v", r)
	}
	if ev.GetMessage().GetTemplate() != "Payment provider {Provider} declined card for order {OrderId}" ||
		ev.GetMessage().GetFormatted() != "Payment provider iyzico declined card for order 7" {
		t.Errorf("logger message: %v", ev.GetMessage())
	}
	_, items = fixture(t, "aspnet-01-event-client_report.envelope")
	ev, _ = Decode(itemOf(t, items, "event").payload)
	if q := ev.GetRequest().GetQueryString(); q != "expand=items&page=2" {
		t.Errorf("query string %q", q)
	}
	if ev.GetTransaction() != "GET /orders/{id:int}" {
		t.Errorf("transaction %q", ev.GetTransaction())
	}
}

func TestPlatformsFromSDK(t *testing.T) {
	for name, want := range map[string]string{
		"dart-01-event.envelope":    "dart",
		"rust-01-event.envelope":    "rust",
		"python-01-event.envelope":  "python",
		"node-04-event.envelope":    "node",
		"browser-04-event.envelope": "javascript",
	} {
		_, items := fixture(t, name)
		ev, err := Decode(itemOf(t, items, "event").payload)
		if err != nil {
			t.Fatal(err)
		}
		if ev.GetPlatform() != want {
			t.Errorf("%s: platform %q, want %q", name, ev.GetPlatform(), want)
		}
	}
}

func TestFeedbackFixtures(t *testing.T) {
	for name, want := range map[string]string{
		"dotnet-06-feedback.envelope":  "Ödeme sayfası dondu, iki kez denedim.",
		"node-01-feedback.envelope":    "Ödeme sayfası dondu",
		"dart-03-feedback.envelope":    "Harita yüklenmiyor",
		"browser-08-feedback.envelope": "Kaydet düğmesi çalışmıyor",
	} {
		_, items := fixture(t, name)
		fb, err := DecodeFeedback(itemOf(t, items, "feedback").payload, 0)
		if err != nil {
			t.Fatal(err)
		}
		if fb.Message != want || fb.Email == "" || fb.EventID == nil || fb.Event == nil {
			t.Errorf("%s: %+v", name, fb)
		}
		if _, ok := fb.Event.GetContexts().GetCustom()["feedback"]; ok {
			t.Errorf("%s: the feedback context is stored as feedback, not as a custom context", name)
		}
	}
	fb, err := DecodeUserReport([]byte(`{"event_id":"0b3f4c8e2a9d4b1c8f7e6d5c4b3a2918","name":"Ali","email":"ali@example.com","comments":"Çöktü"}`))
	if err != nil || fb.Message != "Çöktü" || fb.EventID == nil || fb.Source != "user_report" {
		t.Fatalf("user report: %+v %v", fb, err)
	}
	again, _ := DecodeUserReport([]byte(`{"event_id":"0b3f4c8e2a9d4b1c8f7e6d5c4b3a2918","name":"Ali","email":"ali@example.com","comments":"Çöktü"}`))
	if again.FeedbackID != fb.FeedbackID {
		t.Error("a retried user report must keep its id")
	}
}

func TestSessionFixtures(t *testing.T) {
	_, items := fixture(t, "dotnet-01-session.envelope")
	u, err := DecodeSession(itemOf(t, items, "session").payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Init || u.Release != "kalem@2.0.0" || u.Environment != "production" || u.DistinctID == "" || u.Started.IsZero() {
		t.Fatalf("session: %+v", u)
	}
	_, items = fixture(t, "dotnet-09-event-session.envelope")
	u, _ = DecodeSession(itemOf(t, items, "session").payload, 0)
	if u.Status != "crashed" {
		t.Errorf("the crash run must end its session as crashed: %+v", u)
	}
	agg, err := DecodeSessionAggregates([]byte(`{"aggregates":[{"started":"2026-09-27T02:00:00Z","exited":7,"errored":2,"crashed":1}],"attrs":{"release":"api@1.0.0","environment":"staging"}}`), time.Hour)
	if err != nil || len(agg) != 1 || agg[0].Exited != 7 || agg[0].Crashed != 1 || agg[0].Release != "api@1.0.0" ||
		!agg[0].Started.Equal(time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("aggregates: %+v %v", agg, err)
	}
}

func TestAttachmentHeaders(t *testing.T) {
	for name, want := range map[string]string{
		"dotnet-04-event-attachment.envelope": "export.log",
		"node-05-event-attachment.envelope":   "payload.json",
		"python-02-event-attachment.envelope": "tahsilat.log",
		"dart-02-event-attachment.envelope":   "durum.txt",
	} {
		_, items := fixture(t, name)
		a := itemOf(t, items, "attachment")
		if a.header.Filename != want || len(a.payload) == 0 || a.header.ContentType == "" {
			t.Errorf("%s: %+v (%d bytes)", name, a.header, len(a.payload))
		}
	}
}

func TestClockDrift(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if d := clockDrift("2026-09-27T11:59:40Z", now); d != 0 {
		t.Errorf("ordinary delays are not corrected: %v", d)
	}
	if d := clockDrift("2026-09-27T09:00:00Z", now); d != 3*time.Hour {
		t.Errorf("a clock 3 hours behind: %v", d)
	}
	if d := clockDrift("2026-09-28T12:00:00+00:00", now); d != -24*time.Hour {
		t.Errorf("a clock a day ahead: %v", d)
	}
	if d := clockDrift(nil, now); d != 0 {
		t.Errorf("without sent_at: %v", d)
	}
}
