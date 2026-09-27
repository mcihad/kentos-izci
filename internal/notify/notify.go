// Package notify delivers the notifications the alert engine queues in the
// transactional outbox to their channels: Telegram chats, e-mail, Slack,
// Microsoft Teams, webhooks and Jira.
//
// The Dispatcher claims due outbox rows (SKIP LOCKED, so several instances
// share the work), loads the issue a notification is about and hands both
// to the Sender registered for the channel's kind. Senders report
// temporary problems with RetryError and hopeless ones with PermanentError;
// other errors are retried with exponential backoff.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
)

// maxAttempts bounds delivery attempts of one notification.
const maxAttempts = 8

// IssueView is everything a notification shows about an issue.
type IssueView struct {
	ID          string
	ShortID     string
	OrgSlug     string
	OrgName     string
	ProjectSlug string
	ProjectName string
	Title       string
	Type        string
	Value       string
	Culprit     string
	Filename    string
	Lineno      uint32
	Level       int16
	Status      int16
	Unhandled   bool
	TimesSeen   int64
	Users       int32
	Customers   int32
	FirstSeen   time.Time
	LastSeen    time.Time
	// From the event that triggered the notification.
	EventID     string
	Customer    string
	Tenant      string
	Environment string
	Release     string
	User        string
	// StatusBy names who resolved or ignored the issue.
	StatusBy string
}

// Notification is one delivery: an outbox row, its channel and, unless it
// is a test, the issue it is about.
type Notification struct {
	Item    store.OutboxItem
	Channel *store.Channel
	// OrgName is the channel's organization.
	OrgName string
	// Issue is nil for test notifications.
	Issue *IssueView
	// Link opens the issue in izci.
	Link string
	// Thread is what an earlier delivery for the same issue recorded in
	// this channel (a Telegram message, an e-mail id, a Jira ticket).
	Thread *store.Thread
}

// Test reports a test notification.
func (n *Notification) Test() bool { return n.Item.Kind == store.NotifyTest }

// Receipt is what a sender reports after a delivery.
type Receipt struct {
	// Thread, when set, is kept for later notifications of the issue.
	Thread *store.Thread
	// Ticket is a ticket the delivery created (Jira).
	Ticket *Ticket
}

// Ticket is an issue created in an external tracker.
type Ticket struct {
	Key string
	URL string
}

// Sender delivers notifications of one channel kind.
type Sender interface {
	Send(ctx context.Context, n *Notification) (*Receipt, error)
}

// RetryError asks for another attempt after a delay (rate limits,
// temporary outages).
type RetryError struct {
	After  time.Duration
	Reason string
	// Err is the underlying error, if any (a StatusError).
	Err error
}

func (e *RetryError) Error() string { return e.Reason }
func (e *RetryError) Unwrap() error { return e.Err }

// PermanentError gives up the notification; the reason is shown on the
// channel (a revoked webhook, wrong credentials).
type PermanentError struct {
	Reason string
	// Err is the underlying error, if any (a StatusError).
	Err error
}

func (e *PermanentError) Error() string { return e.Reason }
func (e *PermanentError) Unwrap() error { return e.Err }

// StatusError is an unsuccessful answer of an external service, with the
// service's own message (in its language).
type StatusError struct {
	Service string
	Status  int
	Detail  string
}

func (e *StatusError) Error() string {
	s := fmt.Sprintf("%s answered %d", e.Service, e.Status)
	if e.Service == "jira" {
		s = fmt.Sprintf("Jira answered %d", e.Status)
	}
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// Permanent wraps a reason as a PermanentError.
func Permanent(format string, args ...any) error {
	return &PermanentError{Reason: fmt.Sprintf(format, args...)}
}

// KindName is the metrics label and log name of a channel kind.
func KindName(kind int16) string {
	switch kind {
	case store.ChannelTelegram:
		return "telegram"
	case store.ChannelEmail:
		return "email"
	case store.ChannelSlack:
		return "slack"
	case store.ChannelTeams:
		return "teams"
	case store.ChannelWebhook:
		return "webhook"
	case store.ChannelJira:
		return "jira"
	}
	return "other"
}

// Dispatcher delivers queued notifications.
type Dispatcher struct {
	db        *db.DB
	log       *slog.Logger
	publicURL string

	mu      sync.RWMutex
	senders map[int16]Sender
	wake    chan struct{}
}

// NewDispatcher creates a dispatcher; register senders before Run.
func NewDispatcher(database *db.DB, log *slog.Logger, publicURL string) *Dispatcher {
	return &Dispatcher{
		db: database, log: log.With(slog.String("component", "notify")), publicURL: strings.TrimRight(publicURL, "/"),
		senders: map[int16]Sender{}, wake: make(chan struct{}, 1),
	}
}

// Register sets the sender of a channel kind.
func (d *Dispatcher) Register(kind int16, s Sender) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.senders[kind] = s
}

// Available reports whether a channel kind can be delivered.
func (d *Dispatcher) Available(kind int16) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.senders[kind] != nil
}

