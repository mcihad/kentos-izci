package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Feedback is what an end user wrote about a problem.
type Feedback struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	FeedbackID  uuid.UUID
	EventID     *uuid.UUID
	IssueID     *uuid.UUID
	Name        string
	Email       string
	Message     string
	URL         string
	Environment string
	Release     string
	Source      string
	CustomerKey string
	UserLabel   string
	// Payload is the normalized feedback event as protojson, when any.
	Payload   []byte
	CreatedAt time.Time
}

// InsertFeedback stores feedback and resolves the issue of its event when
// that event is already stored. A feedback id seen before is ignored; the
// result reports whether a row was inserted.
func InsertFeedback(ctx context.Context, q Querier, f *Feedback) (bool, error) {
	err := q.QueryRow(ctx, `
		INSERT INTO user_feedback (org_id, project_id, feedback_id, event_id, issue_id, name, email, message, url,
		  environment, release, source, customer_key, user_label, payload)
		VALUES ($1, $2, $3, $4,
		  (SELECT issue_id FROM events WHERE $4::uuid IS NOT NULL AND project_id = $2 AND event_id = $4 LIMIT 1),
		  $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (project_id, feedback_id) DO NOTHING
		RETURNING id, issue_id, created_at`,
		f.OrgID, f.ProjectID, f.FeedbackID, f.EventID, f.Name, f.Email, f.Message, f.URL,
		f.Environment, f.Release, f.Source, f.CustomerKey, f.UserLabel, f.Payload).Scan(&f.ID, &f.IssueID, &f.CreatedAt)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// LinkFeedback resolves the issue of recent feedback whose event was stored
// after the feedback arrived.
func LinkFeedback(ctx context.Context, q Querier, since time.Time) (int64, error) {
	tag, err := q.Exec(ctx, `
		UPDATE user_feedback f SET issue_id = e.issue_id
		FROM events e
		WHERE f.issue_id IS NULL AND f.event_id IS NOT NULL AND f.created_at >= $1
		  AND e.project_id = f.project_id AND e.event_id = f.event_id AND e.received_at >= $1 - interval '1 day'`, since)
	return tag.RowsAffected(), err
}

// FeedbackRow is feedback with the names of its project and issue.
type FeedbackRow struct {
	Feedback
	ProjectSlug     string
	ProjectName     string
	ProjectPlatform string
	IssueShortSeq   int32
	IssueTitle      string
	CustomerName    string
}

// FeedbackFilter selects feedback; zero values mean "any".
type FeedbackFilter struct {
	ProjectID *uuid.UUID
	IssueID   *uuid.UUID
	// EventID limits to feedback about one event (with ProjectID).
	EventID *uuid.UUID
	Query   string
	// Before is the keyset cursor (created_at, id) of the previous page.
	Before   *time.Time
	BeforeID uuid.UUID
	Limit    int
}

// ListFeedback returns feedback newest first.
func ListFeedback(ctx context.Context, q Querier, f FeedbackFilter) ([]FeedbackRow, error) {
	pattern := ""
	if s := strings.TrimSpace(f.Query); s != "" {
		pattern = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
	}
	rows, err := q.Query(ctx, `
		SELECT f.id, f.org_id, f.project_id, f.feedback_id, f.event_id, f.issue_id, f.name, f.email, f.message, f.url,
		  f.environment, f.release, f.source, f.customer_key, f.user_label, f.payload, f.created_at,
		  p.slug, p.name, p.platform, coalesce(i.short_seq, 0), coalesce(i.title, ''), coalesce(c.name, '')
		FROM user_feedback f
		JOIN projects p ON p.id = f.project_id
		LEFT JOIN issues i ON i.id = f.issue_id
		LEFT JOIN customers c ON c.org_id = f.org_id AND c.key = f.customer_key AND f.customer_key <> ''
		WHERE ($1::uuid IS NULL OR f.project_id = $1)
		  AND ($2::uuid IS NULL OR f.issue_id = $2)
		  AND ($3::uuid IS NULL OR f.event_id = $3)
		  AND ($4 = '' OR f.message ILIKE $4 OR f.name ILIKE $4 OR f.email ILIKE $4)
		  AND ($5::timestamptz IS NULL OR (f.created_at, f.id) < ($5, $6))
		ORDER BY f.created_at DESC, f.id DESC LIMIT $7`,
		f.ProjectID, f.IssueID, f.EventID, pattern, f.Before, f.BeforeID, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FeedbackRow, error) {
		var x FeedbackRow
		err := r.Scan(&x.ID, &x.OrgID, &x.ProjectID, &x.FeedbackID, &x.EventID, &x.IssueID, &x.Name, &x.Email, &x.Message,
			&x.URL, &x.Environment, &x.Release, &x.Source, &x.CustomerKey, &x.UserLabel, &x.Payload, &x.CreatedAt,
			&x.ProjectSlug, &x.ProjectName, &x.ProjectPlatform, &x.IssueShortSeq, &x.IssueTitle, &x.CustomerName)
		return x, err
	})
}
