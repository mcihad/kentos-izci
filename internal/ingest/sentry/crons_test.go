package sentry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
)

type envState struct {
	status   string
	failures int
	issue    *uuid.UUID
	next     *time.Time
}

func monitorEnv(t *testing.T, e *ingesttest.Env, slug, env string) envState {
	t.Helper()
	ctx := context.Background()
	var s envState
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT me.status, me.failures, me.issue_id, me.next_checkin_at
			FROM monitor_environments me JOIN monitors m ON m.id = me.monitor_id
			WHERE m.slug = $1 AND me.environment = $2`, slug, env).Scan(&s.status, &s.failures, &s.issue, &s.next)
	})
	if err != nil {
		t.Fatalf("monitor environment %s/%s: %v", slug, env, err)
	}
	return s
}

func issueState(t *testing.T, e *ingesttest.Env, id uuid.UUID) (status int, title string, events int64) {
	t.Helper()
	ctx := context.Background()
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, title, times_seen FROM issues WHERE id = $1`, id).Scan(&status, &title, &events)
	})
	if err != nil {
		t.Fatal(err)
	}
	return status, title, events
}

// TestCronCheckInsFromSentryGoSDK drives a monitor with the official Go SDK:
// the first check-in creates the monitor from monitor_config, the closing
// one finishes the run with the measured duration.
func TestCronCheckInsFromSentryGoSDK(t *testing.T) {
	e := newEnv(t, 0)
	host := strings.TrimPrefix(e.Server.URL, "http://")
	client, err := sentrygo.NewClient(sentrygo.ClientOptions{
		Dsn: fmt.Sprintf("http://%s@%s/%d", e.Key, host, e.Project.PublicID), Environment: "production",
		Release: "rapor@1.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	hub := sentrygo.NewHub(client, sentrygo.NewScope())
	cfg := &sentrygo.MonitorConfig{Schedule: sentrygo.CrontabSchedule("0 3 * * *"), CheckInMargin: 5, MaxRuntime: 45,
		Timezone: "Europe/Istanbul", FailureIssueThreshold: 2, RecoveryThreshold: 1}
	id := hub.CaptureCheckIn(&sentrygo.CheckIn{MonitorSlug: "gunluk-rapor", Status: sentrygo.CheckInStatusInProgress}, cfg)
	if id == nil || !client.Flush(5*time.Second) {
		t.Fatal("in-progress check-in not sent")
	}
	hub.CaptureCheckIn(&sentrygo.CheckIn{ID: *id, MonitorSlug: "gunluk-rapor", Status: sentrygo.CheckInStatusOK,
		Duration: 90 * time.Second}, cfg)
	if !client.Flush(5 * time.Second) {
		t.Fatal("closing check-in not sent")
	}

	var (
		crontab, tz        string
		margin, maxRuntime int
		threshold          int
	)
	ctx := context.Background()
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT schedule_crontab, timezone, checkin_margin, max_runtime, failure_threshold
			FROM monitors WHERE slug = 'gunluk-rapor'`).Scan(&crontab, &tz, &margin, &maxRuntime, &threshold)
	})
	if err != nil {
		t.Fatalf("monitor not created: %v", err)
	}
	if crontab != "0 3 * * *" || tz != "Europe/Istanbul" || margin != 5 || maxRuntime != 45 || threshold != 2 {
		t.Fatalf("config: %q %q %d %d %d", crontab, tz, margin, maxRuntime, threshold)
	}
	var (
		status, release string
		runs            int
		duration        int
	)
	e.Scalar(t, &runs, `SELECT count(*) FROM monitor_checkins`)
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, duration_ms, release FROM monitor_checkins WHERE check_in_id = $1`,
			uuid.UUID(uuidFromEventID(t, string(*id)))).Scan(&status, &duration, &release)
	})
	if err != nil {
		t.Fatal(err)
	}
	if runs != 1 || status != "ok" || duration != 90_000 || release != "rapor@1.2.0" {
		t.Fatalf("run: %d runs, %s, %dms, %q", runs, status, duration, release)
	}
	s := monitorEnv(t, e, "gunluk-rapor", "production")
	// 03:00 in Istanbul is 00:00 UTC.
	if s.status != "ok" || s.next == nil || s.next.UTC().Hour() != 0 || s.next.UTC().Minute() != 0 {
		t.Fatalf("environment: %+v", s)
	}
}

