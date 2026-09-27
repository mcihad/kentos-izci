package sentry_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/store"
)

// TestSDKEnvelopesEndToEnd replays every recorded SDK envelope against the
// envelope endpoint and checks what izci stored: events, attachments, user
// feedback (linked to issues) and release health.
func TestSDKEnvelopesEndToEnd(t *testing.T) {
	e := newEnv(t, 0)
	ctx := context.Background()
	files, _ := filepath.Glob(filepath.Join("testdata", "sdk", "*.envelope"))
	sort.Strings(files)
	url := fmt.Sprintf("%s/api/%d/envelope/", e.Server.URL, e.Project.PublicID)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		res := post(t, url, "application/x-sentry-envelope", "", body, map[string]string{
			"X-Sentry-Auth": "Sentry sentry_version=7, sentry_key=" + e.Key,
		})
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: %d %s", filepath.Base(f), res.StatusCode, msg)
		}
	}
	e.Service.SessionRecorder().Flush(ctx, e.DB, slog.New(slog.DiscardHandler))
	if err := e.DB.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.LinkFeedback(ctx, tx, time.Now().Add(-24*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		e.Scalar(t, &n, sql, args...)
		return n
	}
	// Events are written in batches after the response; wait for them.
	deadline := time.Now().Add(10 * time.Second)
	for count(`SELECT count(*) FROM events`) < 21 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := count(`SELECT count(*) FROM events`); n != 21 {
		t.Errorf("stored %d events, want 21 (every event item of the fixtures)", n)
	}
	if n := count(`SELECT count(*) FROM event_attachments`); n != 4 {
		t.Errorf("stored %d attachments, want 4", n)
	}
	if n := count(`SELECT count(*) FROM event_attachments a WHERE EXISTS (
		SELECT 1 FROM events e WHERE e.project_id = a.project_id AND e.event_id = a.event_id)`); n != 4 {
		t.Errorf("%d attachments are linked to a stored event, want 4", n)
	}
	if n := count(`SELECT count(*) FROM user_feedback`); n != 4 {
		t.Errorf("stored %d feedback messages, want 4", n)
	}
	// Every feedback names an event that the SDK sent too.
	if n := count(`SELECT count(*) FROM user_feedback WHERE issue_id IS NOT NULL`); n != 4 {
		t.Errorf("%d feedback messages are linked to an issue, want 4", n)
	}
	if n := count(`SELECT count(*) FROM user_feedback WHERE message = 'Ödeme sayfası dondu, iki kez denedim.' AND email = 'ayse@example.com'`); n != 1 {
		t.Error("the .NET feedback was not stored as sent")
	}

	type health struct{ started, errored, crashed int }
	byRelease := map[string]health{}
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT release, sum(started), sum(errored), sum(crashed) FROM release_sessions GROUP BY release`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r string
			var h health
			if err := rows.Scan(&r, &h.started, &h.errored, &h.crashed); err != nil {
				return err
			}
			byRelease[r] = h
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	// .NET: two runs (the second crashes); the browser page started one
	// session; Node reported one.
	if h := byRelease["kalem@2.0.0"]; h.started != 2 || h.crashed != 1 || h.errored < 2 {
		t.Errorf(".NET release health: %+v", h)
	}
	if h := byRelease["kalem-web@5.0.0"]; h.started != 1 || h.errored != 1 {
		t.Errorf("browser release health: %+v", h)
	}
	if h := byRelease["platform@4.2.0"]; h.started != 1 {
		t.Errorf("Node release health: %+v", h)
	}
	if n := count(`SELECT count(*) FROM releases WHERE version = 'kalem-web@5.0.0'`); n != 1 {
		t.Error("a release that reported sessions must be listed")
	}
	if n := count(`SELECT count(DISTINCT user_hash) FROM release_session_users WHERE release = 'kalem@2.0.0'`); n != 1 {
		t.Errorf("the .NET runs share one installation id; distinct users = %d", n)
	}

	// Replaying the same envelopes must not count sessions or feedback twice.
	for _, f := range files {
		body, _ := os.ReadFile(f)
		res := post(t, url, "application/x-sentry-envelope", "", body, map[string]string{"X-Sentry-Auth": "Sentry sentry_key=" + e.Key})
		res.Body.Close()
	}
	e.Service.SessionRecorder().Flush(ctx, e.DB, slog.New(slog.DiscardHandler))
	if n := count(`SELECT sum(started) FROM release_sessions WHERE release = 'kalem@2.0.0'`); n != 2 {
		t.Errorf("retried sessions counted again: %d", n)
	}
	if n := count(`SELECT count(*) FROM user_feedback`); n != 4 {
		t.Errorf("retried feedback stored again: %d", n)
	}
}

// TestAttachmentsFollowTheEvent: files of a rejected event are dropped, and
// projects can discard attachments altogether.
func TestAttachmentsFollowTheEvent(t *testing.T) {
	e := newEnv(t, 0)
	url := fmt.Sprintf("%s/api/%d/envelope/", e.Server.URL, e.Project.PublicID)
	auth := map[string]string{"X-Sentry-Auth": "Sentry sentry_key=" + e.Key}
	body := func(id, env string) []byte {
		return []byte(`{"event_id":"` + id + `"}
{"type":"event"}
{"event_id":"` + id + `","environment":"` + env + `","exception":{"values":[{"type":"E","value":"x"}]}}
{"type":"attachment","length":5,"filename":"a.txt","content_type":"text/plain"}
hello
`)
	}
	expectOK(t, post(t, url, "application/x-sentry-envelope", "", body("11111111111111111111111111111111", "production"), auth))

	// Events of ignored environments are filtered: their files too.
	settings := e.Project.Settings
	settings.IgnoredEnvironments = []string{"development"}
	if err := e.DB.InOrg(context.Background(), e.Org.ID, func(tx pgx.Tx) error {
		_, err := store.UpdateProject(context.Background(), tx, e.Project.ID, e.Project.Name, e.Project.Platform, settings)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.Service.Keys().InvalidateAll()
	expectOK(t, post(t, url, "application/x-sentry-envelope", "", body("22222222222222222222222222222222", "development"), auth))

	var n int
	e.Scalar(t, &n, `SELECT count(*) FROM event_attachments`)
	if n != 1 {
		t.Fatalf("stored %d attachments, want only the accepted event's", n)
	}
}
