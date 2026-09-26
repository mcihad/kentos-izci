package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AI run status, trigger and mode values (izci.api.v1.AiRunStatus, …).
const (
	AiQueued    int16 = 1
	AiPreparing int16 = 2
	AiRunning   int16 = 3
	AiSucceeded int16 = 4
	AiFailed    int16 = 5
	AiCancelled int16 = 6

	AiModeAnalyze int16 = 1
	AiModeFix     int16 = 2

	AiTriggerManual    int16 = 1
	AiTriggerAutomatic int16 = 2
	AiTriggerCLI       int16 = 3
	AiTriggerMCP       int16 = 4
	AiTriggerTelegram  int16 = 5
)

// AI states of issues (izci.api.v1.AiState).
const (
	AiStateNone        int16 = 1
	AiStateRunning     int16 = 2
	AiStateAnalyzed    int16 = 3
	AiStateFixProposed int16 = 4
	AiStatePullRequest int16 = 5
	AiStateFailed      int16 = 6
)

// uniqueActiveAiRun enforces one active run per issue.
const uniqueActiveAiRun = "ai_runs_active_key"

// ErrRunActive means the issue already has a queued or running analysis.
var ErrRunActive = errors.New("an AI run is already active for this issue")

// AiResult is the structured outcome an agent writes to .izci/result.json.
type AiResult struct {
	Summary         string   `json:"summary"`
	RootCause       string   `json:"root_cause"`
	FixSummary      string   `json:"fix_summary"`
	Confidence      float64  `json:"confidence"`
	Risk            string   `json:"risk"`
	TestSuggestions []string `json:"test_suggestions"`
}

// ChangedFile is one file of a proposed fix.
type ChangedFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Status    string `json:"status"`
}

// AiUsage counts tokens and cost of a run.
type AiUsage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// PullRequestInfo is the branch or pull request made from a run.
type PullRequestInfo struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Branch string `json:"branch"`
	Draft  bool   `json:"draft,omitempty"`
	State  string `json:"state"`
}

// AiRun is one analysis of an issue.
type AiRun struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ProjectID    uuid.UUID
	IssueID      uuid.UUID
	EventID      *uuid.UUID
	Status       int16
	Trigger      int16
	Mode         int16
	Model        string
	Instructions string
	RequestedBy  *uuid.UUID
	BaseCommit   string
	BaseBranch   string
	Result       AiResult
	Analysis     string
	Diff         string
	Files        []ChangedFile
	PullRequest  *PullRequestInfo
	Error        string
	Usage        AiUsage
	Worker       string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

// Active reports whether the run is queued or in progress.
func (r *AiRun) Active() bool {
	return r.Status == AiQueued || r.Status == AiPreparing || r.Status == AiRunning
}

const aiRunColumns = `id, org_id, project_id, issue_id, event_id, status, trigger, mode, model, instructions, requested_by,
	base_commit, base_branch, result, analysis, diff, files, pull_request, error, usage, worker, created_at, started_at, finished_at`