func uuidFromEventID(t *testing.T, id string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("check-in id %q: %v", id, err)
	}
	return u
}

// TestCronHTTPEndpoint covers Sentry's HTTP check-in endpoint: monitor
// upsert from the body, the failure threshold, the issue it raises and its
// resolution when the job recovers.
func TestCronHTTPEndpoint(t *testing.T) {
	e := newEnv(t, 0)
	call := func(method, path, body string, hdr map[string]string) (int, map[string]string) {
		t.Helper()
		r, err := http.NewRequest(method, fmt.Sprintf("%s/api/%d/cron/%s", e.Server.URL, e.Project.PublicID, path),
			strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		out := map[string]string{}
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	key := e.Key

	// An unknown monitor needs a config.
	if code, body := call("POST", "yedekleme/"+key+"/?status=ok", "", nil); code != http.StatusNotFound ||
		!strings.Contains(body["detail"], "monitor_config") {
		t.Fatalf("unknown monitor: %d %v", code, body)
	}
	if code, _ := call("POST", "yedekleme/"+key+"/?status=done", "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", code)
	}
	if code, _ := call("GET", "yedekleme/"+key+"/", "", nil); code != http.StatusBadRequest {
		t.Fatalf("missing status: %d", code)
	}
	if code, _ := call("POST", "yedekleme/izk_wrong/?status=ok", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	config := `{"monitor_config":{"schedule":{"type":"interval","value":1,"unit":"hour"},"failure_issue_threshold":2}}`
	if code, body := call("POST", "yedekleme/"+key+"/?status=bogus", config, nil); code != http.StatusBadRequest {
		t.Fatalf("bad status with config: %d %v", code, body)
	}

	// First failure: counted, no issue yet (threshold 2).
	code, body := call("POST", "yedekleme/"+key+"/?status=error&environment=Production", config, nil)
	if code != http.StatusAccepted || len(body["id"]) != 32 {
		t.Fatalf("first failure: %d %v", code, body)
	}
	if s := monitorEnv(t, e, "yedekleme", "production"); s.status != "error" || s.failures != 1 || s.issue != nil {
		t.Fatalf("after one failure: %+v", s)
	}
	// Second failure through the Sentry auth header raises the issue.
	code, _ = call("POST", "yedekleme/?status=error&environment=production", "",
		map[string]string{"X-Sentry-Auth": "Sentry sentry_version=7, sentry_key=" + key})
	if code != http.StatusAccepted {
		t.Fatalf("second failure: %d", code)
	}
	s := monitorEnv(t, e, "yedekleme", "production")
	if s.failures != 2 || s.issue == nil {
		t.Fatalf("after two failures: %+v", s)
	}
	status, title, events := issueState(t, e, *s.issue)
	if status != 1 || title != "Zamanlanmış görev başarısız: yedekleme" || events != 1 {
		t.Fatalf("issue: %d %q %d", status, title, events)
	}
	var tag string
	e.Scalar(t, &tag, `SELECT value FROM issue_tags WHERE key = 'monitor.status' LIMIT 1`)
	if tag != "error" {
		t.Fatalf("monitor.status tag: %q", tag)
	}
	// A third failure adds an event to the same issue.
	call("POST", "yedekleme/"+key+"/?status=error", "", nil)
	if _, _, events := issueState(t, e, *s.issue); events != 2 {
		t.Fatalf("third failure did not reach the issue: %d events", events)
	}

	// A run that starts and succeeds resolves it (recovery threshold 1). The
	// closing check-in has no id: it finishes the open run.
	code, body = call("POST", "yedekleme/"+key+"/?status=in_progress", "", nil)
	if code != http.StatusAccepted {
		t.Fatalf("in progress: %d", code)
	}
	runID := body["id"]
	code, body = call("POST", "yedekleme/"+key+"/?status=ok&duration=2.5", "", nil)
	if code != http.StatusAccepted || body["id"] != runID {
		t.Fatalf("closing check-in: %d %v (run %s)", code, body, runID)
	}
	s = monitorEnv(t, e, "yedekleme", "production")
	if s.status != "ok" || s.issue != nil || s.failures != 0 {
		t.Fatalf("after recovery: %+v", s)
	}
	if status, _, _ := issueState(t, e, *monitorIssue(t, e)); status != 2 {
		t.Fatalf("issue not resolved: %d", status)
	}
	var duration int
	e.Scalar(t, &duration, `SELECT duration_ms FROM monitor_checkins WHERE replace(check_in_id::text, '-', '') = $1`, runID)
	if duration != 2500 {
		t.Fatalf("duration: %d", duration)
	}
	var activity string
	e.Scalar(t, &activity, `SELECT data->>'reason' FROM issue_activity WHERE kind = 3 ORDER BY created_at DESC LIMIT 1`)
	if activity != "monitor_recovered" {
		t.Fatalf("resolution activity: %q", activity)
	}
}

func monitorIssue(t *testing.T, e *ingesttest.Env) *uuid.UUID {
	t.Helper()
	var id uuid.UUID
	e.Scalar(t, &id, `SELECT id FROM issues WHERE title LIKE 'Zamanlanmış görev başarısız:%' LIMIT 1`)
	return &id
}

// TestCronSweep checks what izci records on its own: runs that never checked
// in and runs that never finished.
func TestCronSweep(t *testing.T) {
	e := newEnv(t, 0)
	ctx := context.Background()
	post := func(path string, body string) {
		t.Helper()
		res, err := http.Post(fmt.Sprintf("%s/api/%d/cron/%s", e.Server.URL, e.Project.PublicID, path), "application/json",
			strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("check-in %s: %d", path, res.StatusCode)
		}
	}
	post("gece-aktarim/"+e.Key+"/?status=ok",
		`{"monitor_config":{"schedule":{"type":"crontab","value":"*/10 * * * *"},"checkin_margin":2,"max_runtime":30}}`)

	// Nothing is overdue yet.
	if err := e.Monitors.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var missed int
	e.Scalar(t, &missed, `SELECT count(*) FROM monitor_checkins WHERE status = 'missed'`)
	if missed != 0 {
		t.Fatalf("premature missed runs: %d", missed)
	}

	// The expected run is long overdue.
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE monitor_environments SET next_checkin_at = now() - interval '1 hour',
			next_checkin_latest = now() - interval '58 minutes'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Monitors.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	// The next expected run may have started a moment ago: it is still
	// ahead as long as its check-in margin (2 minutes) has not passed.
	s := monitorEnv(t, e, "gece-aktarim", "production")
	if s.status != "missed" || s.failures != 1 || s.issue == nil || s.next == nil || !s.next.Add(2*time.Minute).After(time.Now()) {
		t.Fatalf("after missed run: %+v", s)
	}
	e.Scalar(t, &missed, `SELECT count(*) FROM monitor_checkins WHERE status = 'missed'`)
	if missed != 1 {
		t.Fatalf("missed runs: %d", missed)
	}
	var tag string
	e.Scalar(t, &tag, `SELECT value FROM issue_tags WHERE key = 'monitor.status' LIMIT 1`)
	if tag != "missed" {
		t.Fatalf("monitor.status tag: %q", tag)
	}
	// A second sweep does not count the same slot again.
	if err := e.Monitors.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	e.Scalar(t, &missed, `SELECT count(*) FROM monitor_checkins WHERE status = 'missed'`)
	if missed != 1 {
		t.Fatalf("missed runs after a second sweep: %d", missed)
	}

	// A run that started two hours ago and never finished times out.
	post("gece-aktarim/"+e.Key+"/?status=in_progress&check_in_id=11111111-2222-3333-4444-555555555555", "")
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE monitor_checkins SET created_at = now() - interval '2 hours'
			WHERE check_in_id = '11111111-2222-3333-4444-555555555555'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Monitors.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var duration int
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, duration_ms FROM monitor_checkins
			WHERE check_in_id = '11111111-2222-3333-4444-555555555555'`).Scan(&status, &duration)
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != "timeout" || duration != 30*60_000 {
		t.Fatalf("timed out run: %s %d", status, duration)
	}
	if s := monitorEnv(t, e, "gece-aktarim", "production"); s.status != "timeout" || s.failures != 2 {
		t.Fatalf("after timeout: %+v", s)
	}
	// A late "ok" for the timed out run changes nothing.
	post("gece-aktarim/"+e.Key+"/?status=ok&check_in_id=11111111-2222-3333-4444-555555555555", "")
	if s := monitorEnv(t, e, "gece-aktarim", "production"); s.status != "timeout" {
		t.Fatalf("late ok changed the state: %+v", s)
	}
}
