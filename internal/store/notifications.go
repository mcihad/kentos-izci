package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Channel kinds (izci.api.v1.ChannelKind).
const ChannelTelegram int16 = 1

// Outbox notification kinds.
const (
	NotifyNewIssue   int16 = 1
	NotifyRegression int16 = 2
	NotifyFrequency  int16 = 3
	NotifyTest       int16 = 4
)

// Link code kinds.
const (
	LinkTelegramChat    int16 = 1
	LinkTelegramAccount int16 = 2
)

// TelegramTarget is where a Telegram channel delivers: a chat and, in forum
// supergroups, a topic.
type TelegramTarget struct {
	ChatID   int64  `json:"chat_id"`
	ThreadID int64  `json:"thread_id,omitempty"`
	Title    string `json:"title,omitempty"`
	Type     string `json:"type,omitempty"`
	Username string `json:"username,omitempty"`
}

// Channel is a notification destination of an organization.
type Channel struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	ProjectID      *uuid.UUID
	Kind           int16
	Name           string
	Telegram       TelegramTarget
	Muted          bool
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	LastDeliveryAt *time.Time
	LastError      string
}

const channelColumns = `id, org_id, project_id, kind, name, config, muted, created_by, created_at, last_delivery_at, last_error`

func scanChannel(row interface{ Scan(...any) error }) (*Channel, error) {
	var c Channel
	var cfg []byte
	if err := row.Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.Kind, &c.Name, &cfg, &c.Muted, &c.CreatedBy, &c.CreatedAt,
		&c.LastDeliveryAt, &c.LastError); err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal(cfg, &c.Telegram); err != nil {
		return nil, fmt.Errorf("decode channel config: %w", err)
	}
	return &c, nil
}

// ListChannels returns the channels of an organization, newest first.
func ListChannels(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Channel, error) {
	rows, err := q.Query(ctx, `SELECT `+channelColumns+` FROM notification_channels WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChannel returns a channel by id.
func GetChannel(ctx context.Context, q Querier, id uuid.UUID) (*Channel, error) {
	return scanChannel(q.QueryRow(ctx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = $1`, id))
}

// UpsertTelegramChannel links a chat (and topic) to an organization; linking
// an already linked chat updates its name, scope and title.
func UpsertTelegramChannel(ctx context.Context, q Querier, orgID uuid.UUID, projectID, createdBy *uuid.UUID, name string, t TelegramTarget) (*Channel, bool, error) {
	cfg, err := json.Marshal(t)
	if err != nil {
		return nil, false, err
	}
	row := q.QueryRow(ctx, `
		INSERT INTO notification_channels AS c (org_id, project_id, kind, name, config, created_by)
		VALUES ($1, $2, 1, $3, $4, $5)
		ON CONFLICT (org_id, (config ->> 'chat_id'), (coalesce(config ->> 'thread_id', '0'))) WHERE kind = 1
		DO UPDATE SET project_id = excluded.project_id, config = excluded.config, name = excluded.name, last_error = ''
		RETURNING `+prefixed("c.", channelColumns)+`, old.id IS NULL`, orgID, projectID, name, cfg, createdBy)
	var created bool
	c, err := scanChannel(extraScanner{row, []any{&created}})
	return c, created, err
}

// UpdateChannel changes the name and mute state of a channel.
func UpdateChannel(ctx context.Context, q Querier, id uuid.UUID, name string, muted bool) (*Channel, error) {
	return scanChannel(q.QueryRow(ctx, `
		UPDATE notification_channels SET name = CASE WHEN $2 = '' THEN name ELSE $2 END, muted = $3
		WHERE id = $1 RETURNING `+channelColumns, id, name, muted))
}

// DeleteChannel removes a channel and detaches it from alert rules.
func DeleteChannel(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = q.Exec(ctx, `UPDATE alert_rules SET channel_ids = array_remove(channel_ids, $1) WHERE $1 = ANY(channel_ids)`, id)
	return err
}

