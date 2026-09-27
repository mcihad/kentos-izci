package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/app"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/selfmon"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// TestMetricsAndSelfMonitoring runs the assembled application: an event
// sent over Connect shows up in /metrics, and error logs of izci itself are
// reported to a project through the Go SDK, rate limited.
func TestMetricsAndSelfMonitoring(t *testing.T) {
	ctx := context.Background()
	database := testutil.NewDB(t)
	now := time.Now()
	if _, err := database.EnsureEventPartitions(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	var org *store.Organization
	var project *store.Project
	var key string
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if org, err = store.CreateOrganization(ctx, tx, "kentos", "KentOS"); err != nil {
			return err
		}
		if project, err = store.CreateProject(ctx, tx, org.ID, "izci", "İzci", "go"); err != nil {
			return err
		}
		_, key, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: org.ID, ProjectID: project.ID, Name: "self",
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Env: config.EnvTest, PublicURL: "http://izci.test", SecretKey: make([]byte, 32), DataDir: t.TempDir(),
		HTTP:      config.HTTPConfig{Addr: "127.0.0.1:0", ShutdownTimeout: 5 * time.Second},
		Retention: config.RetentionConfig{Days: 30, PremakeDays: 2},
		Ingest: config.IngestConfig{Shards: 2, QueueSize: 100, BatchSize: 100, Ack: "durable", AckTimeout: 10 * time.Second,
			RateLimitPerMinute: 100000, DedupeSize: 1000},
		Auth:    config.AuthConfig{Signup: "invite", SessionTTL: time.Hour, PasswordLogin: true, InvitationTTL: time.Hour},
		AI:      config.AIConfig{MaxConcurrent: 1, Timeout: time.Minute},
		Metrics: config.MetricsConfig{Enabled: true},
	}
	log := slog.New(slog.DiscardHandler)
	a, err := app.New(cfg, log, database, app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a.Pipeline.Start()
	t.Cleanup(func() { _ = a.Pipeline.Close(context.Background()) })
	srv := httptest.NewServer(a.Server.Handler())
	t.Cleanup(srv.Close)

	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, srv.URL)
	req := connect.NewRequest(&ingestv1.SendEventRequest{Event: &ingestv1.Event{
		Message: &ingestv1.Message{Formatted: "metrics smoke test"},
	}})
	req.Header().Set("Authorization", "Bearer "+key)
	if _, err := client.SendEvent(ctx, req); err != nil {
		t.Fatal(err)
	}

	body := get(t, srv.URL+"/metrics", nil, http.StatusOK)
	for _, series := range []string{
		`izci_ingest_events_total{outcome="accepted"}`,
		`izci_ingest_stored_events_total`,
		`izci_http_requests_total{handler="ingest",code="200"}`,
	} {
		if value(t, body, series) < 1 {
			t.Errorf("%s did not count the event", series)
		}
	}
	if got := value(t, body, "izci_ingest_queue_capacity"); got != 200 {
		t.Errorf("queue capacity %v, want 200", got)
	}
	value(t, body, `izci_db_pool_connections{state="idle"}`)
	value(t, body, "izci_ai_runs_running")
	// Through a reverse proxy the endpoint stays closed.
	get(t, srv.URL+"/metrics", http.Header{"X-Forwarded-For": {"203.0.113.9"}}, http.StatusForbidden)

	// Self-monitoring: izci's error logs become an issue of the project.
	cfg.Self = config.SelfConfig{URL: srv.URL, Key: config.Secret(key), Environment: "test"}
	mon, err := selfmon.Start(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		mon.Logger().Error("store batch", slog.Any("err", errors.New("disk quota exceeded")))
	}
	mon.Close(10 * time.Second)

	var seen int64
	var release string
	err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT times_seen, (SELECT release FROM events e WHERE e.issue_id = i.id LIMIT 1)
			FROM issues i WHERE project_id = $1 AND value = 'disk quota exceeded'`, project.ID).Scan(&seen, &release)
	})
	if err != nil {
		t.Fatalf("self-reported issue: %v", err)
	}
	// The limiter lets a burst of 60 through, plus what refills meanwhile.
	if seen < 60 || seen > 62 {
		t.Errorf("self-monitoring sent %d of 100 reports, want about 60", seen)
	}
	if release != "izci@dev" {
		t.Errorf("release %q", release)
	}
}

func get(t *testing.T, url string, header http.Header, status int) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("GET %s: %d, want %d\n%s", url, resp.StatusCode, status, b)
	}
	return string(b)
}

// value returns the sample of one series in a metrics page.
func value(t *testing.T, body, series string) float64 {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(series) + ` (\S+)$`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("metrics lack %s", series)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
