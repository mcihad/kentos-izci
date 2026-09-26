package ingest

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func projectCtx(mutate func(*apiv1.ProjectSettings)) ProjectContext {
	s := store.DefaultProjectSettings()
	if mutate != nil {
		mutate(s)
	}
	return ProjectContext{OrgID: uuid.New(), ProjectID: uuid.New(), ProjectSlug: "kalem", Platform: "dotnet", Settings: s}
}

func dotnetEvent() *ingestv1.Event {
	extra, _ := structpb.NewStruct(map[string]any{"order_id": 91512, "api_key": "sk_live_123"})
	return &ingestv1.Event{
		EventId:  "6F1C1B5E2C3D4F6A9B8E0D1E2F3A4B5C",
		Platform: "DotNet",
		Customer: &ingestv1.Customer{Id: " Sivas-Belediyesi ", Name: "Sivas Belediyesi"},
		Tenant:   &ingestv1.Tenant{Id: "t-19"},
		Exceptions: []*ingestv1.Exception{{
			Type:      "System.NullReferenceException",
			Value:     "Object reference not set to an instance of an object.",
			Mechanism: &ingestv1.Mechanism{Type: "middleware", Handled: ptr(false)},
			Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{
				{Module: "Microsoft.AspNetCore.Mvc", Function: "Invoke"},
				{Module: "KentOS.Kalem.Services", Function: "OrderService.CreateOrder", Filename: "/src/Services/OrderService.cs", Lineno: 142,
					Vars: map[string]string{"password": "hunter2", "count": "3"}},
			}},
		}},
		Request: &ingestv1.Request{
			Method: "post",
			Url:    "https://kalem.example.com/api/orders?token=abc123&page=2",
			Headers: []*ingestv1.Header{
				{Name: "Authorization", Value: "Bearer secret-token-value"},
				{Name: "User-Agent", Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.6668.58 Safari/537.36"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "X-Forwarded-For", Value: "203.0.113.50"},
			},
			Cookies:    map[string]string{"session": "abc", "theme": "dark"},
			Body:       `{"email":"ali@example.com","password":"p4ss"}`,
			RemoteAddr: "203.0.113.50",
		},
		User:  &ingestv1.User{Id: "42", Email: "ali@example.com"},
		Tags:  map[string]string{"module": "orders", "izci.internal": "x", "bad key": "y", "environment": "fake"},
		Extra: extra,
		Contexts: &ingestv1.Contexts{
			Process:  &ingestv1.ProcessContext{RssBytes: 512 << 20},
			Database: &ingestv1.DatabaseContext{System: "PostgreSQL", Statement: "SELECT * FROM users WHERE password = 'x' AND token=abcdef123456"},
		},
	}
}

