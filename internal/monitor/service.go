package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Run and environment statuses. Jobs send in_progress, ok and error; izci
// records missed and timeout itself. "active" marks an environment whose
// first run has not finished yet.
const (
	StatusInProgress = "in_progress"
	StatusOK         = "ok"
	StatusError      = "error"
	StatusMissed     = "missed"
	StatusTimeout    = "timeout"
	StatusActive     = "active"
)

// CheckIn is a check-in as a job sent it.
type CheckIn struct {
	// ID the job gave the run; uuid.Nil when absent. A later check-in with
	// the same id finishes the run.
	ID          uuid.UUID
	MonitorSlug string
	// Status is StatusInProgress, StatusOK or StatusError.
	Status string
	// Duration of the run when the job measured it.
	Duration    *time.Duration
	Environment string
	Release     string
	TraceID     string
	// Config creates or updates the monitor (Sentry monitor_config).
	Config *Config
	// Received is when the check-in arrived (clock drift corrected); zero
	// means now.
	Received time.Time
}

// Target is the organization and project a check-in was sent to.
type Target struct {
	OrgID     uuid.UUID
	ProjectID uuid.UUID
}

// Incident is a failed run of a monitor that has reached (or already
// passed) its failure threshold: it becomes an event of the monitor's issue.
type Incident struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	MonitorID   uuid.UUID
	MonitorSlug string
	MonitorName string
	EnvID       uuid.UUID
	Environment string
	Release     string
	// Reason is StatusError, StatusMissed or StatusTimeout.
	Reason     string
	CheckInID  uuid.UUID
	ExpectedAt *time.Time
	// Schedule describes the schedule, e.g. "0 3 * * * (Europe/Istanbul)".
	Schedule string
	// Failures is the number of failed runs in a row.
	Failures int
	At       time.Time
}

// Emitter stores the event of an incident and returns the issue it was
// grouped into.
type Emitter interface {
	EmitIncident(ctx context.Context, inc Incident) (uuid.UUID, error)
}

// InvalidError reports a check-in or config the server cannot accept.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// ErrUnknownMonitor rejects a check-in for a monitor that does not exist
// when the check-in does not carry a config to create it.
var ErrUnknownMonitor = errors.New("monitor not found; send monitor_config to create it")

// Service processes check-ins and sweeps overdue runs.
type Service struct {
	db   *db.DB
	log  *slog.Logger
	emit Emitter
	now  func() time.Time
}

// NewService creates the service; emit may be nil (no issues are raised).
func NewService(database *db.DB, log *slog.Logger, emit Emitter) *Service {
	return &Service{db: database, log: log, emit: emit, now: time.Now}
}

// SetEmitter sets where incidents go.
func (s *Service) SetEmitter(e Emitter) { s.emit = e }

// NormalizeSlug lower-cases a monitor slug, spells Turkish letters in ASCII
// and replaces what Sentry would not accept with dashes; the result has 1–50
// characters of a-z, 0-9, "-" and "_", or is empty when nothing usable is
// left.
func NormalizeSlug(raw string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(slug.Transliterate(strings.TrimSpace(raw))) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 50 {
		out = strings.TrimRight(out[:50], "-")
	}
	return out
}

// NormalizeEnvironment applies the ingest rules for environments.
func NormalizeEnvironment(env string) string {
	env = strings.ToLower(strings.TrimSpace(env))
	if env == "" {
		return "production"
	}
	if len(env) > 64 {
		env = env[:64]
	}
	return env
}

