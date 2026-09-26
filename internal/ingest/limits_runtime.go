package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/store"
)

// RateLimiter enforces events-per-minute limits per API key (when the key
// sets one) and per project (project setting or instance default).
type RateLimiter struct {
	defaultPerMinute int
	keys             *expirable.LRU[uuid.UUID, *limiterEntry]
	projects         *expirable.LRU[uuid.UUID, *limiterEntry]
	mu               sync.Mutex
}

type limiterEntry struct {
	perMinute int
	lim       *rate.Limiter
}

// NewRateLimiter creates a limiter with the instance default per project.
func NewRateLimiter(defaultPerMinute int) *RateLimiter {
	return &RateLimiter{
		defaultPerMinute: defaultPerMinute,
		keys:             expirable.NewLRU[uuid.UUID, *limiterEntry](50000, nil, time.Hour),
		projects:         expirable.NewLRU[uuid.UUID, *limiterEntry](50000, nil, time.Hour),
	}
}

func (r *RateLimiter) limiter(cache *expirable.LRU[uuid.UUID, *limiterEntry], id uuid.UUID, perMinute int) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := cache.Get(id); ok && e.perMinute == perMinute {
		return e.lim
	}
	// Burst: 10 seconds worth of events, at least 20, so bursts of a crash
	// loop are absorbed while sustained floods are cut.
	burst := max(perMinute/6, 20)
	e := &limiterEntry{perMinute: perMinute, lim: rate.NewLimiter(rate.Limit(float64(perMinute)/60), burst)}
	cache.Add(id, e)
	return e.lim
}

// Allow consumes n tokens for the key and its project. When denied it
// returns how long the client should wait.
func (r *RateLimiter) Allow(rk *store.ResolvedKey, n int) (bool, time.Duration) {
	now := time.Now()
	projectLimit := int(rk.ProjectSettings.GetRateLimitPerMinute())
	if projectLimit <= 0 {
		projectLimit = r.defaultPerMinute
	}
	var reservations []*rate.Reservation
	check := func(lim *rate.Limiter) (bool, time.Duration) {
		res := lim.ReserveN(now, n)
		if !res.OK() {
			return false, time.Minute
		}
		if d := res.DelayFrom(now); d > 0 {
			res.CancelAt(now)
			return false, d
		}
		reservations = append(reservations, res)
		return true, 0
	}
	if kl := int(rk.Key.RateLimitPerMinute); kl > 0 {
		if ok, d := check(r.limiter(r.keys, rk.Key.ID, kl)); !ok {
			return false, d
		}
	}
	if projectLimit > 0 {
		if ok, d := check(r.limiter(r.projects, rk.Key.ProjectID, projectLimit)); !ok {
			for _, res := range reservations {
				res.CancelAt(now)
			}
			return false, d
		}
	}
	return true, 0
}

// QuotaTracker enforces the monthly event quota of organizations. It
// refreshes the month total from usage_daily at most once a minute and
// counts locally in between.
type QuotaTracker struct {
	db  *db.DB
	mu  sync.Mutex
	org map[uuid.UUID]*orgQuota
}

type orgQuota struct {
	month    string
	base     int64
	local    int64
	loadedAt time.Time
}

// NewQuotaTracker creates a tracker.
func NewQuotaTracker(database *db.DB) *QuotaTracker {
	return &QuotaTracker{db: database, org: map[uuid.UUID]*orgQuota{}}
}

// Allow reports whether n more events fit into the organization's monthly
// quota (limit 0 = unlimited) and reserves them.
func (q *QuotaTracker) Allow(ctx context.Context, org uuid.UUID, limit uint64, n int) bool {
	if limit == 0 {
		return true
	}
	now := time.Now().UTC()
	month := now.Format("2006-01")
	q.mu.Lock()
	e := q.org[org]
	stale := e == nil || e.month != month || now.Sub(e.loadedAt) > time.Minute
	q.mu.Unlock()
	if stale {
		var total int64
		err := q.db.InOrg(ctx, org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT coalesce(sum(accepted), 0) FROM usage_daily
				WHERE org_id = $1 AND day >= date_trunc('month', now() AT TIME ZONE 'UTC')::date`, org).Scan(&total)
		})
		if err == nil {
			q.mu.Lock()
			e = &orgQuota{month: month, base: total, loadedAt: now}
			q.org[org] = e
			q.mu.Unlock()
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if e = q.org[org]; e == nil {
		return true // fail open when usage cannot be read
	}
	if uint64(e.base+e.local+int64(n)) > limit {
		return false
	}
	e.local += int64(n)
	return true
}

// UsageRecorder accumulates rejected-event counts (rate limited, filtered)
// per organization and day and flushes them to usage_daily periodically.
// Accepted events are counted by the batch writer in the same transaction.
type UsageRecorder struct {
	mu      sync.Mutex
	pending map[usageKey]*usageDelta
}

type usageKey struct {
	org uuid.UUID
	day string
}

type usageDelta struct {
	rateLimited int64
	filtered    int64
}

// NewUsageRecorder creates a recorder.
func NewUsageRecorder() *UsageRecorder {
	return &UsageRecorder{pending: map[usageKey]*usageDelta{}}
}

// RateLimited counts events dropped by rate limits or quota.
func (u *UsageRecorder) RateLimited(org uuid.UUID, n int) { u.add(org, n, 0) }

// Filtered counts events dropped by project filters.
func (u *UsageRecorder) Filtered(org uuid.UUID, n int) { u.add(org, 0, n) }

func (u *UsageRecorder) add(org uuid.UUID, limited, filtered int) {
	k := usageKey{org, time.Now().UTC().Format("2006-01-02")}
	u.mu.Lock()
	d := u.pending[k]
	if d == nil {
		d = &usageDelta{}
		u.pending[k] = d
	}
	d.rateLimited += int64(limited)
	d.filtered += int64(filtered)
	u.mu.Unlock()
}

// Flush writes accumulated counts.
func (u *UsageRecorder) Flush(ctx context.Context, database *db.DB, log *slog.Logger) {
	u.mu.Lock()
	pending := u.pending
	u.pending = map[usageKey]*usageDelta{}
	u.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		for k, d := range pending {
			if _, err := tx.Exec(ctx, `
				INSERT INTO usage_daily AS u (org_id, day, rate_limited, filtered) VALUES ($1, $2::date, $3, $4)
				ON CONFLICT (org_id, day) DO UPDATE SET rate_limited = u.rate_limited + excluded.rate_limited,
				  filtered = u.filtered + excluded.filtered`, k.org, k.day, d.rateLimited, d.filtered); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Warn("record usage", slog.Any("err", err))
	}
}

// Dedupe remembers recently accepted client event ids per project so that
// SDK retries are acknowledged instead of stored twice.
type Dedupe struct {
	cache *expirable.LRU[dedupeKey, Result]
}

type dedupeKey struct {
	project uuid.UUID
	event   uuid.UUID
}

// NewDedupe creates a dedupe cache holding size ids for ttl.
func NewDedupe(size int, ttl time.Duration) *Dedupe {
	if size <= 0 {
		size = 100000
	}
	return &Dedupe{cache: expirable.NewLRU[dedupeKey, Result](size, nil, ttl)}
}

// Seen returns the recorded result for an event id, if any.
func (d *Dedupe) Seen(project, event uuid.UUID) (Result, bool) {
	return d.cache.Get(dedupeKey{project, event})
}

// Remember records an accepted event.
func (d *Dedupe) Remember(project, event uuid.UUID, r Result) {
	d.cache.Add(dedupeKey{project, event}, r)
}