// Wake starts a delivery round without waiting for the next tick.
func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run delivers notifications until ctx is done.
func (d *Dispatcher) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		n, err := d.DispatchOnce(ctx)
		if err != nil && ctx.Err() == nil {
			d.log.Error("notification dispatch failed", slog.Any("err", err))
		}
		if n > 0 {
			continue // there may be more due
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.wake:
		}
	}
}

// DispatchOnce delivers due notifications and returns how many it claimed.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	var items []store.OutboxItem
	err := d.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		items, err = store.ClaimNotifications(ctx, tx, 20, 2*time.Minute)
		return err
	})
	if err != nil {
		return 0, err
	}
	// Deliveries to different channels do not wait for each other; one
	// channel's notifications go out in the order they were queued (the
	// claim returns rows in no particular order).
	slices.SortFunc(items, func(a, b store.OutboxItem) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return bytes.Compare(a.ID[:], b.ID[:])
	})
	byChannel := map[uuid.UUID][]store.OutboxItem{}
	var order []uuid.UUID
	for _, it := range items {
		if _, ok := byChannel[it.ChannelID]; !ok {
			order = append(order, it.ChannelID)
		}
		byChannel[it.ChannelID] = append(byChannel[it.ChannelID], it)
	}
	var wg sync.WaitGroup
	for _, id := range order {
		list := byChannel[id]
		wg.Go(func() {
			for _, it := range list {
				if ctx.Err() != nil {
					return
				}
				d.deliver(ctx, it)
			}
		})
	}
	wg.Wait()
	return len(items), nil
}

func (d *Dispatcher) deliver(ctx context.Context, it store.OutboxItem) {
	var ch *store.Channel
	var orgName string
	err := d.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if ch, err = store.GetChannel(ctx, tx, it.ChannelID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, ch.OrgID).Scan(&orgName)
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		d.finish(ctx, it, nil, "channel deleted", nil)
		return
	case err != nil:
		d.retry(ctx, it, nil, time.Now().Add(30*time.Second), err.Error())
		return
	case ch.Muted:
		d.finish(ctx, it, nil, "channel muted", nil)
		return
	}
	d.mu.RLock()
	sender := d.senders[ch.Kind]
	d.mu.RUnlock()
	if sender == nil {
		d.giveUp(ctx, it, ch, KindName(ch.Kind)+" notifications are not configured on this server")
		return
	}
	n := &Notification{Item: it, Channel: ch, OrgName: orgName}
	if !n.Test() {
		if it.IssueID == nil {
			d.finish(ctx, it, nil, "no issue", nil)
			return
		}
		n.Issue, err = LoadIssueView(ctx, d.db, *it.IssueID, it.Payload)
		if errors.Is(err, store.ErrNotFound) {
			d.finish(ctx, it, nil, "issue deleted", nil)
			return
		}
		if err != nil {
			d.retry(ctx, it, ch, time.Now().Add(30*time.Second), err.Error())
			return
		}
		n.Link = d.IssueURL(n.Issue)
		_ = d.db.InSystem(ctx, func(tx pgx.Tx) error {
			n.Thread, _ = store.GetThread(ctx, tx, ch.ID, *it.IssueID)
			return nil
		})
	}
	receipt, err := sender.Send(ctx, n)
	if err != nil {
		d.fail(ctx, it, ch, err)
		return
	}
	if receipt != nil && receipt.Thread != nil {
		if err := d.db.InSystem(ctx, func(tx pgx.Tx) error { return store.SaveThread(ctx, tx, ch.OrgID, *receipt.Thread) }); err != nil {
			d.log.Error("save notification thread", slog.Any("err", err))
		}
	}
	var ticket *Ticket
	if receipt != nil {
		ticket = receipt.Ticket
	}
	d.finish(ctx, it, ch, "", ticket)
}

// IssueURL links an issue in izci.
func (d *Dispatcher) IssueURL(v *IssueView) string {
	return d.publicURL + "/o/" + v.OrgSlug + "/issues/" + v.ShortID
}

// finish marks a notification handled; with a channel it also records the
// delivery on the channel and in the issue's activity.
func (d *Dispatcher) finish(ctx context.Context, it store.OutboxItem, ch *store.Channel, note string, ticket *Ticket) {
	label := "other"
	if ch != nil {
		label = KindName(ch.Kind)
		metrics.Notification(label, "delivered")
	} else {
		metrics.Notification(label, "skipped")
	}
	err := d.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.MarkNotificationSent(ctx, tx, it.ID); err != nil {
			return err
		}
		if ch == nil {
			if note != "" {
				_, err := tx.Exec(ctx, `UPDATE notification_outbox SET error = $2 WHERE id = $1`, it.ID, note)
				return err
			}
			return nil
		}
		if err := store.RecordDelivery(ctx, tx, ch.ID, ""); err != nil {
			return err
		}
		if it.IssueID == nil {
			return nil
		}
		if ticket != nil {
			return store.AddIssueActivity(ctx, tx, it.OrgID, []uuid.UUID{*it.IssueID}, store.ActivityTicketCreated, nil,
				map[string]string{"channel": ch.Name, "key": ticket.Key, "url": ticket.URL})
		}
		return store.AddIssueActivity(ctx, tx, it.OrgID, []uuid.UUID{*it.IssueID}, store.ActivityNotified, nil,
			map[string]string{"channel": ch.Name})
	})
	if err != nil {
		d.log.Error("record notification delivery", slog.Any("err", err))
	}
}

