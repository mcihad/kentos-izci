package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

type fixture struct {
	*ingesttest.Env
	connect ingestv1connect.IngestServiceClient
	grpc    ingestv1connect.IngestServiceClient
}

func newFixture(t *testing.T, keyRate int32) *fixture {
	t.Helper()
	env := ingesttest.New(t, ingesttest.Options{KeyRateLimit: keyRate, AllowedOrigins: []string{"https://kalem.example.com"}})
	f := &fixture{Env: env}
	f.connect = ingestv1connect.NewIngestServiceClient(http.DefaultClient, env.Server.URL)
	h2c := &http.Transport{Protocols: new(http.Protocols)}
	h2c.Protocols.SetUnencryptedHTTP2(true)
	f.grpc = ingestv1connect.NewIngestServiceClient(&http.Client{Transport: h2c}, env.Server.URL, connect.WithGRPC())
	return f
}

func (f *fixture) authed(msg *ingestv1.SendEventRequest) *connect.Request[ingestv1.SendEventRequest] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+f.Key)
	return req
}

func orderEvent(n int, customer string) *ingestv1.Event {
	return &ingestv1.Event{
		Environment: "production",
		Release:     "1.4.2",
		Customer:    &ingestv1.Customer{Id: customer, Name: "Müşteri " + customer},
		User:        &ingestv1.User{Id: fmt.Sprintf("user-%d", n%5)},
		Exceptions: []*ingestv1.Exception{{
			Type:  "System.NullReferenceException",
			Value: fmt.Sprintf("Order %d has no owner", n),
			Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{
				{Module: "KentOS.Kalem.Services", Function: "OrderService.CreateOrder", Filename: "OrderService.cs", Lineno: uint32(100 + n)},
			}},
		}},
	}
}

