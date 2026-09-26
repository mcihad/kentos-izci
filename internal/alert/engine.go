// Package alert decides which ingested events notify whom. Rules are
// evaluated inside the ingest batch transaction and matching notifications
// are written to the transactional outbox, so a notification exists exactly
// when its events were stored.
package alert

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Trigger kinds (izci.api.v1.TriggerKind).
const (
	TriggerNewIssue   int16 = 1
	TriggerRegression int16 = 2
	TriggerFrequency  int16 = 3
)

// Engine evaluates alert rules for committed batches.
type Engine struct {
	ttl time.Duration

	mu    sync.Mutex
	rules map[uuid.UUID]cachedRules
}

type cachedRules struct {
	rules   []*store.AlertRule
	expires time.Time
}

// NewEngine creates an engine that caches rules for ttl (rule changes also
// invalidate the cache explicitly).
func NewEngine(ttl time.Duration) *Engine {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Engine{ttl: ttl, rules: map[uuid.UUID]cachedRules{}}
}

// Invalidate drops the cached rules of an organization.
func (e *Engine) Invalidate(orgID uuid.UUID) {
	e.mu.Lock()
	delete(e.rules, orgID)
	e.mu.Unlock()
}

// rulesFor returns the enabled rules of organizations, loading missing ones
// through tx.
func (e *Engine) rulesFor(ctx context.Context, tx pgx.Tx, orgs []uuid.UUID) (map[uuid.UUID][]*store.AlertRule, error) {
	now := time.Now()
	out := make(map[uuid.UUID][]*store.AlertRule, len(orgs))
	var missing []uuid.UUID
	e.mu.Lock()
	for _, o := range orgs {
		if c, ok := e.rules[o]; ok && now.Before(c.expires) {
			out[o] = c.rules
		} else {
			missing = append(missing, o)
		}
	}
	e.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	loaded, err := store.ListAlertRules(ctx, tx, missing, true)
	if err != nil {
		return nil, err
	}
	fresh := map[uuid.UUID][]*store.AlertRule{}
	for _, r := range loaded {
		fresh[r.OrgID] = append(fresh[r.OrgID], r)
	}
	e.mu.Lock()
	for _, o := range missing {
		e.rules[o] = cachedRules{rules: fresh[o], expires: now.Add(e.ttl)}
		out[o] = fresh[o]
	}
	e.mu.Unlock()
	return out, nil
}

type candidate struct {
	rule  *store.AlertRule
	issue *ingest.IssueOutcome
	event *ingest.Processed
	kind  int16
}