func scanAiRun(row interface{ Scan(...any) error }) (*AiRun, error) {
	var r AiRun
	var result, files, pr, usage []byte
	if err := row.Scan(&r.ID, &r.OrgID, &r.ProjectID, &r.IssueID, &r.EventID, &r.Status, &r.Trigger, &r.Mode, &r.Model,
		&r.Instructions, &r.RequestedBy, &r.BaseCommit, &r.BaseBranch, &result, &r.Analysis, &r.Diff, &files, &pr, &r.Error,
		&usage, &r.Worker, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(result, &r.Result)
	_ = json.Unmarshal(files, &r.Files)
	_ = json.Unmarshal(usage, &r.Usage)
	if len(pr) > 0 && string(pr) != "null" {
		r.PullRequest = &PullRequestInfo{}
		_ = json.Unmarshal(pr, r.PullRequest)
	}
	return &r, nil
}

// CreateAiRun queues a run; ErrRunActive when the issue has one already.
func CreateAiRun(ctx context.Context, q Querier, r *AiRun) (*AiRun, error) {
	run, err := scanAiRun(q.QueryRow(ctx, `
		INSERT INTO ai_runs (org_id, project_id, issue_id, event_id, trigger, mode, model, instructions, requested_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+aiRunColumns, r.OrgID, r.ProjectID, r.IssueID, r.EventID, r.Trigger, r.Mode, r.Model, r.Instructions, r.RequestedBy))
	if UniqueViolation(err, uniqueActiveAiRun) {
		return nil, ErrRunActive
	}
	return run, err
}

// GetAiRun returns a run by id.
func GetAiRun(ctx context.Context, q Querier, id uuid.UUID) (*AiRun, error) {
	return scanAiRun(q.QueryRow(ctx, `SELECT `+aiRunColumns+` FROM ai_runs WHERE id = $1`, id))
}

// AiRunFilter narrows ListAiRuns.
type AiRunFilter struct {
	IssueID   *uuid.UUID
	ProjectID *uuid.UUID
	Status    int16
	// Before continues a listing (created_at, id of the last row).
	Before   *time.Time
	BeforeID uuid.UUID
}

// ListAiRuns lists an organization's runs, newest first.
func ListAiRuns(ctx context.Context, q Querier, orgID uuid.UUID, f AiRunFilter, limit int) ([]*AiRun, error) {
	rows, err := q.Query(ctx, `SELECT `+aiRunColumns+` FROM ai_runs
		WHERE org_id = $1 AND ($2::uuid IS NULL OR issue_id = $2) AND ($3::uuid IS NULL OR project_id = $3)
		  AND ($4 = 0 OR status = $4) AND ($5::timestamptz IS NULL OR (created_at, id) < ($5, $6))
		ORDER BY created_at DESC, id DESC LIMIT $7`, orgID, f.IssueID, f.ProjectID, f.Status, f.Before, f.BeforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AiRun
	for rows.Next() {
		r, err := scanAiRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClaimAiRuns moves up to limit queued runs to "preparing" for worker.
func ClaimAiRuns(ctx context.Context, q Querier, limit int, worker string) ([]*AiRun, error) {
	rows, err := q.Query(ctx, `
		UPDATE ai_runs SET status = 2, worker = $2, started_at = now()
		WHERE id IN (SELECT id FROM ai_runs WHERE status = 1 ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING `+aiRunColumns, limit, worker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AiRun
	for rows.Next() {
		r, err := scanAiRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkAiRunRunning records the checkout of a preparing run and moves it to
// "running". It reports false when the run is no longer preparing (for
// example because it was cancelled).
func MarkAiRunRunning(ctx context.Context, q Querier, id uuid.UUID, model, baseCommit, baseBranch string) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE ai_runs SET status = 3, model = $2, base_commit = $3, base_branch = $4
		WHERE id = $1 AND status = 2`, id, model, baseCommit, baseBranch)
	return tag.RowsAffected() == 1, err
}

// FinishAiRun stores the outcome of an active run. It reports false when
// the run was finished elsewhere in the meantime (cancelled by a user).
func FinishAiRun(ctx context.Context, q Querier, r *AiRun) (bool, error) {
	result, _ := json.Marshal(r.Result)
	files, _ := json.Marshal(r.Files)
	usage, _ := json.Marshal(r.Usage)
	if r.Files == nil {
		files = []byte("[]")
	}
	tag, err := q.Exec(ctx, `
		UPDATE ai_runs SET status = $2, model = $3, base_commit = $4, base_branch = $5, result = $6, analysis = $7, diff = $8,
		  files = $9, error = $10, usage = $11, finished_at = now()
		WHERE id = $1 AND status IN (1, 2, 3)`,
		r.ID, r.Status, r.Model, r.BaseCommit, r.BaseBranch, result, r.Analysis, r.Diff, files, r.Error, usage)
	return tag.RowsAffected() == 1, err
}

// CancelAiRun stops a queued or running run; ErrNotFound when the run is
// not active.
func CancelAiRun(ctx context.Context, q Querier, id uuid.UUID, reason string) (*AiRun, error) {
	return scanAiRun(q.QueryRow(ctx, `UPDATE ai_runs SET status = 6, error = $2, finished_at = now()
		WHERE id = $1 AND status IN (1, 2, 3) RETURNING `+aiRunColumns, id, reason))
}

// AiRunStatus returns the current status of a run.
func AiRunStatus(ctx context.Context, q Querier, id uuid.UUID) (int16, error) {
	var status int16
	err := q.QueryRow(ctx, `SELECT status FROM ai_runs WHERE id = $1`, id).Scan(&status)
	return status, notFound(err)
}

// SetAiRunPullRequest records the branch or pull request made from a run.
func SetAiRunPullRequest(ctx context.Context, q Querier, id uuid.UUID, pr *PullRequestInfo) error {
	js, err := json.Marshal(pr)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE ai_runs SET pull_request = $2 WHERE id = $1`, id, js)
	return err
}

// FailStaleAiRuns marks runs of this worker that were interrupted (for
// example by a restart) as failed and returns their issues.
func FailStaleAiRuns(ctx context.Context, q Querier, worker string) ([]uuid.UUID, error) {
	return issueIDs(q.Query(ctx, `UPDATE ai_runs SET status = 5, error = 'interrupted by a server restart', finished_at = now()
		WHERE worker = $1 AND status IN (2, 3) RETURNING issue_id`, worker))
}

// FailExpiredAiRuns marks runs that started before the given time and are
// still active as failed: no worker runs that long, so the worker is gone.
func FailExpiredAiRuns(ctx context.Context, q Querier, startedBefore time.Time) ([]uuid.UUID, error) {
	return issueIDs(q.Query(ctx, `UPDATE ai_runs SET status = 5, error = 'the worker stopped responding', finished_at = now()
		WHERE status IN (2, 3) AND started_at < $1 RETURNING issue_id`, startedBefore))
}

func issueIDs(rows pgx.Rows, err error) ([]uuid.UUID, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// AiLog is one line of a run's log.
type AiLog struct {
	Seq     int
	Time    time.Time
	Kind    int16
	Tool    string
	Message string
}

// AppendAiLogs stores log lines of a run.
func AppendAiLogs(ctx context.Context, q Querier, orgID, runID uuid.UUID, logs []AiLog) error {
	if len(logs) == 0 {
		return nil
	}
	var seqs []int32
	var times []time.Time
	var kinds []int16
	var tools, msgs []string
	for _, l := range logs {
		seqs, times, kinds, tools, msgs = append(seqs, int32(l.Seq)), append(times, l.Time), append(kinds, l.Kind), append(tools, l.Tool), append(msgs, l.Message)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO ai_run_logs (run_id, seq, org_id, time, kind, tool, message)
		SELECT $1, s, $2, t, k, tl, m FROM unnest($3::int[], $4::timestamptz[], $5::smallint[], $6::text[], $7::text[]) AS x(s, t, k, tl, m)
		ON CONFLICT DO NOTHING`, runID, orgID, seqs, times, kinds, tools, msgs)
	return err
}

// ListAiLogs returns log lines after seq.
func ListAiLogs(ctx context.Context, q Querier, runID uuid.UUID, after int) ([]AiLog, error) {
	rows, err := q.Query(ctx, `SELECT seq, time, kind, tool, message FROM ai_run_logs WHERE run_id = $1 AND seq > $2 ORDER BY seq`, runID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AiLog
	for rows.Next() {
		var l AiLog
		if err := rows.Scan(&l.Seq, &l.Time, &l.Kind, &l.Tool, &l.Message); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CountAiRuns counts runs of an organization (optionally one project and
// only automatic ones) created since.
func CountAiRuns(ctx context.Context, q Querier, orgID uuid.UUID, projectID *uuid.UUID, automaticOnly bool, since time.Time) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM ai_runs WHERE org_id = $1 AND ($2::uuid IS NULL OR project_id = $2)
		AND (NOT $3 OR trigger = 2) AND created_at >= $4`, orgID, projectID, automaticOnly, since).Scan(&n)
	return n, err
}

// RefreshIssueAiState derives the AI state shown on an issue from its
// latest run that was not cancelled.
func RefreshIssueAiState(ctx context.Context, q Querier, issueID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE issues SET ai_state = coalesce((
		  SELECT CASE WHEN r.status IN (1, 2, 3) THEN 2
		              WHEN r.status = 4 AND r.pull_request IS NOT NULL THEN 5
		              WHEN r.status = 4 AND r.diff <> '' THEN 4
		              WHEN r.status = 4 THEN 3
		              ELSE 6 END
		  FROM ai_runs r WHERE r.issue_id = issues.id AND r.status <> 6 ORDER BY r.created_at DESC LIMIT 1), 1)
		WHERE id = $1`, issueID)
	return err
}

// CountActiveAiRuns counts an organization's queued and running runs.
func CountActiveAiRuns(ctx context.Context, q Querier, orgID uuid.UUID) (queued, running int, err error) {
	err = q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status IN (2, 3))
		FROM ai_runs WHERE org_id = $1 AND status IN (1, 2, 3)`, orgID).Scan(&queued, &running)
	return queued, running, err
}

// AddAiRunUsage counts a started run in the organization's daily usage.
func AddAiRunUsage(ctx context.Context, q Querier, orgID uuid.UUID, at time.Time) error {
	_, err := q.Exec(ctx, `INSERT INTO usage_daily AS u (org_id, day, ai_runs) VALUES ($1, $2::timestamptz::date, 1)
		ON CONFLICT (org_id, day) DO UPDATE SET ai_runs = u.ai_runs + 1`, orgID, at)
	return err
}