// RecordDelivery stores the outcome of the latest delivery to a channel.
func RecordDelivery(ctx context.Context, q Querier, id uuid.UUID, deliveryErr string) error {
	if deliveryErr == "" {
		_, err := q.Exec(ctx, `UPDATE notification_channels SET last_delivery_at = now(), last_error = '' WHERE id = $1`, id)
		return err
	}
	_, err := q.Exec(ctx, `UPDATE notification_channels SET last_error = $2 WHERE id = $1`, id, deliveryErr)
	return err
}

// MigrateTelegramChat follows a group that Telegram upgraded to a
// supergroup (the chat id changes).
func MigrateTelegramChat(ctx context.Context, q Querier, oldID, newID int64) error {
	_, err := q.Exec(ctx, `
		UPDATE notification_channels SET config = jsonb_set(config, '{chat_id}', to_jsonb($2::bigint))
		WHERE kind = 1 AND (config ->> 'chat_id')::bigint = $1`, oldID, newID)
	return err
}

// RuleTrigger is when a rule fires (stored as JSON).
type RuleTrigger struct {
	Kind          int16 `json:"kind"`
	Threshold     int   `json:"threshold,omitempty"`
	WindowSeconds int   `json:"window_seconds,omitempty"`
}

// RuleFilters narrow the events a rule reacts to (stored as JSON).
type RuleFilters struct {
	MinLevel      int16             `json:"min_level,omitempty"`
	Environments  []string          `json:"environments,omitempty"`
	Customers     []string          `json:"customers,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	UnhandledOnly bool              `json:"unhandled_only,omitempty"`
}

// AlertRule decides which events notify which channels.
type AlertRule struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	ProjectID       *uuid.UUID
	Name            string
	Enabled         bool
	Trigger         RuleTrigger
	Filters         RuleFilters
	ChannelIDs      []uuid.UUID
	ThrottleSeconds int32
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastTriggeredAt *time.Time
}

const ruleColumns = `id, org_id, project_id, name, enabled, trigger, filters, channel_ids, throttle_seconds, created_by,
	created_at, updated_at, last_triggered_at`

func scanRule(row interface{ Scan(...any) error }) (*AlertRule, error) {
	var r AlertRule
	var trigger, filters []byte
	if err := row.Scan(&r.ID, &r.OrgID, &r.ProjectID, &r.Name, &r.Enabled, &trigger, &filters, &r.ChannelIDs,
		&r.ThrottleSeconds, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &r.LastTriggeredAt); err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal(trigger, &r.Trigger); err != nil {
		return nil, fmt.Errorf("decode rule trigger: %w", err)
	}
	if err := json.Unmarshal(filters, &r.Filters); err != nil {
		return nil, fmt.Errorf("decode rule filters: %w", err)
	}
	return &r, nil
}

// ListAlertRules returns rules of organizations (all rules of each). When
// enabledOnly is set, disabled rules are skipped.
func ListAlertRules(ctx context.Context, q Querier, orgIDs []uuid.UUID, enabledOnly bool) ([]*AlertRule, error) {
	rows, err := q.Query(ctx, `SELECT `+ruleColumns+` FROM alert_rules
		WHERE org_id = ANY($1) AND (NOT $2 OR enabled) ORDER BY created_at`, orgIDs, enabledOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AlertRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveAlertRule inserts a rule (zero ID) or updates it.
func SaveAlertRule(ctx context.Context, q Querier, r *AlertRule) (*AlertRule, error) {
	trigger, err := json.Marshal(r.Trigger)
	if err != nil {
		return nil, err
	}
	filters, err := json.Marshal(r.Filters)
	if err != nil {
		return nil, err
	}
	if r.ChannelIDs == nil {
		r.ChannelIDs = []uuid.UUID{}
	}
	if r.ID == uuid.Nil {
		return scanRule(q.QueryRow(ctx, `
			INSERT INTO alert_rules (org_id, project_id, name, enabled, trigger, filters, channel_ids, throttle_seconds, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+ruleColumns,
			r.OrgID, r.ProjectID, r.Name, r.Enabled, trigger, filters, r.ChannelIDs, r.ThrottleSeconds, r.CreatedBy))
	}
	return scanRule(q.QueryRow(ctx, `
		UPDATE alert_rules SET project_id = $2, name = $3, enabled = $4, trigger = $5, filters = $6, channel_ids = $7,
		  throttle_seconds = $8, updated_at = now()
		WHERE id = $1 RETURNING `+ruleColumns,
		r.ID, r.ProjectID, r.Name, r.Enabled, trigger, filters, r.ChannelIDs, r.ThrottleSeconds))
}

