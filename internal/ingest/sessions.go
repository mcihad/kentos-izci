package ingest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Session statuses reported by Sentry SDKs.
const (
	SessionOK       = "ok"
	SessionExited   = "exited"
	SessionCrashed  = "crashed"
	SessionAbnormal = "abnormal"
	SessionErrored  = "errored"
)

// Sessions older than this are dropped, like Sentry does: release health
// looks at recent adoption, and late updates would rewrite settled hours.
const maxSessionAge = 5 * 24 * time.Hour

// SessionUpdate is one update of a single session (Sentry "session" item):
// an app run, a page visit or a request.
type SessionUpdate struct {
	SID        string
	DistinctID string
	// Init marks the first update of the session.
	Init        bool
	Started     time.Time
	Status      string
	Errors      int
	Release     string
	Environment string
}

// SessionAggregate is a bucket of finished sessions (Sentry "sessions"
// item), sent by server SDKs that track one session per request.
type SessionAggregate struct {
	Started     time.Time
	DistinctID  string
	Exited      int
	Errored     int
	Abnormal    int
	Crashed     int
	Release     string
	Environment string
}

type sessionKey struct {
	project     uuid.UUID
	release     string
	environment string
	hour        int64
}

type sessionUserKey struct {
	project     uuid.UUID
	release     string
	environment string
	day         int64
	hash        [16]byte
}

// SessionRecorder aggregates session updates in memory; Flush writes them
// as release health counters. Retried updates are counted once per instance.
type SessionRecorder struct {
	key  []byte
	seen *expirable.LRU[string, struct{}]

	mu     sync.Mutex
	orgs   map[uuid.UUID]uuid.UUID
	counts map[sessionKey]*store.SessionCounts
	users  map[sessionUserKey]bool
}

// NewSessionRecorder creates a recorder. key keys the hashes of distinct
// ids, so stored hashes cannot be matched against guessed ids.
func NewSessionRecorder(key []byte) *SessionRecorder {
	return &SessionRecorder{
		key:    key,
		seen:   expirable.NewLRU[string, struct{}](200000, nil, 6*time.Hour),
		orgs:   map[uuid.UUID]uuid.UUID{},
		counts: map[sessionKey]*store.SessionCounts{},
		users:  map[sessionUserKey]bool{},
	}
}

// accept normalizes release, environment and start time; ok is false when
// the session is not recorded.
func (r *SessionRecorder) accept(c *Caller, release, env string, started, now time.Time) (string, string, time.Time, bool) {
	release = strings.TrimSpace(release)
	if release == "" || len(release) > maxRelease || strings.ContainsAny(release, "\n\r\t") {
		metrics.SessionsInvalid.Inc()
		return "", "", time.Time{}, false
	}
	env = strings.ToLower(strings.TrimSpace(env))
	if len(env) > maxShortString {
		env = env[:maxShortString]
	}
	if env == "" {
		env = "production"
	}
	if slices.Contains(c.Key.ProjectSettings.GetIgnoredEnvironments(), env) {
		metrics.SessionsFiltered.Inc()
		return "", "", time.Time{}, false
	}
	if started.IsZero() {
		started = now
	}
	if started.Before(now.Add(-maxSessionAge)) || started.After(now.Add(time.Hour)) {
		metrics.SessionsInvalid.Inc()
		return "", "", time.Time{}, false
	}
	return release, env, started.UTC(), true
}

// once reports whether the marker was not seen before and remembers it.
func (r *SessionRecorder) once(marker string) bool {
	if r.seen.Contains(marker) {
		return false
	}
	r.seen.Add(marker, struct{}{})
	return true
}

func (r *SessionRecorder) counter(c *Caller, release, env string, started time.Time) *store.SessionCounts {
	k := sessionKey{c.Key.Key.ProjectID, release, env, started.Truncate(time.Hour).Unix()}
	sc := r.counts[k]
	if sc == nil {
		sc = &store.SessionCounts{OrgID: c.Key.Key.OrgID, ProjectID: c.Key.Key.ProjectID, Release: release,
			Environment: env, Hour: started.Truncate(time.Hour)}
		r.counts[k] = sc
	}
	return sc
}

func (r *SessionRecorder) user(c *Caller, release, env string, started time.Time, did string, crashed bool) {
	if did == "" {
		return
	}
	mac := hmac.New(sha256.New, r.key)
	mac.Write(c.Key.Key.ProjectID[:])
	mac.Write([]byte(did))
	k := sessionUserKey{project: c.Key.Key.ProjectID, release: release, environment: env,
		day: started.Truncate(24 * time.Hour).Unix()}
	copy(k.hash[:], mac.Sum(nil))
	r.users[k] = r.users[k] || crashed
}