func (d *Dispatcher) retry(ctx context.Context, it store.OutboxItem, ch *store.Channel, at time.Time, msg string) {
	label := "other"
	if ch != nil {
		label = KindName(ch.Kind)
	}
	metrics.Notification(label, "retried")
	if err := d.db.InSystem(ctx, func(tx pgx.Tx) error { return store.MarkNotificationFailed(ctx, tx, it.ID, &at, msg) }); err != nil {
		d.log.Error("reschedule notification", slog.Any("err", err))
	}
}

// fail handles a delivery error: retries after the sender's delay, gives
// up on permanent errors and after too many attempts, and otherwise backs
// off exponentially.
func (d *Dispatcher) fail(ctx context.Context, it store.OutboxItem, ch *store.Channel, err error) {
	var retry *RetryError
	var permanent *PermanentError
	switch {
	case errors.As(err, &permanent):
		d.giveUp(ctx, it, ch, permanent.Reason)
	case int(it.Attempts)+1 >= maxAttempts:
		d.giveUp(ctx, it, ch, err.Error())
	case errors.As(err, &retry):
		d.retry(ctx, it, ch, time.Now().Add(retry.After), retry.Reason)
	default:
		backoff := min(10*time.Second<<it.Attempts, time.Hour)
		d.retry(ctx, it, ch, time.Now().Add(backoff), err.Error())
	}
}

func (d *Dispatcher) giveUp(ctx context.Context, it store.OutboxItem, ch *store.Channel, msg string) {
	metrics.Notification(KindName(ch.Kind), "dropped")
	d.log.Warn("notification dropped", slog.String("channel", ch.Name), slog.String("kind", KindName(ch.Kind)), slog.String("reason", msg))
	err := d.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.MarkNotificationFailed(ctx, tx, it.ID, nil, msg); err != nil {
			return err
		}
		return store.RecordDelivery(ctx, tx, ch.ID, msg)
	})
	if err != nil {
		d.log.Error("record failed notification", slog.Any("err", err))
	}
}

// LoadIssueView reads an issue and the event that triggered a notification
// (payload["event_id"], optional).
func LoadIssueView(ctx context.Context, database *db.DB, issueID uuid.UUID, payload map[string]any) (*IssueView, error) {
	v := &IssueView{ID: issueID.String()}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var (
			seq        int32
			meta       []byte
			resolvedBy *string
		)
		err := tx.QueryRow(ctx, `
			SELECT coalesce(i.short_seq, 0), p.slug, p.name, o.slug, o.name, i.title, i.type, i.value, i.culprit, i.metadata, i.level,
			       i.status, i.unhandled, i.times_seen, i.users_count, i.customers_count, i.first_seen, i.last_seen,
			       nullif(coalesce(u.name, u.email), '')
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			JOIN organizations o ON o.id = i.org_id
			LEFT JOIN users u ON u.id = i.resolved_by
			WHERE i.id = $1`, issueID).Scan(&seq, &v.ProjectSlug, &v.ProjectName, &v.OrgSlug, &v.OrgName, &v.Title, &v.Type,
			&v.Value, &v.Culprit, &meta, &v.Level, &v.Status, &v.Unhandled, &v.TimesSeen, &v.Users, &v.Customers, &v.FirstSeen,
			&v.LastSeen, &resolvedBy)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		v.ShortID = ingest.ShortID(v.ProjectSlug, seq)
		if v.Status == store.IssueResolved && resolvedBy != nil {
			v.StatusBy = *resolvedBy
		}
		var loc ingest.Location
		_ = json.Unmarshal(meta, &loc)
		v.Filename, v.Lineno = loc.Filename, loc.Lineno

		raw, _ := payload["event_id"].(string)
		eventID, err := uuid.Parse(raw)
		if err != nil {
			return nil
		}
		at := ingest.ReceivedAt(eventID)
		err = tx.QueryRow(ctx, `
			SELECT e.event_id::text, e.environment, e.release,
			       CASE WHEN e.customer_key = '' THEN '' ELSE coalesce(nullif(c.name, ''), e.customer_key) END,
			       e.tenant_key, e.user_label
			FROM events e
			LEFT JOIN customers c ON e.customer_key <> '' AND c.org_id = e.org_id AND c.key = e.customer_key
			WHERE e.id = $1 AND e.received_at BETWEEN $2 AND $3`, eventID, at.Add(-time.Second), at.Add(time.Second)).Scan(
			&v.EventID, &v.Environment, &v.Release, &v.Customer, &v.Tenant, &v.User)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return v, err
}