// DeleteAlertRule removes a rule.
func DeleteAlertRule(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// OutboxItem is a pending notification.
type OutboxItem struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ChannelID uuid.UUID
	RuleID    *uuid.UUID
	IssueID   *uuid.UUID
	Kind      int16
	Payload   map[string]any
	Attempts  int32
	CreatedAt time.Time
}

// EnqueueNotifications writes outbox rows (in the transaction that caused
// them).
func EnqueueNotifications(ctx context.Context, q Querier, items []OutboxItem) error {
	if len(items) == 0 {
		return nil
	}
	var (
		orgs, channels []uuid.UUID
		rules, issues  []*uuid.UUID
		kinds          []int16
		payloads       []string
	)
	for _, it := range items {
		p := it.Payload
		if p == nil {
			p = map[string]any{}
		}
		js, err := json.Marshal(p)
		if err != nil {
			return err
		}
		orgs, channels, rules, issues = append(orgs, it.OrgID), append(channels, it.ChannelID), append(rules, it.RuleID), append(issues, it.IssueID)
		kinds, payloads = append(kinds, it.Kind), append(payloads, string(js))
	}
	_, err := q.Exec(ctx, `
		INSERT INTO notification_outbox (org_id, channel_id, rule_id, issue_id, kind, payload)
		SELECT o, c, r, i, k, p::jsonb FROM unnest($1::uuid[], $2::uuid[], $3::uuid[], $4::uuid[], $5::smallint[], $6::text[])
		  AS t(o, c, r, i, k, p)`, orgs, channels, rules, issues, kinds, payloads)
	return err
}

// ClaimNotifications leases up to limit due outbox rows for delivery: they
// become invisible to other dispatchers for lease, after which an
// unfinished delivery is retried. No transaction stays open while the
// notifications are sent.
func ClaimNotifications(ctx context.Context, q Querier, limit int, lease time.Duration) ([]OutboxItem, error) {
	rows, err := q.Query(ctx, `
		UPDATE notification_outbox SET next_attempt_at = now() + $2::interval
		WHERE id IN (
		  SELECT id FROM notification_outbox WHERE sent_at IS NULL AND next_attempt_at <= now()
		  ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, org_id, channel_id, rule_id, issue_id, kind, payload, attempts, created_at`, limit, lease)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var payload []byte
		if err := rows.Scan(&it.ID, &it.OrgID, &it.ChannelID, &it.RuleID, &it.IssueID, &it.Kind, &payload, &it.Attempts, &it.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &it.Payload)
		out = append(out, it)
	}
	return out, rows.Err()
}

// MarkNotificationSent records a delivery.
func MarkNotificationSent(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE notification_outbox SET sent_at = now(), attempts = attempts + 1, error = '' WHERE id = $1`, id)
	return err
}

// MarkNotificationFailed schedules another attempt at next, or gives up when
// next is nil.
func MarkNotificationFailed(ctx context.Context, q Querier, id uuid.UUID, next *time.Time, msg string) error {
	_, err := q.Exec(ctx, `
		UPDATE notification_outbox SET attempts = attempts + 1, error = $3,
		  next_attempt_at = coalesce($2, 'infinity'::timestamptz)
		WHERE id = $1`, id, next, msg)
	return err
}

// RecentlyNotified returns the (rule, issue) pairs that produced a
// notification after since, for throttling.
func RecentlyNotified(ctx context.Context, q Querier, ruleIDs, issueIDs []uuid.UUID, since time.Time) (map[[2]uuid.UUID]time.Time, error) {
	out := map[[2]uuid.UUID]time.Time{}
	if len(ruleIDs) == 0 || len(issueIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT rule_id, issue_id, max(created_at) FROM notification_outbox
		WHERE rule_id = ANY($1) AND issue_id = ANY($2) AND created_at > $3
		GROUP BY 1, 2`, ruleIDs, issueIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r, i uuid.UUID
		var at time.Time
		if err := rows.Scan(&r, &i, &at); err != nil {
			return nil, err
		}
		out[[2]uuid.UUID{r, i}] = at
	}
	return out, rows.Err()
}

