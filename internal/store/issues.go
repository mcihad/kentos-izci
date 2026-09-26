package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Issue status and activity values (izci.api.v1.IssueStatus, ActivityKind).
const (
	IssueUnresolved int16 = 1
	IssueResolved   int16 = 2
	IssueIgnored    int16 = 3

	ActivityStatusChanged   int16 = 3
	ActivityAssigned        int16 = 4
	ActivityPriorityChanged int16 = 5
	ActivityNotified        int16 = 10
)

// ResolveIssues marks issues resolved. inRelease records the release that
// fixed them; inNextRelease resolves them in whatever release comes after
// the latest one seen.
func ResolveIssues(ctx context.Context, q Querier, orgID uuid.UUID, ids []uuid.UUID, actor uuid.UUID, inRelease string, inNextRelease bool) error {
	_, err := q.Exec(ctx, `
		UPDATE issues SET status = 2, substatus = 1, resolved_at = now(), resolved_by = $2,
		  resolved_in_release = CASE WHEN $3 <> '' THEN $3 WHEN $4 THEN last_release ELSE '' END,
		  resolved_in_next_release = $4 AND $3 = '',
		  snooze_until = NULL, snooze_count = NULL, snooze_base = NULL, snooze_until_escalating = false, updated_at = now()
		WHERE id = ANY($1) AND org_id = $5`, ids, actor, inRelease, inNextRelease, orgID)
	return err
}

// IgnoreOptions ends an ignore period: at a time, after count more events
// or when the issue escalates. All nil/false means forever.
type IgnoreOptions struct {
	Until           *time.Time
	Count           *int64
	UntilEscalating bool
}

// IgnoreIssues snoozes issues.
func IgnoreIssues(ctx context.Context, q Querier, orgID uuid.UUID, ids []uuid.UUID, o IgnoreOptions) error {
	_, err := q.Exec(ctx, `
		UPDATE issues SET status = 3, substatus = 1, snooze_until = $2, snooze_count = $3,
		  snooze_base = CASE WHEN $3::bigint IS NULL THEN NULL ELSE times_seen END,
		  snooze_until_escalating = $4, updated_at = now()
		WHERE id = ANY($1) AND org_id = $5`, ids, o.Until, o.Count, o.UntilEscalating, orgID)
	return err
}

// ReopenIssues marks resolved or ignored issues unresolved again.
func ReopenIssues(ctx context.Context, q Querier, orgID uuid.UUID, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE issues SET status = 1, substatus = 2, resolved_at = NULL, resolved_by = NULL, resolved_in_release = '',
		  resolved_in_next_release = false, snooze_until = NULL, snooze_count = NULL, snooze_base = NULL,
		  snooze_until_escalating = false, updated_at = now()
		WHERE id = ANY($1) AND org_id = $2`, ids, orgID)
	return err
}

// AddIssueActivity records an activity entry on each issue.
func AddIssueActivity(ctx context.Context, q Querier, orgID uuid.UUID, ids []uuid.UUID, kind int16, actor *uuid.UUID, data map[string]string) error {
	if data == nil {
		data = map[string]string{}
	}
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO issue_activity (org_id, issue_id, kind, actor_id, data)
		SELECT $1, id, $3, $4, $5::jsonb FROM issues WHERE id = ANY($2) AND org_id = $1`,
		orgID, ids, kind, actor, string(js))
	return err
}

// StatusName is the izci.api.v1.IssueStatus enum name used in activity data.
func StatusName(status int16) string {
	switch status {
	case IssueResolved:
		return "ISSUE_STATUS_RESOLVED"
	case IssueIgnored:
		return "ISSUE_STATUS_IGNORED"
	default:
		return "ISSUE_STATUS_UNRESOLVED"
	}
}
