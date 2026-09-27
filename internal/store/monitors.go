package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Monitor is a cron monitor with its schedule and failure policy.
type Monitor struct {
	ID                uuid.UUID
	OrgID             uuid.UUID
	ProjectID         uuid.UUID
	ProjectSlug       string
	ProjectName       string
	Slug              string
	Name              string
	ScheduleType      string
	Crontab           string
	Interval          int
	Unit              string
	Timezone          string
	CheckinMargin     int
	MaxRuntime        int
	FailureThreshold  int
	RecoveryThreshold int
	Paused            bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const monitorColumns = `m.id, m.org_id, m.project_id, p.slug, p.name, m.slug, m.name, m.schedule_type, m.schedule_crontab,
	m.schedule_interval, m.schedule_unit, m.timezone, m.checkin_margin, m.max_runtime, m.failure_threshold,
	m.recovery_threshold, m.paused, m.created_at, m.updated_at`

func scanMonitor(row pgx.Row) (*Monitor, error) {
	var m Monitor
	err := row.Scan(&m.ID, &m.OrgID, &m.ProjectID, &m.ProjectSlug, &m.ProjectName, &m.Slug, &m.Name, &m.ScheduleType,
		&m.Crontab, &m.Interval, &m.Unit, &m.Timezone, &m.CheckinMargin, &m.MaxRuntime, &m.FailureThreshold,
		&m.RecoveryThreshold, &m.Paused, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

// GetMonitor returns a monitor of the organization.
func GetMonitor(ctx context.Context, q Querier, orgID, id uuid.UUID) (*Monitor, error) {
	return scanMonitor(q.QueryRow(ctx, `SELECT `+monitorColumns+` FROM monitors m JOIN projects p ON p.id = m.project_id
		WHERE m.id = $1 AND m.org_id = $2 AND p.deleted_at IS NULL`, id, orgID))
}

// GetMonitorBySlug returns the monitor a project knows by slug.
func GetMonitorBySlug(ctx context.Context, q Querier, projectID uuid.UUID, slug string) (*Monitor, error) {
	return scanMonitor(q.QueryRow(ctx, `SELECT `+monitorColumns+` FROM monitors m JOIN projects p ON p.id = m.project_id
		WHERE m.project_id = $1 AND m.slug = $2`, projectID, slug))
}

// MonitorFilter selects monitors of an organization.
type MonitorFilter struct {
	OrgID     uuid.UUID
	ProjectID *uuid.UUID
	// Query matches the name or slug (case-insensitive substring).
	Query string
}

// ListMonitors returns monitors ordered by name.
func ListMonitors(ctx context.Context, q Querier, f MonitorFilter) ([]Monitor, error) {
	rows, err := q.Query(ctx, `SELECT `+monitorColumns+` FROM monitors m JOIN projects p ON p.id = m.project_id
		WHERE m.org_id = $1 AND p.deleted_at IS NULL AND ($2::uuid IS NULL OR m.project_id = $2)
		  AND ($3 = '' OR m.name ILIKE '%' || $3 || '%' OR m.slug ILIKE '%' || $3 || '%')
		ORDER BY lower(m.name), m.slug`, f.OrgID, f.ProjectID, f.Query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// InsertMonitor creates a monitor; m.ID, CreatedAt and UpdatedAt are set.
// A taken slug fails with a unique violation on monitors_project_id_slug_key.
func InsertMonitor(ctx context.Context, q Querier, m *Monitor) error {
	return q.QueryRow(ctx, `
		INSERT INTO monitors (org_id, project_id, slug, name, schedule_type, schedule_crontab, schedule_interval,
		  schedule_unit, timezone, checkin_margin, max_runtime, failure_threshold, recovery_threshold, paused)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, created_at, updated_at`,
		m.OrgID, m.ProjectID, m.Slug, m.Name, m.ScheduleType, m.Crontab, m.Interval, m.Unit, m.Timezone,
		m.CheckinMargin, m.MaxRuntime, m.FailureThreshold, m.RecoveryThreshold, m.Paused,
	).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
}

// UpdateMonitor saves the name, schedule, policy and paused flag of m.
func UpdateMonitor(ctx context.Context, q Querier, m *Monitor) error {
	tag, err := q.Exec(ctx, `
		UPDATE monitors SET name = $3, schedule_type = $4, schedule_crontab = $5, schedule_interval = $6,
		  schedule_unit = $7, timezone = $8, checkin_margin = $9, max_runtime = $10, failure_threshold = $11,
		  recovery_threshold = $12, paused = $13, updated_at = now()
		WHERE id = $1 AND org_id = $2`,
		m.ID, m.OrgID, m.Name, m.ScheduleType, m.Crontab, m.Interval, m.Unit, m.Timezone, m.CheckinMargin,
		m.MaxRuntime, m.FailureThreshold, m.RecoveryThreshold, m.Paused)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteMonitor removes a monitor with its environments and check-ins.
func DeleteMonitor(ctx context.Context, q Querier, orgID, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM monitors WHERE id = $1 AND org_id = $2`, id, orgID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// MonitorEnv is the state of a monitor in one environment.
type MonitorEnv struct {
	ID                uuid.UUID
	OrgID             uuid.UUID
	MonitorID         uuid.UUID
	Environment       string
	Status            string
	LastCheckinAt     *time.Time
	NextCheckinAt     *time.Time
	NextCheckinLatest *time.Time
	Failures          int
	Successes         int
	IssueID           *uuid.UUID
	// IssueSeq is the short sequence of the issue (see ingest.ShortID),
	// filled by ListMonitorEnvs.
	IssueSeq  int32
	UpdatedAt time.Time
}

const monitorEnvColumns = `me.id, me.org_id, me.monitor_id, me.environment, me.status, me.last_checkin_at,
	me.next_checkin_at, me.next_checkin_latest, me.failures, me.successes, me.issue_id, me.updated_at`

func scanMonitorEnv(row pgx.Row, extra ...any) (*MonitorEnv, error) {
	var e MonitorEnv
	dest := append([]any{&e.ID, &e.OrgID, &e.MonitorID, &e.Environment, &e.Status, &e.LastCheckinAt, &e.NextCheckinAt,
		&e.NextCheckinLatest, &e.Failures, &e.Successes, &e.IssueID, &e.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, notFound(err)
	}
	return &e, nil
}

// LockMonitorEnv returns the state of a monitor in an environment, creating
// it on first use, and locks it for the rest of the transaction.
func LockMonitorEnv(ctx context.Context, q Querier, orgID, monitorID uuid.UUID, env string) (*MonitorEnv, error) {
	if _, err := q.Exec(ctx, `INSERT INTO monitor_environments (org_id, monitor_id, environment) VALUES ($1, $2, $3)
		ON CONFLICT (monitor_id, environment) DO NOTHING`, orgID, monitorID, env); err != nil {
		return nil, err
	}
	return scanMonitorEnv(q.QueryRow(ctx, `SELECT `+monitorEnvColumns+` FROM monitor_environments me
		WHERE me.monitor_id = $1 AND me.environment = $2 FOR UPDATE`, monitorID, env))
}

// SaveMonitorEnv stores the mutable state of e.
func SaveMonitorEnv(ctx context.Context, q Querier, e *MonitorEnv) error {
	_, err := q.Exec(ctx, `
		UPDATE monitor_environments SET status = $2, last_checkin_at = $3, next_checkin_at = $4,
		  next_checkin_latest = $5, failures = $6, successes = $7, issue_id = $8, updated_at = now()
		WHERE id = $1`,
		e.ID, e.Status, e.LastCheckinAt, e.NextCheckinAt, e.NextCheckinLatest, e.Failures, e.Successes, e.IssueID)
	return err
}

// SetMonitorEnvIssue records the issue raised for a monitor environment,
// unless another one was recorded meanwhile.
func SetMonitorEnvIssue(ctx context.Context, q Querier, envID, issueID uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE monitor_environments SET issue_id = $2 WHERE id = $1 AND issue_id IS NULL`, envID, issueID)
	return err
}

// ListMonitorEnvs returns the environments of the given monitors, keyed by
// monitor, each list ordered by environment name.
func ListMonitorEnvs(ctx context.Context, q Querier, monitorIDs []uuid.UUID) (map[uuid.UUID][]MonitorEnv, error) {
	rows, err := q.Query(ctx, `SELECT `+monitorEnvColumns+`, coalesce(i.short_seq, 0)
		FROM monitor_environments me
		LEFT JOIN issues i ON i.id = me.issue_id
		WHERE me.monitor_id = ANY($1) ORDER BY me.environment`, monitorIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]MonitorEnv{}
	for rows.Next() {
		var seq int32
		e, err := scanMonitorEnv(rows, &seq)
		if err != nil {
			return nil, err
		}
		e.IssueSeq = seq
		out[e.MonitorID] = append(out[e.MonitorID], *e)
	}
	return out, rows.Err()
}

// DueMonitorEnv is a monitor environment whose next run is overdue, with its
// monitor.
type DueMonitorEnv struct {
	Env     MonitorEnv
	Monitor Monitor
}

// DueMonitorEnvs locks (skipping rows other instances hold) up to limit
// environments of active monitors whose latest check-in time has passed.
func DueMonitorEnvs(ctx context.Context, q Querier, now time.Time, limit int) ([]DueMonitorEnv, error) {
	rows, err := q.Query(ctx, `SELECT `+monitorEnvColumns+`, `+monitorColumns+`
		FROM monitor_environments me
		JOIN monitors m ON m.id = me.monitor_id
		JOIN projects p ON p.id = m.project_id
		WHERE me.next_checkin_latest <= $1 AND NOT m.paused AND p.deleted_at IS NULL
		ORDER BY me.next_checkin_latest
		LIMIT $2
		FOR UPDATE OF me SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueMonitorEnv
	for rows.Next() {
		var d DueMonitorEnv
		m := &d.Monitor
		e, err := scanMonitorEnv(rows, &m.ID, &m.OrgID, &m.ProjectID, &m.ProjectSlug, &m.ProjectName, &m.Slug, &m.Name,
			&m.ScheduleType, &m.Crontab, &m.Interval, &m.Unit, &m.Timezone, &m.CheckinMargin, &m.MaxRuntime,
			&m.FailureThreshold, &m.RecoveryThreshold, &m.Paused, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		d.Env = *e
		out = append(out, d)
	}
	return out, rows.Err()
}

// CheckIn is one run of a monitor.
type CheckIn struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	MonitorID   uuid.UUID
	Environment string
	CheckInID   uuid.UUID
	Status      string
	DurationMS  *int
	Release     string
	ExpectedAt  *time.Time
	TraceID     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const checkInColumns = `c.id, c.org_id, c.monitor_id, c.environment, c.check_in_id, c.status, c.duration_ms, c.release,
	c.expected_at, c.trace_id, c.created_at, c.updated_at`

func scanCheckIn(row pgx.Row, extra ...any) (*CheckIn, error) {
	var c CheckIn
	dest := append([]any{&c.ID, &c.OrgID, &c.MonitorID, &c.Environment, &c.CheckInID, &c.Status, &c.DurationMS,
		&c.Release, &c.ExpectedAt, &c.TraceID, &c.CreatedAt, &c.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// LockCheckIn returns a run by the id the job gave it, locked.
func LockCheckIn(ctx context.Context, q Querier, monitorID, checkInID uuid.UUID) (*CheckIn, error) {
	return scanCheckIn(q.QueryRow(ctx, `SELECT `+checkInColumns+` FROM monitor_checkins c
		WHERE c.monitor_id = $1 AND c.check_in_id = $2 FOR UPDATE`, monitorID, checkInID))
}

// LockLatestOpenCheckIn returns the newest run still in progress in an
// environment that started after since, locked.
func LockLatestOpenCheckIn(ctx context.Context, q Querier, monitorID uuid.UUID, env string, since time.Time) (*CheckIn, error) {
	return scanCheckIn(q.QueryRow(ctx, `SELECT `+checkInColumns+` FROM monitor_checkins c
		WHERE c.monitor_id = $1 AND c.environment = $2 AND c.status = 'in_progress' AND c.created_at >= $3
		ORDER BY c.created_at DESC LIMIT 1 FOR UPDATE`, monitorID, env, since))
}

// InsertCheckIn records a run; c.ID, CreatedAt and UpdatedAt are set.
func InsertCheckIn(ctx context.Context, q Querier, c *CheckIn) error {
	return q.QueryRow(ctx, `
		INSERT INTO monitor_checkins (org_id, monitor_id, environment, check_in_id, status, duration_ms, release,
		  expected_at, trace_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		RETURNING id, created_at, updated_at`,
		c.OrgID, c.MonitorID, c.Environment, c.CheckInID, c.Status, c.DurationMS, c.Release, c.ExpectedAt, c.TraceID,
		c.CreatedAt,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

// FinishCheckIn stores the outcome of a run.
func FinishCheckIn(ctx context.Context, q Querier, id uuid.UUID, status string, durationMS *int) error {
	_, err := q.Exec(ctx, `UPDATE monitor_checkins SET status = $2, duration_ms = $3, updated_at = now() WHERE id = $1`,
		id, status, durationMS)
	return err
}

// TouchCheckIn marks a run still in progress (a repeated in_progress check-in).
func TouchCheckIn(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE monitor_checkins SET updated_at = now() WHERE id = $1`, id)
	return err
}

// CheckInFilter pages the runs of a monitor, newest first.
type CheckInFilter struct {
	MonitorID   uuid.UUID
	Environment string
	Before      *time.Time
	BeforeID    uuid.UUID
	Limit       int
}

// ListCheckIns returns runs of a monitor, newest first.
func ListCheckIns(ctx context.Context, q Querier, f CheckInFilter) ([]CheckIn, error) {
	rows, err := q.Query(ctx, `SELECT `+checkInColumns+` FROM monitor_checkins c
		WHERE c.monitor_id = $1 AND ($2 = '' OR c.environment = $2)
		  AND ($3::timestamptz IS NULL OR (c.created_at, c.id) < ($3, $4))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $5`, f.MonitorID, f.Environment, f.Before, f.BeforeID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CheckIn
	for rows.Next() {
		c, err := scanCheckIn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// RecentCheckIns returns the newest perMonitor runs of each monitor, keyed
// by monitor, newest first.
func RecentCheckIns(ctx context.Context, q Querier, monitorIDs []uuid.UUID, perMonitor int) (map[uuid.UUID][]CheckIn, error) {
	rows, err := q.Query(ctx, `SELECT `+checkInColumns+` FROM unnest($1::uuid[]) AS ids(id)
		CROSS JOIN LATERAL (SELECT * FROM monitor_checkins WHERE monitor_id = ids.id
		  ORDER BY created_at DESC LIMIT $2) c`, monitorIDs, perMonitor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]CheckIn{}
	for rows.Next() {
		c, err := scanCheckIn(rows)
		if err != nil {
			return nil, err
		}
		out[c.MonitorID] = append(out[c.MonitorID], *c)
	}
	return out, rows.Err()
}

// OverdueCheckIn is a run in progress for longer than its monitor allows.
type OverdueCheckIn struct {
	CheckIn CheckIn
	Monitor Monitor
}

// OverdueCheckIns locks (skipping rows other instances hold) up to limit
// runs that have been in progress longer than their monitor's max runtime.
func OverdueCheckIns(ctx context.Context, q Querier, now time.Time, limit int) ([]OverdueCheckIn, error) {
	rows, err := q.Query(ctx, `SELECT `+checkInColumns+`, `+monitorColumns+`
		FROM monitor_checkins c
		JOIN monitors m ON m.id = c.monitor_id
		JOIN projects p ON p.id = m.project_id
		WHERE c.status = 'in_progress' AND c.created_at + make_interval(mins => m.max_runtime) <= $1
		  AND p.deleted_at IS NULL
		ORDER BY c.created_at
		LIMIT $2
		FOR UPDATE OF c SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OverdueCheckIn
	for rows.Next() {
		var o OverdueCheckIn
		m := &o.Monitor
		c, err := scanCheckIn(rows, &m.ID, &m.OrgID, &m.ProjectID, &m.ProjectSlug, &m.ProjectName, &m.Slug, &m.Name,
			&m.ScheduleType, &m.Crontab, &m.Interval, &m.Unit, &m.Timezone, &m.CheckinMargin, &m.MaxRuntime,
			&m.FailureThreshold, &m.RecoveryThreshold, &m.Paused, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		o.CheckIn = *c
		out = append(out, o)
	}
	return out, rows.Err()
}

// ResolveIssueBySystem resolves an unresolved issue without a person behind
// it (a monitor recovered) and records the change in its activity.
func ResolveIssueBySystem(ctx context.Context, q Querier, orgID, issueID uuid.UUID, reason string) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE issues SET status = 2, substatus = 1, resolved_at = now(), resolved_by = NULL, resolved_in_release = '',
		  resolved_in_next_release = false, updated_at = now()
		WHERE id = $1 AND org_id = $2 AND status = 1`, issueID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	err = AddIssueActivity(ctx, q, orgID, []uuid.UUID{issueID}, ActivityStatusChanged, nil,
		map[string]string{"to": StatusName(IssueResolved), "reason": reason})
	return err == nil, err
}