// Thread is the message posted for an issue in a channel.
type Thread struct {
	ChannelID  uuid.UUID
	IssueID    uuid.UUID
	MessageID  int64
	LastSentAt time.Time
	LastCount  int64
	// Kind and Payload of the notification the message shows.
	Kind    int16
	Payload map[string]any
}

const threadColumns = `t.channel_id, t.issue_id, t.message_id, t.last_sent_at, t.last_count, t.kind, t.payload`

func scanThread(row interface{ Scan(...any) error }) (*Thread, error) {
	var t Thread
	var payload []byte
	if err := row.Scan(&t.ChannelID, &t.IssueID, &t.MessageID, &t.LastSentAt, &t.LastCount, &t.Kind, &payload); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(payload, &t.Payload)
	return &t, nil
}

// GetThread returns the message posted for an issue in a channel.
func GetThread(ctx context.Context, q Querier, channelID, issueID uuid.UUID) (*Thread, error) {
	return scanThread(q.QueryRow(ctx, `SELECT `+threadColumns+` FROM notification_threads t
		WHERE t.channel_id = $1 AND t.issue_id = $2`, channelID, issueID))
}

// SaveThread records the message posted for an issue in a channel.
func SaveThread(ctx context.Context, q Querier, orgID uuid.UUID, t Thread) error {
	payload := t.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	js, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO notification_threads (channel_id, issue_id, org_id, message_id, last_sent_at, last_count, kind, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (channel_id, issue_id) DO UPDATE SET message_id = excluded.message_id,
		  last_sent_at = excluded.last_sent_at, last_count = excluded.last_count, kind = excluded.kind, payload = excluded.payload`,
		t.ChannelID, t.IssueID, orgID, t.MessageID, t.LastSentAt, t.LastCount, t.Kind, string(js))
	return err
}

// ThreadsOfMessage returns the threads that point at a Telegram message (to
// update it after a button press).
func ThreadsOfMessage(ctx context.Context, q Querier, chatID, messageID int64) ([]*Thread, error) {
	rows, err := q.Query(ctx, `
		SELECT `+threadColumns+`
		FROM notification_threads t JOIN notification_channels c ON c.id = t.channel_id
		WHERE c.kind = 1 AND (c.config ->> 'chat_id')::bigint = $1 AND t.message_id = $2`, chatID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// LinkCode is a one-time code redeemed through the bot.
type LinkCode struct {
	Code      string
	Kind      int16
	OrgID     *uuid.UUID
	ProjectID *uuid.UUID
	UserID    *uuid.UUID
	ExpiresAt time.Time
}

// CreateLinkCode stores a one-time code.
func CreateLinkCode(ctx context.Context, q Querier, c LinkCode) error {
	_, err := q.Exec(ctx, `INSERT INTO link_codes (code, kind, org_id, project_id, user_id, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		c.Code, c.Kind, c.OrgID, c.ProjectID, c.UserID, c.ExpiresAt)
	return err
}

// RedeemLinkCode consumes a valid code once.
func RedeemLinkCode(ctx context.Context, q Querier, code string) (*LinkCode, error) {
	var c LinkCode
	err := q.QueryRow(ctx, `
		UPDATE link_codes SET used_at = now()
		WHERE code = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING code, kind, org_id, project_id, user_id, expires_at`, code).Scan(&c.Code, &c.Kind, &c.OrgID, &c.ProjectID, &c.UserID, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}