func TestNormalizeServerEvent(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	ev := dotnetEvent()
	ev.Timestamp = timestamppb.New(now.Add(time.Hour)) // in the future
	p, err := Normalize(ev, NormalizeOptions{Project: projectCtx(nil), TransportIP: netip.MustParseAddr("10.0.0.9"), Retention: 90 * 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if p.EventID.String() != "6f1c1b5e-2c3d-4f6a-9b8e-0d1e2f3a4b5c" {
		t.Errorf("event id = %s", p.EventID)
	}
	if !p.ReceivedAt.Equal(now) || !p.OccurredAt.Equal(now) || uuidV7Time(p.ID) != now {
		t.Errorf("times: received %s occurred %s id-time %s", p.ReceivedAt, p.OccurredAt, uuidV7Time(p.ID))
	}
	if p.Platform != "dotnet" || p.Environment != "production" || p.Level != ingestv1.Level_LEVEL_ERROR || !p.Unhandled {
		t.Errorf("defaults: platform=%s env=%s level=%s unhandled=%v", p.Platform, p.Environment, p.Level, p.Unhandled)
	}
	if p.Title != "System.NullReferenceException: Object reference not set to an instance of an object." {
		t.Errorf("title = %q", p.Title)
	}
	if p.Culprit != "OrderService.CreateOrder in OrderService.cs" || p.Location.Lineno != 142 {
		t.Errorf("culprit = %q location=%+v", p.Culprit, p.Location)
	}
	frames := ev.Exceptions[0].Stacktrace.Frames
	if frames[0].GetInApp() || !frames[1].GetInApp() {
		t.Errorf("in-app resolution wrong: %v %v", frames[0].GetInApp(), frames[1].GetInApp())
	}
	if frames[1].Vars["password"] != Filtered || frames[1].Vars["count"] != "3" {
		t.Errorf("frame vars not scrubbed: %v", frames[1].Vars)
	}
	if p.CustomerKey != "sivas-belediyesi" || p.TenantKey != "t-19" {
		t.Errorf("customer=%q tenant=%q", p.CustomerKey, p.TenantKey)
	}
	r := ev.Request
	if r.Method != "POST" || r.Url != "https://kalem.example.com/api/orders" || !strings.Contains(r.QueryString, "token=%5BFiltered%5D") {
		t.Errorf("request url/query: %q %q", r.Url, r.QueryString)
	}
	if headerValue(r.Headers, "Authorization") != Filtered || r.Cookies["theme"] != Filtered {
		t.Error("credentials and cookie values must be scrubbed")
	}
	if strings.Contains(r.Body, "p4ss") || !strings.Contains(r.Body, "ali@example.com") {
		t.Errorf("body scrub: %s", r.Body)
	}
	if p.ClientIP.String() != "203.0.113.50" {
		t.Errorf("client ip = %s (server events use the address the app saw, not the transport)", p.ClientIP)
	}
	if p.Browser != "Chrome 129" || p.OS != "Windows 10" {
		t.Errorf("ua: browser=%q os=%q", p.Browser, p.OS)
	}
	if ev.Contexts.GetOs() != nil {
		t.Error("server events must not get the end user's OS as their os context")
	}
	if ev.Contexts.GetBrowser().GetName() != "Chrome" {
		t.Error("browser context must be filled from the User-Agent")
	}
	if p.Tags["environment"] != "production" || p.Tags["module"] != "orders" || p.Tags["customer"] != "sivas-belediyesi" || p.Tags["handled"] != "no" {
		t.Errorf("tags: %v", p.Tags)
	}
	if _, ok := p.Tags["izci.internal"]; ok {
		t.Error("reserved tags must be dropped")
	}
	if ev.Extra.Fields["api_key"].GetStringValue() != Filtered {
		t.Error("extra must be scrubbed")
	}
	if strings.Contains(ev.Contexts.Database.Statement, "abcdef123456") {
		t.Errorf("db statement leaks a token: %s", ev.Contexts.Database.Statement)
	}
	if p.UserLabel != "ali@example.com" || len(p.UserHash) != 8 {
		t.Errorf("user identity: %q %x", p.UserLabel, p.UserHash)
	}
	if !strings.Contains(strings.Join(p.Warnings, ";"), "timestamp") {
		t.Errorf("future timestamp must be reported: %v", p.Warnings)
	}
	if len(p.Payload) == 0 || !strings.Contains(string(p.Payload), `"event_id":"6f1c1b5e-2c3d-4f6a-9b8e-0d1e2f3a4b5c"`) {
		t.Errorf("payload: %s", p.Payload)
	}
	if p.Grouping.Strategy != StrategyStacktrace {
		t.Errorf("grouping strategy = %s", p.Grouping.Strategy)
	}
}

func TestNormalizePrivacyAndBrowser(t *testing.T) {
	ev := &ingestv1.Event{
		Platform: "javascript",
		Message:  &ingestv1.Message{Formatted: "Checkout failed"},
		User:     &ingestv1.User{IpAddress: "{{auto}}"},
		Request: &ingestv1.Request{Headers: []*ingestv1.Header{{Name: "User-Agent",
			Value: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"}}},
	}
	ctx := projectCtx(func(s *apiv1.ProjectSettings) { s.Privacy.IpStorage = apiv1.IpStorage_IP_STORAGE_ANONYMIZED })
	p, err := Normalize(ev, NormalizeOptions{Project: ctx, TransportIP: netip.MustParseAddr("198.51.100.77")})
	if err != nil {
		t.Fatal(err)
	}
	if p.ClientIP.String() != "198.51.100.0" || ev.User.IpAddress != "198.51.100.0" {
		t.Errorf("anonymized ip: %s / %s", p.ClientIP, ev.User.IpAddress)
	}
	if p.Level != ingestv1.Level_LEVEL_INFO || p.Grouping.Strategy != StrategyMessage || p.Title != "Checkout failed" {
		t.Errorf("message event: level=%s strategy=%s title=%q", p.Level, p.Grouping.Strategy, p.Title)
	}
	if ev.Contexts.GetOs().GetName() != "iOS" || ev.Contexts.GetOs().GetVersion() != "17.5" || ev.Contexts.GetDevice().GetDeviceType() != "mobile" {
		t.Errorf("browser platform must get os/device from the UA: %v", ev.Contexts)
	}
	if p.Browser != "Safari 17" {
		t.Errorf("browser label = %q", p.Browser)
	}

	none := projectCtx(func(s *apiv1.ProjectSettings) { s.Privacy.IpStorage = apiv1.IpStorage_IP_STORAGE_NONE })
	ev2 := &ingestv1.Event{Platform: "javascript", Message: &ingestv1.Message{Formatted: "x"}, User: &ingestv1.User{Id: "1", IpAddress: "{{auto}}"}}
	p2, _ := Normalize(ev2, NormalizeOptions{Project: none, TransportIP: netip.MustParseAddr("198.51.100.77")})
	if p2.ClientIP.IsValid() || ev2.User.IpAddress != "" {
		t.Errorf("IP_STORAGE_NONE must drop addresses: %s %q", p2.ClientIP, ev2.User.IpAddress)
	}
}

func TestNormalizeRejectsEmptyAndTrims(t *testing.T) {
	if _, err := Normalize(&ingestv1.Event{}, NormalizeOptions{Project: projectCtx(nil)}); err != ErrEmptyEvent {
		t.Fatalf("empty event must be rejected, got %v", err)
	}
	var frames []*ingestv1.Frame
	for i := range 400 {
		frames = append(frames, &ingestv1.Frame{Function: "f", Lineno: uint32(i + 1)})
	}
	ev := &ingestv1.Event{
		Exceptions: []*ingestv1.Exception{{Type: "E", Value: strings.Repeat("x", 20000), Stacktrace: &ingestv1.Stacktrace{Frames: frames}}},
		EventId:    "not-a-uuid",
	}
	p, err := Normalize(ev, NormalizeOptions{Project: projectCtx(nil)})
	if err != nil {
		t.Fatal(err)
	}
	fs := ev.Exceptions[0].Stacktrace.Frames
	if len(fs) != maxFrames || fs[0].Lineno != 1 || fs[len(fs)-1].Lineno != 400 {
		t.Errorf("frames trimmed wrong: len=%d first=%d last=%d", len(fs), fs[0].Lineno, fs[len(fs)-1].Lineno)
	}
	if len(ev.Exceptions[0].Value) != maxValueLen {
		t.Errorf("value length %d", len(ev.Exceptions[0].Value))
	}
	joined := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"event_id", "frames trimmed", "value: truncated"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in:\n%s", want, joined)
		}
	}
}
