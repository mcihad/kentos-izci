package alert_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/alert"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func TestMatches(t *testing.T) {
	ev := &ingest.Processed{Level: ingestv1.Level_LEVEL_ERROR, Environment: "production", CustomerKey: "sivas-belediyesi",
		Unhandled: true, Tags: map[string]string{"region": "ic-anadolu"}}
	for _, tc := range []struct {
		name string
		f    store.RuleFilters
		want bool
	}{
		{"no filters", store.RuleFilters{}, true},
		{"level at minimum", store.RuleFilters{MinLevel: 4}, true},
		{"level below minimum", store.RuleFilters{MinLevel: 5}, false},
		{"environment, any case", store.RuleFilters{Environments: []string{"staging", "PRODUCTION"}}, true},
		{"other environment", store.RuleFilters{Environments: []string{"staging"}}, false},
		{"customer", store.RuleFilters{Customers: []string{"sivas-belediyesi"}}, true},
		{"other customer", store.RuleFilters{Customers: []string{"tokat-belediyesi"}}, false},
		{"tag", store.RuleFilters{Tags: map[string]string{"region": "İC-ANADOLU"}}, false},
		{"tag, same case", store.RuleFilters{Tags: map[string]string{"region": "ic-anadolu"}}, true},
		{"missing tag", store.RuleFilters{Tags: map[string]string{"module": "orders"}}, false},
		{"unhandled only", store.RuleFilters{UnhandledOnly: true}, true},
	} {
		if got := alert.Matches(tc.f, ev); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

type env struct {
	db       *db.DB
	org      uuid.UUID
	projects map[string]*store.Project
	engine   *alert.Engine
	pipeline *ingest.Pipeline
	channel  uuid.UUID
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	e := &env{db: testutil.NewDB(t), projects: map[string]*store.Project{}, engine: alert.NewEngine(time.Minute)}
	if _, err := e.db.EnsureEventPartitions(ctx, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		if err != nil {
			return err
		}
		e.org = org.ID
		for _, slug := range []string{"kalem", "bildirim"} {
			p, err := store.CreateProject(ctx, tx, org.ID, slug, slug, "go")
			if err != nil {
				return err
			}
			e.projects[slug] = p
		}
		ch, _, err := store.UpsertTelegramChannel(ctx, tx, org.ID, nil, nil, "Hatalar", store.TelegramTarget{ChatID: -1})
		e.channel = ch.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	e.pipeline = ingest.NewPipeline(e.db, slog.New(slog.DiscardHandler), ingest.PipelineConfig{Shards: 1, BatchSize: 50},
		ingest.Hooks{InTx: func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
			n, err := e.engine.Evaluate(ctx, tx, b)
			b.Notifications += n
			return err
		}})
	e.pipeline.Start()
	t.Cleanup(func() { _ = e.pipeline.Close(context.Background()) })
	return e
}

func (e *env) rule(t *testing.T, r store.AlertRule) {
	t.Helper()
	ctx := context.Background()
	r.OrgID, r.Enabled, r.ChannelIDs = e.org, true, []uuid.UUID{e.channel}
	if err := e.db.InOrg(ctx, e.org, func(tx pgx.Tx) error { _, err := store.SaveAlertRule(ctx, tx, &r); return err }); err != nil {
		t.Fatal(err)
	}
	e.engine.Invalidate(e.org)
}

// send stores an event and returns how many notifications its batch queued.
func (e *env) send(t *testing.T, project, message, release string) (ingest.Result, int) {
	t.Helper()
	p := e.projects[project]
	ev := &ingestv1.Event{Level: ingestv1.Level_LEVEL_ERROR, Environment: "production", Release: release,
		Message: &ingestv1.Message{Formatted: message}}
	processed, err := ingest.Normalize(ev, ingest.NormalizeOptions{Project: ingest.ProjectContext{
		OrgID: e.org, OrgSlug: "kentos", ProjectID: p.ID, ProjectSlug: p.Slug, Settings: p.Settings}, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	before := e.outbox(t)
	chans, err := e.pipeline.Submit(processed)
	if err != nil {
		t.Fatal(err)
	}
	r := <-chans[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return r, e.outbox(t) - before
}

func (e *env) outbox(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification_outbox`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) resolve(t *testing.T, issue uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE issues SET status = 2, resolved_at = now() WHERE id = $1`, issue)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNewIssueRegressionAndThrottle(t *testing.T) {
	e := setup(t)
	kalem := e.projects["kalem"].ID
	e.rule(t, store.AlertRule{Name: "new", Trigger: store.RuleTrigger{Kind: alert.TriggerNewIssue}, ThrottleSeconds: 1800})
	e.rule(t, store.AlertRule{Name: "regression", ProjectID: &kalem, Trigger: store.RuleTrigger{Kind: alert.TriggerRegression},
		ThrottleSeconds: 1800})

	first, n := e.send(t, "kalem", "database is down", "1.0.0")
	if n != 1 || !first.IssueNew {
		t.Fatalf("new issue: %d notifications", n)
	}
	if _, n := e.send(t, "kalem", "database is down", "1.0.0"); n != 0 {
		t.Fatalf("a known issue notified again: %d", n)
	}

	// A resolved issue that comes back notifies once, then throttles.
	e.resolve(t, first.IssueID)
	if r, n := e.send(t, "kalem", "database is down", "1.0.1"); n != 1 || !r.IssueRegressed {
		t.Fatalf("regression: %d notifications, regressed %v", n, r.IssueRegressed)
	}
	e.resolve(t, first.IssueID)
	if r, n := e.send(t, "kalem", "database is down", "1.0.2"); n != 0 || !r.IssueRegressed {
		t.Fatalf("throttled regression: %d notifications, regressed %v", n, r.IssueRegressed)
	}

	// The regression rule is scoped to kalem.
	other, n := e.send(t, "bildirim", "queue is full", "1.0.0")
	if n != 1 {
		t.Fatalf("new issue in bildirim: %d", n)
	}
	e.resolve(t, other.IssueID)
	if r, n := e.send(t, "bildirim", "queue is full", "1.0.1"); n != 0 || !r.IssueRegressed {
		t.Fatalf("project-scoped regression rule fired for another project: %d", n)
	}
}

func TestFrequencyAndIgnoredIssues(t *testing.T) {
	e := setup(t)
	e.rule(t, store.AlertRule{Name: "burst", Trigger: store.RuleTrigger{Kind: alert.TriggerFrequency, Threshold: 3, WindowSeconds: 600},
		ThrottleSeconds: 3600})
	counts := []int{}
	for range 5 {
		_, n := e.send(t, "kalem", "timeout calling SMS provider", "1.0.0")
		counts = append(counts, n)
	}
	if counts[0] != 0 || counts[1] != 0 || counts[2] != 1 || counts[3] != 0 || counts[4] != 0 {
		t.Fatalf("notifications per event: %v, want one at the threshold then throttled", counts)
	}

	// Ignored issues stay quiet.
	e2 := setup(t)
	e2.rule(t, store.AlertRule{Name: "burst", Trigger: store.RuleTrigger{Kind: alert.TriggerFrequency, Threshold: 1, WindowSeconds: 600}})
	r, n := e2.send(t, "kalem", "noisy", "1.0.0")
	if n != 1 {
		t.Fatalf("first event over threshold 1: %d", n)
	}
	ctx := context.Background()
	if err := e2.db.InSystem(ctx, func(tx pgx.Tx) error {
		return store.IgnoreIssues(ctx, tx, e2.org, []uuid.UUID{r.IssueID}, store.IgnoreOptions{})
	}); err != nil {
		t.Fatal(err)
	}
	if _, n := e2.send(t, "kalem", "noisy", "1.0.0"); n != 0 {
		t.Fatalf("ignored issue notified: %d", n)
	}
}
