package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// Kinds of personal notifications (izci.api.v1.UserNotificationKind).
const (
	NotifyAssigned  int16 = 1
	NotifyMentioned int16 = 2
	NotifyRegressed int16 = 3
	NotifyCommented int16 = 4
)

// FollowIssues makes a member follow issues.
func FollowIssues(ctx context.Context, q Querier, orgID uuid.UUID, issueIDs []uuid.UUID, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `INSERT INTO issue_subscriptions (org_id, issue_id, user_id)
		SELECT $1, unnest($2::uuid[]), $3 ON CONFLICT DO NOTHING`, orgID, issueIDs, userID)
	return err
}

// UnfollowIssue makes a member stop following an issue.
func UnfollowIssue(ctx context.Context, q Querier, issueID, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM issue_subscriptions WHERE issue_id = $1 AND user_id = $2`, issueID, userID)
	return err
}

// Following reports whether a member follows an issue.
func Following(ctx context.Context, q Querier, issueID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_subscriptions WHERE issue_id = $1 AND user_id = $2)`,
		issueID, userID).Scan(&ok)
	return ok, err
}

// IssueFollowers returns the followers of issues who are still active
// members of the issue's organization.
func IssueFollowers(ctx context.Context, q Querier, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT s.issue_id, s.user_id FROM issue_subscriptions s
		JOIN org_members m ON m.org_id = s.org_id AND m.user_id = s.user_id
		JOIN users u ON u.id = s.user_id AND NOT u.disabled
		WHERE s.issue_id = ANY($1)`, issueIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var issue, user uuid.UUID
		if err := rows.Scan(&issue, &user); err != nil {
			return nil, err
		}
		out[issue] = append(out[issue], user)
	}
	return out, rows.Err()
}

// PersonalNotification is a notification for one member.
type PersonalNotification struct {
	OrgID   uuid.UUID
	UserID  uuid.UUID
	IssueID uuid.UUID
	Kind    int16
	ActorID *uuid.UUID
	Data    map[string]string
}

// AddPersonalNotifications stores notifications; the delivery worker sends
// them by e-mail and Telegram.
func AddPersonalNotifications(ctx context.Context, q Querier, ns []PersonalNotification) error {
	if len(ns) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, n := range ns {
		data, _ := json.Marshal(n.Data)
		if n.Data == nil {
			data = []byte("{}")
		}
		b.Queue(`INSERT INTO user_notifications (org_id, user_id, issue_id, kind, actor_id, data) VALUES ($1, $2, $3, $4, $5, $6)`,
			n.OrgID, n.UserID, n.IssueID, n.Kind, n.ActorID, data)
	}
	return q.SendBatch(ctx, b).Close()
}

// InboxEntry is a notification with what the inbox shows of its issue.
type InboxEntry struct {
	ID           uuid.UUID
	Kind         int16
	OrgID        uuid.UUID
	OrgSlug      string
	OrgName      string
	IssueID      uuid.UUID
	ProjectSlug  string
	IssueSeq     int32
	IssueTitle   string
	ActorID      *uuid.UUID
	ActorName    string
	ActorEmail   string
	Data         map[string]string
	CreatedAt    time.Time
	ReadAt       *time.Time
	UserID       uuid.UUID
	Attempts     int16
	DeliveredAt  *time.Time
	IssueLevel   int16
	IssueCulprit string
}

const inboxSelect = `
	SELECT n.id, n.kind, n.org_id, o.slug, o.name, n.issue_id, p.slug, i.short_seq, i.title, n.actor_id,
	       coalesce(a.name, ''), coalesce(a.email, ''), n.data, n.created_at, n.read_at, n.user_id, n.attempts,
	       n.delivered_at, i.level, i.culprit
	FROM user_notifications n
	JOIN organizations o ON o.id = n.org_id
	JOIN issues i ON i.id = n.issue_id
	JOIN projects p ON p.id = i.project_id
	LEFT JOIN users a ON a.id = n.actor_id`

func scanInbox(rows pgx.Rows) ([]*InboxEntry, error) {
	defer rows.Close()
	var out []*InboxEntry
	for rows.Next() {
		var e InboxEntry
		var data []byte
		if err := rows.Scan(&e.ID, &e.Kind, &e.OrgID, &e.OrgSlug, &e.OrgName, &e.IssueID, &e.ProjectSlug, &e.IssueSeq, &e.IssueTitle,
			&e.ActorID, &e.ActorName, &e.ActorEmail, &data, &e.CreatedAt, &e.ReadAt, &e.UserID, &e.Attempts, &e.DeliveredAt,
			&e.IssueLevel, &e.IssueCulprit); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(data, &e.Data)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ListInbox lists a member's notifications in the organizations they still
// belong to (only org when set), newest first, before the cursor (system
// scope).
func ListInbox(ctx context.Context, q Querier, userID uuid.UUID, org *uuid.UUID, unreadOnly bool, before *time.Time, beforeID uuid.UUID,
	limit int) ([]*InboxEntry, error) {
	rows, err := q.Query(ctx, inboxSelect+`
		JOIN org_members m ON m.org_id = n.org_id AND m.user_id = n.user_id
		WHERE n.user_id = $1 AND ($6::uuid IS NULL OR n.org_id = $6) AND (NOT $2 OR n.read_at IS NULL)
		  AND ($3::timestamptz IS NULL OR (n.created_at, n.id) < ($3, $4))
		ORDER BY n.created_at DESC, n.id DESC LIMIT $5`, userID, unreadOnly, before, beforeID, limit, org)
	if err != nil {
		return nil, err
	}
	return scanInbox(rows)
}

// UnreadCount counts a member's unread notifications (only in org when
// set; system scope).
func UnreadCount(ctx context.Context, q Querier, userID uuid.UUID, org *uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM user_notifications n
		JOIN org_members m ON m.org_id = n.org_id AND m.user_id = n.user_id
		WHERE n.user_id = $1 AND ($2::uuid IS NULL OR n.org_id = $2) AND n.read_at IS NULL`, userID, org).Scan(&n)
	return n, err
}