// Record adds one session update.
func (r *SessionRecorder) Record(c *Caller, u SessionUpdate, now time.Time) {
	release, env, started, ok := r.accept(c, u.Release, u.Environment, u.Started, now)
	if !ok {
		return
	}
	metrics.SessionsAccepted.Inc()
	sid := c.Key.Key.ProjectID.String() + "|" + u.SID
	status := strings.ToLower(u.Status)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orgs[c.Key.Key.ProjectID] = c.Key.Key.OrgID
	sc := r.counter(c, release, env, started)
	if u.Init && r.once("i|"+sid) {
		sc.Started++
	}
	crashed := status == SessionCrashed
	if (u.Errors > 0 || crashed || status == SessionAbnormal || status == SessionErrored) && r.once("e|"+sid) {
		sc.Errored++
	}
	if crashed && r.once("c|"+sid) {
		sc.Crashed++
	}
	if status == SessionAbnormal && r.once("a|"+sid) {
		sc.Abnormal++
	}
	r.user(c, release, env, started, u.DistinctID, crashed)
}

// RecordAggregate adds a bucket of finished sessions.
func (r *SessionRecorder) RecordAggregate(c *Caller, a SessionAggregate, now time.Time) {
	if a.Exited < 0 || a.Errored < 0 || a.Abnormal < 0 || a.Crashed < 0 {
		metrics.SessionsInvalid.Inc()
		return
	}
	total := a.Exited + a.Errored + a.Abnormal + a.Crashed
	if total == 0 {
		return
	}
	release, env, started, ok := r.accept(c, a.Release, a.Environment, a.Started, now)
	if !ok {
		return
	}
	metrics.SessionsAccepted.Add(total)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orgs[c.Key.Key.ProjectID] = c.Key.Key.OrgID
	sc := r.counter(c, release, env, started)
	sc.Started += total
	sc.Errored += a.Errored + a.Abnormal + a.Crashed
	sc.Crashed += a.Crashed
	sc.Abnormal += a.Abnormal
	r.user(c, release, env, started, a.DistinctID, a.Crashed > 0)
}

// Flush writes what was recorded since the last flush. Counters that
// cannot be written are kept for the next attempt.
func (r *SessionRecorder) Flush(ctx context.Context, database *db.DB, log *slog.Logger) {
	r.mu.Lock()
	counts, users, orgs := r.counts, r.users, r.orgs
	r.counts, r.users, r.orgs = map[sessionKey]*store.SessionCounts{}, map[sessionUserKey]bool{}, map[uuid.UUID]uuid.UUID{}
	r.mu.Unlock()
	if len(counts) == 0 && len(users) == 0 {
		return
	}
	rows := make([]store.SessionCounts, 0, len(counts))
	seen := map[sessionKey]store.ReleaseSeen{}
	for k, c := range counts {
		rows = append(rows, *c)
		rk := sessionKey{project: k.project, release: k.release, environment: k.environment}
		if s, ok := seen[rk]; !ok || c.Hour.After(s.At) {
			seen[rk] = store.ReleaseSeen{OrgID: c.OrgID, ProjectID: c.ProjectID, Release: c.Release, Environment: c.Environment, At: c.Hour}
		}
	}
	userRows := make([]store.SessionUser, 0, len(users))
	for k, crashed := range users {
		userRows = append(userRows, store.SessionUser{OrgID: orgs[k.project], ProjectID: k.project, Release: k.release,
			Environment: k.environment, Day: time.Unix(k.day, 0).UTC(), Hash: k.hash[:], Crashed: crashed})
	}
	releases := make([]store.ReleaseSeen, 0, len(seen))
	for _, s := range seen {
		releases = append(releases, s)
	}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.AddSessionCounts(ctx, tx, rows); err != nil {
			return err
		}
		if err := store.AddSessionUsers(ctx, tx, userRows); err != nil {
			return err
		}
		return store.TouchReleases(ctx, tx, releases)
	})
	if err == nil {
		return
	}
	log.Error("write release health", slog.Any("err", err))
	// Put the counters back so that a transient failure loses nothing.
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, c := range counts {
		if cur := r.counts[k]; cur != nil {
			cur.Started += c.Started
			cur.Errored += c.Errored
			cur.Crashed += c.Crashed
			cur.Abnormal += c.Abnormal
		} else {
			r.counts[k] = c
		}
	}
	for k, crashed := range users {
		r.users[k] = r.users[k] || crashed
	}
	for p, o := range orgs {
		r.orgs[p] = o
	}
}