// Evaluate writes notifications for the batch and reports how many.
func (e *Engine) Evaluate(ctx context.Context, tx pgx.Tx, b *ingest.Batch) (int, error) {
	var orgs []uuid.UUID
	for _, o := range b.Issues {
		if !slices.Contains(orgs, o.OrgID) {
			orgs = append(orgs, o.OrgID)
		}
	}
	rules, err := e.rulesFor(ctx, tx, orgs)
	if err != nil {
		return 0, err
	}

	// Rules whose trigger and filters match, before throttling.
	var cands []candidate
	for _, o := range b.Issues {
		if o.Status != store.IssueUnresolved {
			continue
		}
		for _, r := range rules[o.OrgID] {
			if r.ProjectID != nil && *r.ProjectID != o.ProjectID || len(r.ChannelIDs) == 0 {
				continue
			}
			var kind int16
			events := o.Events
			switch r.Trigger.Kind {
			case TriggerNewIssue:
				// With an environment filter "new" means new in that
				// environment: an error that reached production after
				// staging is news for a production rule.
				if len(r.Filters.Environments) > 0 {
					events = eventsIn(o.Events, o.NewEnvironments)
					if len(events) == 0 {
						continue
					}
				} else if !o.Created {
					continue
				}
				kind = store.NotifyNewIssue
			case TriggerRegression:
				if !o.Regressed {
					continue
				}
				kind = store.NotifyRegression
			case TriggerFrequency:
				if r.Trigger.Threshold <= 0 || r.Trigger.WindowSeconds <= 0 {
					continue
				}
				kind = store.NotifyFrequency
			default:
				continue
			}
			if ev := matchingEvent(r.Filters, events); ev != nil {
				cands = append(cands, candidate{rule: r, issue: o, event: ev, kind: kind})
			}
		}
	}
	if len(cands) == 0 {
		return 0, nil
	}

	cands, err = e.dropThrottled(ctx, tx, cands)
	if err != nil {
		return 0, err
	}
	cands, err = e.checkFrequency(ctx, tx, cands)
	if err != nil {
		return 0, err
	}

	var items []store.OutboxItem
	var fired []uuid.UUID
	for _, c := range cands {
		payload := map[string]any{"event_id": c.event.ID.String(), "rule": c.rule.Name}
		if c.kind == store.NotifyFrequency {
			payload["threshold"] = c.rule.Trigger.Threshold
			payload["window_seconds"] = c.rule.Trigger.WindowSeconds
		}
		for _, ch := range c.rule.ChannelIDs {
			items = append(items, store.OutboxItem{
				OrgID: c.issue.OrgID, ChannelID: ch, RuleID: &c.rule.ID, IssueID: &c.issue.ID, Kind: c.kind, Payload: payload,
			})
		}
		if !slices.Contains(fired, c.rule.ID) {
			fired = append(fired, c.rule.ID)
		}
	}
	if err := store.EnqueueNotifications(ctx, tx, items); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE alert_rules SET last_triggered_at = now() WHERE id = ANY($1)`, fired); err != nil {
		return 0, err
	}
	return len(items), nil
}

// dropThrottled removes candidates whose rule already notified about the
// issue within the rule's throttle window.
func (e *Engine) dropThrottled(ctx context.Context, tx pgx.Tx, cands []candidate) ([]candidate, error) {
	var ruleIDs, issueIDs []uuid.UUID
	longest := 0
	for _, c := range cands {
		ruleIDs = append(ruleIDs, c.rule.ID)
		issueIDs = append(issueIDs, c.issue.ID)
		longest = max(longest, int(c.rule.ThrottleSeconds))
	}
	if longest == 0 {
		return cands, nil
	}
	recent, err := store.RecentlyNotified(ctx, tx, ruleIDs, issueIDs, time.Now().Add(-time.Duration(longest)*time.Second))
	if err != nil {
		return nil, err
	}
	out := cands[:0]
	for _, c := range cands {
		at, ok := recent[[2]uuid.UUID{c.rule.ID, c.issue.ID}]
		if ok && time.Since(at) < time.Duration(c.rule.ThrottleSeconds)*time.Second {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// checkFrequency keeps frequency candidates whose issue reached the rule's
// threshold within its window (events of this batch included).
func (e *Engine) checkFrequency(ctx context.Context, tx pgx.Tx, cands []candidate) ([]candidate, error) {
	byWindow := map[int][]uuid.UUID{}
	for _, c := range cands {
		if c.kind == store.NotifyFrequency && !slices.Contains(byWindow[c.rule.Trigger.WindowSeconds], c.issue.ID) {
			byWindow[c.rule.Trigger.WindowSeconds] = append(byWindow[c.rule.Trigger.WindowSeconds], c.issue.ID)
		}
	}
	if len(byWindow) == 0 {
		return cands, nil
	}
	counts := map[int]map[uuid.UUID]int64{}
	for window, ids := range byWindow {
		since := time.Now().Add(-time.Duration(window) * time.Second)
		rows, err := tx.Query(ctx, `SELECT issue_id, count(*) FROM events WHERE issue_id = ANY($1) AND received_at >= $2 GROUP BY 1`, ids, since)
		if err != nil {
			return nil, err
		}
		m := map[uuid.UUID]int64{}
		for rows.Next() {
			var id uuid.UUID
			var n int64
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			m[id] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		counts[window] = m
	}
	out := cands[:0]
	for _, c := range cands {
		if c.kind == store.NotifyFrequency && counts[c.rule.Trigger.WindowSeconds][c.issue.ID] < int64(c.rule.Trigger.Threshold) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// eventsIn keeps the events of the given environments.
func eventsIn(events []*ingest.Processed, envs []string) []*ingest.Processed {
	var out []*ingest.Processed
	for _, e := range events {
		if containsFold(envs, e.Environment) {
			out = append(out, e)
		}
	}
	return out
}

// matchingEvent returns the newest event of the issue that passes the
// filters, or nil.
func matchingEvent(f store.RuleFilters, events []*ingest.Processed) *ingest.Processed {
	for i := len(events) - 1; i >= 0; i-- {
		if Matches(f, events[i]) {
			return events[i]
		}
	}
	return nil
}

// Matches reports whether an event passes rule filters.
func Matches(f store.RuleFilters, p *ingest.Processed) bool {
	if f.MinLevel > 0 && int16(p.Level) < f.MinLevel {
		return false
	}
	if f.UnhandledOnly && !p.Unhandled {
		return false
	}
	if len(f.Environments) > 0 && !containsFold(f.Environments, p.Environment) {
		return false
	}
	if len(f.Customers) > 0 && !containsFold(f.Customers, p.CustomerKey) {
		return false
	}
	for k, v := range f.Tags {
		if !strings.EqualFold(p.Tags[k], v) {
			return false
		}
	}
	return true
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