// MarkRead marks a member's notifications read: ids, or all when ids is
// nil (only in org when set; system scope).
func MarkRead(ctx context.Context, q Querier, userID uuid.UUID, org *uuid.UUID, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE user_notifications SET read_at = now()
		WHERE user_id = $1 AND ($3::uuid IS NULL OR org_id = $3) AND read_at IS NULL AND ($2::uuid[] IS NULL OR id = ANY($2))`,
		userID, ids, org)
	return err
}

// ClaimUndelivered takes notifications due for delivery and schedules
// their next attempt, so that other instances skip them meanwhile
// (system scope). Notifications older than a day are not sent any more.
func ClaimUndelivered(ctx context.Context, q Querier, limit int) ([]*InboxEntry, error) {
	rows, err := q.Query(ctx, `
		UPDATE user_notifications SET attempts = attempts + 1,
		  next_attempt_at = now() + interval '2 minutes' * power(2, attempts)
		WHERE id IN (SELECT id FROM user_notifications
		  WHERE delivered_at IS NULL AND next_attempt_at <= now() AND created_at > now() - interval '1 day'
		  ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id`, limit)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err = q.Query(ctx, inboxSelect+` WHERE n.id = ANY($1) ORDER BY n.user_id, n.created_at`, ids)
	if err != nil {
		return nil, err
	}
	return scanInbox(rows)
}

// MarkDelivered records that notifications were sent, or will not be.
func MarkDelivered(ctx context.Context, q Querier, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE user_notifications SET delivered_at = now() WHERE id = ANY($1)`, ids)
	return err
}

// DefaultNotificationSettings are the channels of members who never chose:
// e-mail and Telegram for what is about them, the inbox alone for comments
// on issues they follow.
func DefaultNotificationSettings() *apiv1.NotificationSettings {
	both := func() *apiv1.NotificationChannels { return &apiv1.NotificationChannels{Email: true, Telegram: true} }
	return &apiv1.NotificationSettings{Assigned: both(), Mentioned: both(), Regressed: both(),
		Commented: &apiv1.NotificationChannels{}}
}

// GetNotificationSettings returns a member's settings, defaults when unset.
func GetNotificationSettings(ctx context.Context, q Querier, userID uuid.UUID) (*apiv1.NotificationSettings, error) {
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT notification_settings FROM users WHERE id = $1`, userID).Scan(&raw); err != nil {
		return nil, notFound(err)
	}
	return decodeNotificationSettings(raw), nil
}

func decodeNotificationSettings(raw []byte) *apiv1.NotificationSettings {
	s := &apiv1.NotificationSettings{}
	if len(raw) == 0 || string(raw) == "{}" || unmarshalJSON(raw, s) != nil {
		return DefaultNotificationSettings()
	}
	d := DefaultNotificationSettings()
	if s.Assigned == nil {
		s.Assigned = d.Assigned
	}
	if s.Mentioned == nil {
		s.Mentioned = d.Mentioned
	}
	if s.Regressed == nil {
		s.Regressed = d.Regressed
	}
	if s.Commented == nil {
		s.Commented = d.Commented
	}
	return s
}

// SetNotificationSettings stores a member's settings.
func SetNotificationSettings(ctx context.Context, q Querier, userID uuid.UUID, s *apiv1.NotificationSettings) error {
	// Every kind is written (set messages are kept even when empty), so
	// that later defaults do not change a choice.
	raw, err := jsonOut.Marshal(s)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE users SET notification_settings = $2 WHERE id = $1`, userID, raw)
	return err
}

// Recipient is what delivery needs of a member.
type Recipient struct {
	ID             uuid.UUID
	Email          string
	Name           string
	TelegramUserID *int64
	Settings       *apiv1.NotificationSettings
}

// Recipients loads the members personal notifications go to.
func Recipients(ctx context.Context, q Querier, ids []uuid.UUID) (map[uuid.UUID]*Recipient, error) {
	rows, err := q.Query(ctx, `SELECT id, email, name, telegram_user_id, notification_settings FROM users
		WHERE id = ANY($1) AND NOT disabled`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*Recipient{}
	for rows.Next() {
		var r Recipient
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Email, &r.Name, &r.TelegramUserID, &raw); err != nil {
			return nil, err
		}
		r.Settings = decodeNotificationSettings(raw)
		out[r.ID] = &r
	}
	return out, rows.Err()
}