// Process stores a check-in: it creates or updates the monitor when the
// check-in carries a config, records or finishes the run, advances the
// schedule, and raises or resolves the monitor's issue. It returns the id of
// the run.
func (s *Service) Process(ctx context.Context, t Target, in CheckIn) (uuid.UUID, error) {
	slug := NormalizeSlug(in.MonitorSlug)
	if slug == "" {
		metrics.CheckInsInvalid.Inc()
		return uuid.Nil, &InvalidError{"monitor_slug is required"}
	}
	switch in.Status {
	case StatusInProgress, StatusOK, StatusError:
	default:
		metrics.CheckInsInvalid.Inc()
		return uuid.Nil, &InvalidError{fmt.Sprintf("status must be in_progress, ok or error (got %q)", in.Status)}
	}
	if in.Config != nil {
		if err := in.Config.Normalize(); err != nil {
			metrics.CheckInsInvalid.Inc()
			return uuid.Nil, &InvalidError{"monitor_config: " + err.Error()}
		}
	}
	env := NormalizeEnvironment(in.Environment)
	now := in.Received
	if now.IsZero() {
		now = s.now()
	}
	var (
		runID     uuid.UUID
		incidents []Incident
	)
	err := s.db.InOrg(ctx, t.OrgID, func(tx pgx.Tx) error {
		incidents = nil
		m, err := s.monitorFor(ctx, tx, t, slug, in.Config)
		if err != nil {
			return err
		}
		sched, err := scheduleOf(m)
		if err != nil {
			return err
		}
		e, err := store.LockMonitorEnv(ctx, tx, t.OrgID, m.ID, env)
		if err != nil {
			return err
		}

		run, err := s.findRun(ctx, tx, m, env, in, now)
		if err != nil {
			return err
		}
		outcome := ""
		// The scheduled time the run answers, for the incident message.
		var expected *time.Time
		switch {
		case run != nil && run.Status != StatusInProgress:
			// The run already finished (a retry, or a late check-in after a
			// timeout): nothing changes.
			runID = run.CheckInID
			return nil
		case run != nil && in.Status == StatusInProgress:
			runID = run.CheckInID
			return store.TouchCheckIn(ctx, tx, run.ID)
		case run != nil:
			runID = run.CheckInID
			expected = run.ExpectedAt
			dur := durationMS(in.Duration, now.Sub(run.CreatedAt))
			if err := store.FinishCheckIn(ctx, tx, run.ID, in.Status, dur); err != nil {
				return err
			}
			outcome = in.Status
		default:
			ci := store.CheckIn{OrgID: t.OrgID, MonitorID: m.ID, Environment: env, CheckInID: in.ID, Status: in.Status,
				Release: truncate(in.Release, 250), TraceID: truncate(in.TraceID, 64), CreatedAt: now}
			// The run answers the expected slot unless it came more than the
			// margin early (a manual or extra run).
			if e.NextCheckinAt != nil && !now.Before(e.NextCheckinAt.Add(-time.Duration(m.CheckinMargin)*time.Minute)) {
				ci.ExpectedAt = e.NextCheckinAt
			}
			if ci.CheckInID == uuid.Nil {
				ci.CheckInID = uuid.New()
			}
			if in.Status != StatusInProgress {
				ci.DurationMS = durationMS(in.Duration, 0)
			}
			if err := store.InsertCheckIn(ctx, tx, &ci); err != nil {
				return err
			}
			runID = ci.CheckInID
			expected = ci.ExpectedAt
			// A new run answers the expected one: the next is due one
			// schedule step after it.
			e.LastCheckinAt = &now
			next := sched.Next(now)
			latest := next.Add(time.Duration(m.CheckinMargin) * time.Minute)
			e.NextCheckinAt, e.NextCheckinLatest = &next, &latest
			if in.Status != StatusInProgress {
				outcome = in.Status
			}
		}
		switch outcome {
		case StatusOK:
			if err := s.success(ctx, tx, e, m); err != nil {
				return err
			}
		case StatusError:
			if inc := failure(e, m, StatusError, runID, expected, in.Release, now); inc != nil {
				incidents = append(incidents, *inc)
			}
		}
		return store.SaveMonitorEnv(ctx, tx, e)
	})
	if err != nil {
		if errors.Is(err, ErrUnknownMonitor) {
			metrics.CheckInsUnknown.Inc()
		}
		return uuid.Nil, err
	}
	metrics.CheckInsAccepted.Inc()
	s.raise(ctx, incidents)
	return runID, nil
}

