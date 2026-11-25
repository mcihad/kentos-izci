// Package demo fills an organization with realistic data: several projects
// of the KentOS family, releases rolling out over time, errors spread over
// days with working-hour peaks and a spike, and a few triaged issues.
package demo

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/sample"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Options configure Seed.
type Options struct {
	OrgSlug string
	OrgName string
	Events  int
	Days    int
	Seed    uint64
}

// Summary reports what Seed created.
type Summary struct {
	Projects []string
	Events   int
	Issues   int
	Elapsed  time.Duration
}

type projectSpec struct {
	slug, name, platform string
	templates            []int
	weight               int
}

var projects = []projectSpec{
	{"kalem", "KentOS Kalem", "dotnet", []int{0, 1}, 40},
	{"platform", "KentOS Platform", "node", []int{3}, 25},
	{"bildirim", "KentOS Bildirim", "dotnet", []int{2, 5}, 20},
	{"kalem-web", "KentOS Kalem Web", "javascript", []int{4}, 15},
}

// Seed creates missing projects and writes opts.Events events through the
// regular normalization and batch writer.
func Seed(ctx context.Context, database *db.DB, log *slog.Logger, opts Options) (Summary, error) {
	start := time.Now()
	if opts.Events <= 0 {
		opts.Events = 5000
	}
	if opts.Days <= 0 {
		opts.Days = 14
	}
	if opts.OrgName == "" {
		opts.OrgName = "KentOS"
	}
	r := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))

	if _, err := database.EnsureEventPartitions(ctx, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 2)); err != nil {
		return Summary{}, err
	}

	var org *store.Organization
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		org, err = store.GetOrganizationBySlug(ctx, tx, opts.OrgSlug)
		if err == store.ErrNotFound {
			org, err = store.CreateOrganization(ctx, tx, opts.OrgSlug, opts.OrgName)
		}
		return err
	})
	if err != nil {
		return Summary{}, fmt.Errorf("organization: %w", err)
	}

	ctxs := make([]ingest.ProjectContext, len(projects))
	var summary Summary
	err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		for i, spec := range projects {
			p, err := store.GetProjectBySlug(ctx, tx, org.ID, spec.slug)
			if err == store.ErrNotFound {
				p, err = store.CreateProject(ctx, tx, org.ID, spec.slug, spec.name, spec.platform)
				if err == nil {
					_, _, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: org.ID, ProjectID: p.ID, Name: "demo",
						Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}})
				}
			}
			if err != nil {
				return fmt.Errorf("project %s: %w", spec.slug, err)
			}
			ctxs[i] = ingest.ProjectContext{OrgID: org.ID, OrgSlug: org.Slug, ProjectID: p.ID, ProjectSlug: p.Slug,
				Platform: p.Platform, Settings: p.Settings}
			summary.Projects = append(summary.Projects, p.Slug)
		}
		return nil
	})
	if err != nil {
		return summary, err
	}

	pipeline := ingest.NewPipeline(database, log, ingest.PipelineConfig{Shards: 4, BatchSize: 500, QueueSize: 20000}, ingest.Hooks{})
	pipeline.Start()
	defer pipeline.Close(context.Background())

	totalWeight := 0
	for _, p := range projects {
		totalWeight += p.weight
	}
	now := time.Now()
	spikeDay := opts.Days / 3
	var pending []<-chan ingest.Result
	for i := 0; i < opts.Events; i++ {
		pi := pickProject(r, totalWeight)
		spec := projects[pi]
		tmpl := spec.templates[r.IntN(len(spec.templates))]
		ts := timestamp(r, now, opts.Days, spikeDay, tmpl == 2)
		release := releaseAt(ts, now, opts.Days)
		ev := sample.Event(r, sample.Options{Template: tmpl, Release: release, Timestamp: ts})
		p, err := ingest.Normalize(ev, ingest.NormalizeOptions{Project: ctxs[pi], Retention: time.Duration(opts.Days+2) * 24 * time.Hour})
		if err != nil {
			return summary, err
		}
		chans, err := pipeline.Submit(p)
		for err == ingest.ErrOverloaded {
			time.Sleep(20 * time.Millisecond)
			chans, err = pipeline.Submit(p)
		}
		if err != nil {
			return summary, err
		}
		pending = append(pending, chans...)
		if len(pending) >= 5000 {
			if err := wait(pending); err != nil {
				return summary, err
			}
			pending = pending[:0]
		}
	}
	if err := wait(pending); err != nil {
		return summary, err
	}
	summary.Events = opts.Events

	// A little triage history so the UI has every state to show.
	err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE issues SET status = 2, resolved_at = now() - interval '2 days'
			WHERE id = (SELECT id FROM issues WHERE org_id = $1 ORDER BY times_seen ASC LIMIT 1)`, org.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE issues SET priority = 4
			WHERE id = (SELECT id FROM issues WHERE org_id = $1 AND level = 5 LIMIT 1)`, org.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM issues WHERE org_id = $1`, org.ID).Scan(&summary.Issues)
	})
	summary.Elapsed = time.Since(start)
	return summary, err
}

func wait(chans []<-chan ingest.Result) error {
	for _, ch := range chans {
		if r := <-ch; r.Err != nil {
			return r.Err
		}
	}
	return nil
}

func pickProject(r *rand.Rand, total int) int {
	n := r.IntN(total)
	for i, p := range projects {
		if n < p.weight {
			return i
		}
		n -= p.weight
	}
	return 0
}

// timestamp spreads events over the last days with more traffic during
// working hours (Europe/Istanbul) and a spike of the SMS timeout issue.
func timestamp(r *rand.Rand, now time.Time, days, spikeDay int, spiky bool) time.Time {
	for {
		offset := time.Duration(r.Float64() * float64(time.Duration(days)*24*time.Hour))
		t := now.Add(-offset)
		if spiky && r.IntN(3) == 0 {
			// Two-hour outage of the SMS provider.
			base := now.AddDate(0, 0, -spikeDay).Truncate(time.Hour)
			return base.Add(time.Duration(r.IntN(120)) * time.Minute)
		}
		hour := t.UTC().Add(3 * time.Hour).Hour() // Istanbul
		weight := 0.15
		if hour >= 8 && hour <= 18 {
			weight = 1
		}
		if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			weight *= 0.3
		}
		if r.Float64() < weight {
			return t
		}
	}
}

// releaseAt rolls releases out over the period: 1.4.0 → 1.4.1 → 1.4.2.
func releaseAt(ts, now time.Time, days int) string {
	age := now.Sub(ts).Hours() / 24
	switch {
	case age > float64(days)*2/3:
		return "1.4.0"
	case age > float64(days)/4:
		return "1.4.1"
	default:
		return "1.4.2"
	}
}