func TestSendEventGroupsAndCounts(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()

	// First event over the Connect protocol (HTTP/1.1) creates an issue.
	resp, err := f.connect.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: orderEvent(1, "sivas")}))
	if err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	r := resp.Msg.GetResult()
	if r.GetOutcome() != ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED || r.GetIssueShortId() != "KALEM-1" || !r.GetIssueIsNew() {
		t.Fatalf("unexpected first result: %v", r)
	}
	if r.GetIssueUrl() != "https://izci.test/o/kentos/issues/KALEM-1" {
		t.Errorf("issue url = %q", r.GetIssueUrl())
	}

	// A second occurrence over native gRPC (h2c) joins the same issue.
	ev := orderEvent(2, "tokat")
	ev.EventId = uuid.NewString()
	resp, err = f.grpc.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: ev}))
	if err != nil {
		t.Fatalf("SendEvent over gRPC: %v", err)
	}
	r2 := resp.Msg.GetResult()
	if r2.GetIssueId() != r.GetIssueId() || r2.GetIssueIsNew() {
		t.Fatalf("second event must join the issue: %v", r2)
	}

	// A retried event id is acknowledged as a duplicate.
	dup := orderEvent(2, "tokat")
	dup.EventId = ev.EventId
	resp, err = f.grpc.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: dup}))
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if resp.Msg.GetResult().GetOutcome() != ingestv1.EventOutcome_EVENT_OUTCOME_DUPLICATE || resp.Msg.GetResult().GetIssueId() != r.GetIssueId() {
		t.Fatalf("expected duplicate: %v", resp.Msg.GetResult())
	}

	// A batch with a second fingerprint and several customers.
	var events []*ingestv1.Event
	for i := range 30 {
		e := orderEvent(i, []string{"sivas", "tokat", "yozgat"}[i%3])
		if i%2 == 0 {
			e.Exceptions[0].Type = "System.TimeoutException"
			e.Exceptions[0].Value = fmt.Sprintf("Timeout after %dms", 1000+i)
		}
		events = append(events, e)
	}
	breq := connect.NewRequest(&ingestv1.SendEventsRequest{Events: events})
	breq.Header().Set("X-Izci-Key", f.Key)
	bresp, err := f.connect.SendEvents(ctx, breq)
	if err != nil {
		t.Fatalf("SendEvents: %v", err)
	}
	shortIDs := map[string]int{}
	for _, res := range bresp.Msg.GetResults() {
		if res.GetOutcome() != ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED {
			t.Fatalf("batch event not accepted: %v", res)
		}
		shortIDs[res.GetIssueShortId()]++
	}
	if len(shortIDs) != 2 || shortIDs["KALEM-1"] != 15 || shortIDs["KALEM-2"] != 15 {
		t.Fatalf("batch grouping: %v", shortIDs)
	}

	// Verify what was stored.
	err = f.DB.InOrg(ctx, f.Org.ID, func(tx pgx.Tx) error {
		var times, users, customers int64
		if err := tx.QueryRow(ctx, `SELECT times_seen, users_count, customers_count FROM issues WHERE short_seq = 1`).Scan(&times, &users, &customers); err != nil {
			return err
		}
		if times != 17 || users != 5 || customers != 3 {
			t.Errorf("issue 1 counters: times=%d users=%d customers=%d", times, users, customers)
		}
		var events, hourly, usage int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&events); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT sum(count) FROM issue_hourly`).Scan(&hourly); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT sum(accepted) FROM usage_daily`).Scan(&usage); err != nil {
			return err
		}
		if events != 32 || hourly != 32 || usage != 32 {
			t.Errorf("stored events=%d hourly=%d usage=%d, want 32 each", events, hourly, usage)
		}
		var facet int64
		if err := tx.QueryRow(ctx, `SELECT count FROM issue_tags t JOIN issues i ON i.id = t.issue_id
			WHERE i.short_seq = 2 AND t.key = 'customer' AND t.value = 'yozgat'`).Scan(&facet); err != nil {
			return err
		}
		if facet != 5 {
			t.Errorf("customer facet yozgat on issue 2 = %d, want 5", facet)
		}
		var custName string
		if err := tx.QueryRow(ctx, `SELECT name FROM customers WHERE key = 'sivas'`).Scan(&custName); err != nil {
			return err
		}
		if custName != "Müşteri sivas" {
			t.Errorf("customer name = %q", custName)
		}
		var rel, env int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM releases), (SELECT count(*) FROM environments)`).Scan(&rel, &env); err != nil {
			return err
		}
		if rel != 1 || env != 1 {
			t.Errorf("releases=%d environments=%d", rel, env)
		}
		var activity int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM issue_activity WHERE kind = 1`).Scan(&activity); err != nil {
			return err
		}
		if activity != 2 {
			t.Errorf("created activity rows = %d, want 2", activity)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegressionAndSnooze(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	send := func() *ingestv1.EventResult {
		t.Helper()
		resp, err := f.connect.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: orderEvent(1, "sivas")}))
		if err != nil {
			t.Fatal(err)
		}
		return resp.Msg.GetResult()
	}
	first := send()
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := f.DB.InOrg(ctx, f.Org.ID, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql, args...); return err }); err != nil {
			t.Fatal(err)
		}
	}
	status := func() (int16, int16) {
		t.Helper()
		var s, ss int16
		if err := f.DB.InOrg(ctx, f.Org.ID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT status, substatus FROM issues WHERE id = $1`, first.GetIssueId()).Scan(&s, &ss)
		}); err != nil {
			t.Fatal(err)
		}
		return s, ss
	}

	exec(`UPDATE issues SET status = 2, resolved_at = now() WHERE id = $1`, first.GetIssueId())
	if r := send(); !r.GetIssueRegressed() {
		t.Fatalf("an event after plain resolution must regress: %v", r)
	}
	if s, ss := status(); s != 1 || ss != 3 {
		t.Fatalf("status after regression = %d/%d, want 1/3", s, ss)
	}

	// Resolved "in next release" 1.4.2: more events from 1.4.2 do not regress.
	exec(`UPDATE issues SET status = 2, resolved_in_release = '1.4.2', resolved_in_next_release = true WHERE id = $1`, first.GetIssueId())
	if r := send(); r.GetIssueRegressed() {
		t.Fatalf("an event from the same release must not regress: %v", r)
	}
	if s, _ := status(); s != 2 {
		t.Fatalf("issue must stay resolved, status %d", s)
	}

	// Ignored until 2 more events.
	exec(`UPDATE issues SET status = 3, snooze_count = 2, snooze_base = times_seen WHERE id = $1`, first.GetIssueId())
	send()
	if s, _ := status(); s != 3 {
		t.Fatalf("one event must not end the snooze, status %d", s)
	}
	send()
	if s, ss := status(); s != 1 || ss != 2 {
		t.Fatalf("snooze must expire after 2 events, status %d/%d", s, ss)
	}
}

func TestAuthAndValidation(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()

	req := connect.NewRequest(&ingestv1.SendEventRequest{Event: orderEvent(1, "x")})
	req.Header().Set("Authorization", "Bearer izk_doesnotexistdoesnotexist00")
	if _, err := f.connect.SendEvent(ctx, req); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("unknown key: %v", err)
	}

	req = f.authed(&ingestv1.SendEventRequest{Event: orderEvent(1, "x")})
	req.Header().Set("Origin", "https://evil.example.com")
	if _, err := f.connect.SendEvent(ctx, req); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("foreign origin: %v", err)
	}
	req = f.authed(&ingestv1.SendEventRequest{Event: orderEvent(1, "x")})
	req.Header().Set("Origin", "https://kalem.example.com")
	if _, err := f.connect.SendEvent(ctx, req); err != nil {
		t.Errorf("allowed origin rejected: %v", err)
	}

	if _, err := f.connect.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: &ingestv1.Event{}})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("empty event: %v", err)
	}

	// Preflight requests are answered for any origin.
	pre, _ := http.NewRequest(http.MethodOptions, f.Server.URL+"/izci.ingest.v1.IngestService/SendEvent", nil)
	pre.Header.Set("Origin", "https://kalem.example.com")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	pre.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	res, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "https://kalem.example.com" {
		t.Errorf("preflight: %d %v", res.StatusCode, res.Header)
	}

	rel := connect.NewRequest(&ingestv1.RegisterReleaseRequest{Version: "1.5.0", CommitSha: "ABCDEF0123", Environment: "Production"})
	rel.Header().Set("Authorization", "Bearer "+f.Key)
	rresp, err := f.connect.RegisterRelease(ctx, rel)
	if err != nil || !rresp.Msg.GetCreated() {
		t.Fatalf("register release: %v %v", rresp, err)
	}

	cfgReq := connect.NewRequest(&ingestv1.GetClientConfigRequest{})
	cfgReq.Header().Set("Authorization", "Bearer "+f.Key)
	cfg, err := f.connect.GetClientConfig(ctx, cfgReq)
	if err != nil || cfg.Msg.GetProjectSlug() != "kalem" || cfg.Msg.GetMaxBatchSize() != 100 {
		t.Fatalf("client config: %v %v", cfg, err)
	}
}

func TestRateLimit(t *testing.T) {
	f := newFixture(t, 60) // 60/min → burst of 20
	ctx := context.Background()
	limited := 0
	for i := range 40 {
		_, err := f.connect.SendEvent(ctx, f.authed(&ingestv1.SendEventRequest{Event: orderEvent(i, "x")}))
		if err == nil {
			continue
		}
		var cerr *connect.Error
		if !errors.As(err, &cerr) || cerr.Code() != connect.CodeResourceExhausted {
			t.Fatalf("unexpected error: %v", err)
		}
		limited++
		found := false
		for _, d := range cerr.Details() {
			if v, derr := d.Value(); derr == nil {
				if ri, ok := v.(*errdetails.RetryInfo); ok && ri.GetRetryDelay().AsDuration() > 0 {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("rate limit errors must carry RetryInfo")
		}
	}
	if limited < 15 {
		t.Fatalf("expected most of the events beyond the burst to be limited, got %d", limited)
	}
}
