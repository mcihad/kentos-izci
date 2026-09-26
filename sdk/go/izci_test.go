package izci_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/durationpb"

	izci "github.com/mcihad/kentos-izci/sdk/go"
	"github.com/mcihad/kentos-izci/sdk/go/izcihttp"
	"github.com/mcihad/kentos-izci/sdk/go/izcislog"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// fakeServer records the events it receives.
type fakeServer struct {
	ingestv1connect.UnimplementedIngestServiceHandler
	mu       sync.Mutex
	events   []*ingestv1.Event
	auth     string
	limitFor int // answer RESOURCE_EXHAUSTED for this many calls
}

func (f *fakeServer) SendEvents(_ context.Context, req *connect.Request[ingestv1.SendEventsRequest]) (*connect.Response[ingestv1.SendEventsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = req.Header().Get("Authorization")
	if f.limitFor > 0 {
		f.limitFor--
		err := connect.NewError(connect.CodeResourceExhausted, errors.New("rate limit exceeded"))
		d, _ := connect.NewErrorDetail(&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Hour)})
		err.AddDetail(d)
		return nil, err
	}
	resp := &ingestv1.SendEventsResponse{}
	for _, ev := range req.Msg.GetEvents() {
		f.events = append(f.events, ev)
		resp.Results = append(resp.Results, &ingestv1.EventResult{EventId: ev.GetEventId(), Outcome: ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED})
	}
	return connect.NewResponse(resp), nil
}

func (f *fakeServer) received() []*ingestv1.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*ingestv1.Event(nil), f.events...)
}

func setup(t *testing.T, opts izci.Options) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	path, h := ingestv1connect.NewIngestServiceHandler(fs)
	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	opts.Endpoint, opts.Key = srv.URL, "izk_testkeytestkeytestkeytestkey00"
	if opts.FlushInterval == 0 {
		opts.FlushInterval = 20 * time.Millisecond
	}
	if err := izci.Init(opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { izci.Close(time.Second) })
	return fs
}

type validationError struct{ Field string }

func (e *validationError) Error() string { return "invalid field " + e.Field }

func loadOrder() error {
	return fmt.Errorf("load order 42: %w", &validationError{Field: "owner"})
}

func TestCaptureErrorChainAndStack(t *testing.T) {
	srv := setup(t, izci.Options{Release: "1.2.3", Environment: "staging", CustomerID: "sivas", CustomerName: "Sivas Belediyesi"})
	err := fmt.Errorf("approve: %w", errors.Join(loadOrder(), fs.ErrNotExist))
	id := izci.CaptureError(err, izci.WithTag("module", "orders"))
	if id == "" {
		t.Fatal("no event id")
	}
	if !izci.Flush(2 * time.Second) {
		t.Fatal("flush timed out")
	}
	evs := srv.received()
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	ev := evs[0]
	if srv.auth != "Bearer izk_testkeytestkeytestkeytestkey00" {
		t.Errorf("authorization header: %q", srv.auth)
	}
	if ev.GetEventId() != id || ev.GetPlatform() != "go" || ev.GetRelease() != "1.2.3" || ev.GetEnvironment() != "staging" {
		t.Errorf("event basics: %v", ev)
	}
	if ev.GetCustomer().GetId() != "sivas" || ev.GetTags()["module"] != "orders" {
		t.Errorf("customer/tags: %v %v", ev.GetCustomer(), ev.GetTags())
	}
	var types []string
	for _, ex := range ev.GetExceptions() {
		types = append(types, ex.GetType())
	}
	// Root cause first; transparent fmt.Errorf links are merged away.
	want := []string{"*izci_test.validationError", "errors.Join", ""}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("exception chain = %v, want %v", types, want)
	}
	primary := ev.GetExceptions()[len(ev.GetExceptions())-1]
	if primary.GetMechanism().GetHandled() != true || primary.GetMechanism().GetType() != "generic" {
		t.Errorf("mechanism: %v", primary.GetMechanism())
	}
	frames := primary.GetStacktrace().GetFrames()
	if len(frames) == 0 {
		t.Fatal("primary exception has no stack trace")
	}
	top := frames[len(frames)-1]
	if !strings.Contains(top.GetFunction(), "TestCaptureErrorChainAndStack") || top.GetLineno() == 0 {
		t.Errorf("crash frame must be the caller: %v", top)
	}
	if top.GetContextLine() == "" || !strings.Contains(top.GetContextLine(), "CaptureError") {
		t.Errorf("source context missing: %q", top.GetContextLine())
	}
	for _, f := range frames {
		if strings.HasPrefix(f.GetModule(), "github.com/mcihad/kentos-izci/sdk/go") && !strings.HasSuffix(f.GetModule(), "_test") {
			t.Errorf("SDK frames must be removed: %v", f)
		}
	}
	rc := ev.GetContexts()
	if rc.GetRuntime().GetName() != "go" || rc.GetProcess().GetGoroutines() == 0 || rc.GetDevice().GetCpu().GetCount() == 0 {
		t.Errorf("runtime contexts: %v", rc)
	}
	if rc.GetProcess().GetHeapAllocBytes() == 0 || rc.GetProcess().GetPid() != uint32(os.Getpid()) {
		t.Errorf("process context: %v", rc.GetProcess())
	}
}