// monitorFor finds the monitor of a check-in, creating or updating it from
// the check-in's config.
func (s *Service) monitorFor(ctx context.Context, tx pgx.Tx, t Target, slug string, cfg *Config) (*store.Monitor, error) {
	m, err := store.GetMonitorBySlug(ctx, tx, t.ProjectID, slug)
	switch {
	case errors.Is(err, store.ErrNotFound) && cfg == nil:
		return nil, ErrUnknownMonitor
	case errors.Is(err, store.ErrNotFound):
		m = &store.Monitor{OrgID: t.OrgID, ProjectID: t.ProjectID, Slug: slug, Name: slug}
		applyConfig(m, *cfg)
		if err := store.InsertMonitor(ctx, tx, m); err != nil {
			return nil, err
		}
		return m, nil
	case err != nil:
		return nil, err
	}
	if cfg != nil && configOf(m) != *cfg {
		reschedule := scheduleChanged(configOf(m), *cfg)
		applyConfig(m, *cfg)
		if err := store.UpdateMonitor(ctx, tx, m); err != nil {
			return nil, err
		}
		if reschedule {
			if err := Reschedule(ctx, tx, m, s.now()); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

// findRun returns the run a check-in continues: the one with its id, or for
// a closing check-in without id the newest run still in progress.
func (s *Service) findRun(ctx context.Context, tx pgx.Tx, m *store.Monitor, env string, in CheckIn, now time.Time) (*store.CheckIn, error) {
	var (
		run *store.CheckIn
		err error
	)
	switch {
	case in.ID != uuid.Nil:
		run, err = store.LockCheckIn(ctx, tx, m.ID, in.ID)
	case in.Status != StatusInProgress:
		since := now.Add(-time.Duration(m.MaxRuntime) * time.Minute)
		run, err = store.LockLatestOpenCheckIn(ctx, tx, m.ID, env, since)
	default:
		return nil, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return run, err
}

// success applies a successful run: it resolves the monitor's issue once
// enough runs in a row succeeded.
func (s *Service) success(ctx context.Context, tx pgx.Tx, e *store.MonitorEnv, m *store.Monitor) error {
	e.Status = StatusOK
	if e.IssueID == nil {
		e.Failures, e.Successes = 0, 0
		return nil
	}
	e.Successes++
	if e.Successes < m.RecoveryThreshold {
		return nil
	}
	resolved, err := store.ResolveIssueBySystem(ctx, tx, m.OrgID, *e.IssueID, "monitor_recovered")
	if err != nil {
		return err
	}
	if resolved {
		metrics.MonitorRecoveries.Inc()
	}
	e.IssueID = nil
	e.Failures, e.Successes = 0, 0
	return nil
}

// failure applies a failed run and returns the incident to raise, if the
// failure threshold is reached (or an issue is already open).
func failure(e *store.MonitorEnv, m *store.Monitor, reason string, run uuid.UUID, expected *time.Time, release string, now time.Time) *Incident {
	e.Status = reason
	e.Failures++
	e.Successes = 0
	if m.Paused || (e.IssueID == nil && e.Failures < m.FailureThreshold) {
		return nil
	}
	return &Incident{
		OrgID: m.OrgID, ProjectID: m.ProjectID, MonitorID: m.ID, MonitorSlug: m.Slug, MonitorName: m.Name,
		EnvID: e.ID, Environment: e.Environment, Release: release, Reason: reason, CheckInID: run,
		ExpectedAt: expected, Schedule: Describe(m), Failures: e.Failures, At: now,
	}
}

// raise sends incidents to the emitter and remembers the issue each one
// opened. A failure is logged: the next failed run tries again.
func (s *Service) raise(ctx context.Context, incidents []Incident) {
	if s.emit == nil {
		return
	}
	for _, inc := range incidents {
		issueID, err := s.emit.EmitIncident(ctx, inc)
		if err != nil {
			s.log.Warn("raise monitor issue", slog.String("monitor", inc.MonitorSlug), slog.Any("err", err))
			continue
		}
		metrics.MonitorIncidents.Inc()
		if issueID == uuid.Nil {
			continue
		}
		if err := s.db.InOrg(ctx, inc.OrgID, func(tx pgx.Tx) error {
			return store.SetMonitorEnvIssue(ctx, tx, inc.EnvID, issueID)
		}); err != nil {
			s.log.Warn("remember monitor issue", slog.String("monitor", inc.MonitorSlug), slog.Any("err", err))
		}
	}
}

// Sweep records runs that did not check in on time and runs that stayed in
// progress too long, then raises the resulting issues. Instances skip rows
// another instance is sweeping, so running it everywhere is safe.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	var incidents []Incident
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		incidents = nil
		due, err := store.DueMonitorEnvs(ctx, tx, now, 500)
		if err != nil {
			return err
		}
		for i := range due {
			e, m := &due[i].Env, &due[i].Monitor
			inc, err := missed(ctx, tx, e, m, now)
			if err != nil {
				return err
			}
			if inc != nil {
				incidents = append(incidents, *inc)
			}
		}
		overdue, err := store.OverdueCheckIns(ctx, tx, now, 500)
		if err != nil {
			return err
		}
		for i := range overdue {
			c, m := &overdue[i].CheckIn, &overdue[i].Monitor
			limit := m.MaxRuntime * 60_000
			if err := store.FinishCheckIn(ctx, tx, c.ID, StatusTimeout, &limit); err != nil {
				return err
			}
			metrics.MonitorTimeouts.Inc()
			e, err := store.LockMonitorEnv(ctx, tx, m.OrgID, m.ID, c.Environment)
			if err != nil {
				return err
			}
			if inc := failure(e, m, StatusTimeout, c.CheckInID, c.ExpectedAt, c.Release, now); inc != nil {
				incidents = append(incidents, *inc)
			}
			if err := store.SaveMonitorEnv(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.raise(ctx, incidents)
	return nil
}

// missed records a missed run for an overdue environment and moves its
// schedule to the first run still ahead, so a long outage yields one missed
// run rather than one per skipped slot.
func missed(ctx context.Context, tx pgx.Tx, e *store.MonitorEnv, m *store.Monitor, now time.Time) (*Incident, error) {
	sched, err := scheduleOf(m)
	if err != nil {
		return nil, err
	}
	expected := *e.NextCheckinAt
	ci := store.CheckIn{OrgID: m.OrgID, MonitorID: m.ID, Environment: e.Environment, CheckInID: uuid.New(),
		Status: StatusMissed, ExpectedAt: &expected, CreatedAt: now}
	if err := store.InsertCheckIn(ctx, tx, &ci); err != nil {
		return nil, err
	}
	metrics.MonitorMissed.Inc()
	margin := time.Duration(m.CheckinMargin) * time.Minute
	next := sched.Next(expected)
	for i := 0; !next.Add(margin).After(now); i++ {
		if i == 10_000 {
			next = sched.Next(now)
			break
		}
		next = sched.Next(next)
	}
	latest := next.Add(margin)
	e.NextCheckinAt, e.NextCheckinLatest = &next, &latest
	inc := failure(e, m, StatusMissed, ci.CheckInID, &expected, "", now)
	return inc, store.SaveMonitorEnv(ctx, tx, e)
}

// Reschedule recomputes when each environment of m expects its next run,
// after the schedule changed or the monitor was resumed.
func Reschedule(ctx context.Context, tx pgx.Tx, m *store.Monitor, now time.Time) error {
	sched, err := scheduleOf(m)
	if err != nil {
		return err
	}
	next := sched.Next(now)
	latest := next.Add(time.Duration(m.CheckinMargin) * time.Minute)
	_, err = tx.Exec(ctx, `UPDATE monitor_environments SET next_checkin_at = $2, next_checkin_latest = $3, updated_at = now()
		WHERE monitor_id = $1 AND next_checkin_at IS NOT NULL`, m.ID, next, latest)
	return err
}

func scheduleOf(m *store.Monitor) (Schedule, error) {
	return configOf(m).Schedule()
}

// configOf returns the config a monitor row holds.
func configOf(m *store.Monitor) Config {
	return Config{ScheduleType: m.ScheduleType, Crontab: m.Crontab, Interval: m.Interval, Unit: m.Unit,
		Timezone: m.Timezone, CheckinMargin: m.CheckinMargin, MaxRuntime: m.MaxRuntime,
		FailureThreshold: m.FailureThreshold, RecoveryThreshold: m.RecoveryThreshold}
}

// ApplyConfig copies a normalized config onto a monitor row.
func ApplyConfig(m *store.Monitor, c Config) { applyConfig(m, c) }

func applyConfig(m *store.Monitor, c Config) {
	m.ScheduleType, m.Crontab, m.Interval, m.Unit, m.Timezone = c.ScheduleType, c.Crontab, c.Interval, c.Unit, c.Timezone
	m.CheckinMargin, m.MaxRuntime = c.CheckinMargin, c.MaxRuntime
	m.FailureThreshold, m.RecoveryThreshold = c.FailureThreshold, c.RecoveryThreshold
}

// ConfigOf returns the config of a monitor row.
func ConfigOf(m *store.Monitor) Config { return configOf(m) }

// ScheduleChanged reports whether two configs expect runs at different times.
func ScheduleChanged(a, b Config) bool { return scheduleChanged(a, b) }

func scheduleChanged(a, b Config) bool {
	return a.ScheduleType != b.ScheduleType || a.Crontab != b.Crontab || a.Interval != b.Interval || a.Unit != b.Unit ||
		a.Timezone != b.Timezone || a.CheckinMargin != b.CheckinMargin
}

// Describe renders a monitor's schedule for messages, e.g.
// "0 3 * * * (Europe/Istanbul)" or "every 2 hour".
func Describe(m *store.Monitor) string {
	if m.ScheduleType == ScheduleInterval {
		return fmt.Sprintf("every %d %s", m.Interval, m.Unit)
	}
	return fmt.Sprintf("%s (%s)", m.Crontab, m.Timezone)
}

func durationMS(measured *time.Duration, fallback time.Duration) *int {
	d := fallback
	if measured != nil && *measured >= 0 {
		d = *measured
	}
	if d <= 0 && measured == nil {
		return nil
	}
	ms := int(min(d.Milliseconds(), int64(1<<31-1)))
	return &ms
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