func TestScopesAreIsolated(t *testing.T) {
	srv := setup(t, izci.Options{DisableRuntimeContext: true})
	ctxA := izci.WithScope(context.Background(), func(s *izci.Scope) {
		s.SetUser(izci.User{ID: "1", Email: "a@example.com"})
		s.SetTenant("t-1", "")
		s.AddBreadcrumb(izci.Breadcrumb{Category: "auth", Message: "login"})
	})
	ctxB := izci.WithScope(context.Background(), func(s *izci.Scope) { s.SetUser(izci.User{ID: "2"}) })
	izci.CaptureMessageContext(ctxA, izci.LevelWarning, "from A")
	izci.CaptureMessageContext(ctxB, izci.LevelInfo, "from B")
	izci.Flush(2 * time.Second)
	evs := srv.received()
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	a, b := evs[0], evs[1]
	if a.GetMessage().GetFormatted() != "from A" {
		a, b = b, a
	}
	if a.GetUser().GetId() != "1" || a.GetTenant().GetId() != "t-1" || len(a.GetBreadcrumbs()) != 1 || a.GetLevel() != ingestv1.Level_LEVEL_WARNING {
		t.Errorf("scope A: %v", a)
	}
	if b.GetUser().GetId() != "2" || b.GetTenant() != nil || len(b.GetBreadcrumbs()) != 0 {
		t.Errorf("scope B leaked data: %v", b)
	}
}

func TestHTTPMiddlewareCapturesPanics(t *testing.T) {
	srv := setup(t, izci.Options{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		var o *struct{ Owner string }
		_ = o.Owner // nil pointer dereference panics
	})
	app := httptest.NewServer(izcihttp.New(izcihttp.Options{}).Handle(mux))
	defer app.Close()
	req, _ := http.NewRequest(http.MethodGet, app.URL+"/orders/7?expand=items", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("User-Agent", "test-agent")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d", res.StatusCode)
	}
	izci.Flush(2 * time.Second)
	evs := srv.received()
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	ev := evs[0]
	ex := ev.GetExceptions()[len(ev.GetExceptions())-1]
	if ex.GetMechanism().GetType() != "panic" || ex.GetMechanism().GetHandled() || ev.GetLevel() != ingestv1.Level_LEVEL_FATAL {
		t.Errorf("panic mechanism: %v level %v", ex.GetMechanism(), ev.GetLevel())
	}
	if !strings.Contains(ex.GetValue(), "nil pointer") || ex.GetType() != "runtime.Error" {
		t.Errorf("panic value: %q", ex.GetValue())
	}
	r := ev.GetRequest()
	if r.GetMethod() != "GET" || r.GetRoute() != "GET /orders/{id}" || r.GetQueryString() != "expand=items" || ev.GetTransaction() != "GET /orders/{id}" {
		t.Errorf("request: %v transaction %q", r, ev.GetTransaction())
	}
	for _, h := range r.GetHeaders() {
		if strings.EqualFold(h.GetName(), "Authorization") {
			t.Error("Authorization must not be sent")
		}
	}
	frames := ex.GetStacktrace().GetFrames()
	if len(frames) == 0 || !strings.Contains(frames[len(frames)-1].GetFunction(), "TestHTTPMiddlewareCapturesPanics") {
		t.Errorf("panic stack must end at the panicking handler: %v", frames[len(frames)-1])
	}
}

func TestSlogHandler(t *testing.T) {
	srv := setup(t, izci.Options{DisableRuntimeContext: true})
	logger := slog.New(izcislog.New(slog.DiscardHandler, izcislog.Options{}))
	ctx := izci.WithScope(context.Background(), nil)
	logger.InfoContext(ctx, "order loaded", "order", 42)
	logger.ErrorContext(ctx, "payment failed", "err", errors.New("card declined"), "amount", 99.5)
	izci.Flush(2 * time.Second)
	evs := srv.received()
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.GetExceptions()[0].GetValue() != "card declined" || ev.GetMessage().GetFormatted() != "payment failed" {
		t.Errorf("slog error event: %v", ev)
	}
	if ev.GetExtra().AsMap()["amount"] != 99.5 {
		t.Errorf("extra: %v", ev.GetExtra())
	}
	if len(ev.GetBreadcrumbs()) != 1 || ev.GetBreadcrumbs()[0].GetMessage() != "order loaded" {
		t.Errorf("info log must become a breadcrumb: %v", ev.GetBreadcrumbs())
	}
}

func TestRateLimitBackoff(t *testing.T) {
	srv := setup(t, izci.Options{DisableRuntimeContext: true})
	srv.mu.Lock()
	srv.limitFor = 1
	srv.mu.Unlock()
	izci.CaptureMessage("first")
	izci.Flush(2 * time.Second)
	// The server asked to back off for an hour: new events are dropped
	// locally instead of hammering it.
	if id := izci.CaptureMessage("second"); id != "" {
		t.Errorf("event captured during back-off: %s", id)
	}
	if izci.CurrentClient().Dropped() < 2 {
		t.Errorf("dropped = %d", izci.CurrentClient().Dropped())
	}
	if len(srv.received()) != 0 {
		t.Error("no event should have been stored")
	}
}
