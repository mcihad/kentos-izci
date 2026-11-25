// Package auditforward sends the audit log to a SIEM: new entries go to a
// webhook as signed JSON and to syslog as RFC 5424 messages, in order and
// at least once. Entries are read a little behind real time, so that a
// transaction committing late does not get skipped.
package auditforward

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/store"
)

// cursorKey is the instance setting holding the last forwarded entry.
const cursorKey = "audit_forward_cursor"

// batch is how many entries one webhook call carries.
const batch = 200

// Forwarder sends new audit entries to the configured destinations.
type Forwarder struct {
	db       *db.DB
	cfg      config.AuditConfig
	log      *slog.Logger
	client   *http.Client
	hostname string
	// Grace is how far behind real time entries are read.
	Grace time.Duration
}

// New creates a forwarder; Run does nothing when no destination is set.
func New(database *db.DB, cfg config.AuditConfig, log *slog.Logger) *Forwarder {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "-"
	}
	return &Forwarder{db: database, cfg: cfg, log: log, client: &http.Client{Timeout: 15 * time.Second}, hostname: host,
		Grace: 30 * time.Second}
}

// Entry is an audit entry as forwarded.
type Entry struct {
	ID           uuid.UUID         `json:"id"`
	Time         time.Time         `json:"time"`
	Organization *Ref              `json:"organization,omitempty"`
	Actor        *Actor            `json:"actor,omitempty"`
	Action       string            `json:"action"`
	Target       Target            `json:"target"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	IP           string            `json:"ip,omitempty"`
}

// Ref names an organization.
type Ref struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug,omitempty"`
}

// Actor is who acted.
type Actor struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email,omitempty"`
	Name  string    `json:"name,omitempty"`
}

// Target is what was acted on.
type Target struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func toEntry(r store.AuditRecord) Entry {
	e := Entry{ID: r.ID, Time: r.CreatedAt.UTC(), Action: r.Action, Target: Target{r.TargetType, r.TargetID, r.TargetName},
		Metadata: r.Metadata, IP: r.IP}
	if r.OrgID != nil {
		e.Organization = &Ref{ID: *r.OrgID, Slug: r.OrgSlug}
	}
	if r.ActorID != nil {
		e.Actor = &Actor{ID: *r.ActorID, Email: r.ActorEmail, Name: r.ActorName}
	}
	return e
}

type cursor struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

// Run forwards the entries stored since the last run. One server instance
// works at a time; a failed delivery is retried on the next run.
func (f *Forwarder) Run(ctx context.Context) error {
	if !f.cfg.Enabled() {
		return nil
	}
	conn, err := f.db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('izci:audit-forward'))`).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('izci:audit-forward'))`)
	}()

	var c cursor
	var raw []byte
	err = conn.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key = $1`, cursorKey).Scan(&raw)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Forwarding starts with the entries stored from now on.
		c = cursor{At: time.Now().Add(-max(f.Grace, 0))}
		return f.save(ctx, conn, c)
	case err != nil:
		return err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("audit forward cursor: %w", err)
	}
	for ctx.Err() == nil {
		var records []store.AuditRecord
		err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
			var err error
			records, err = store.AuditAfter(ctx, tx, c.At, c.ID, time.Now().Add(-f.Grace), batch)
			return err
		})
		if err != nil || len(records) == 0 {
			return err
		}
		entries := make([]Entry, len(records))
		for i, r := range records {
			entries[i] = toEntry(r)
		}
		if err := f.deliver(ctx, entries); err != nil {
			return err
		}
		last := records[len(records)-1]
		c = cursor{At: last.CreatedAt, ID: last.ID}
		if err := f.save(ctx, conn, c); err != nil {
			return err
		}
		if len(records) < batch {
			return nil
		}
	}
	return ctx.Err()
}

func (f *Forwarder) save(ctx context.Context, conn *pgxpool.Conn, c cursor) error {
	js, _ := json.Marshal(c)
	_, err := conn.Exec(ctx, `
		INSERT INTO instance_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now()`, cursorKey, js)
	return err
}

func (f *Forwarder) deliver(ctx context.Context, entries []Entry) error {
	if f.cfg.WebhookURL != "" {
		if err := f.webhook(ctx, entries); err != nil {
			return fmt.Errorf("audit webhook: %w", err)
		}
	}
	if f.cfg.SyslogAddr != "" {
		if err := f.syslog(ctx, entries); err != nil {
			return fmt.Errorf("audit syslog: %w", err)
		}
	}
	return nil
}

// Sign returns the X-Izci-Signature value of a webhook body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (f *Forwarder) webhook(ctx context.Context, entries []Entry) error {
	body, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "izci-audit")
	if s := f.cfg.WebhookSecret.Value(); s != "" {
		req.Header.Set("X-Izci-Signature", Sign(s, body))
	}
	res, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return nil
}

// Facility 13 (log audit); severity warning for failed sign-ins, notice
// otherwise.
func priority(action string) int {
	if strings.HasSuffix(action, "_failed") {
		return 13*8 + 4
	}
	return 13*8 + 5
}

// SyslogMessage formats an entry as RFC 5424 with the JSON entry as MSG.
func SyslogMessage(host string, e Entry) string {
	js, _ := json.Marshal(e)
	msgID := e.Action
	if len(msgID) > 32 {
		msgID = msgID[:32]
	}
	return fmt.Sprintf("<%d>1 %s %s izci - %s - %s", priority(e.Action), e.Time.Format(time.RFC3339Nano), host, msgID, js)
}

func (f *Forwarder) syslog(ctx context.Context, entries []Entry) error {
	u, err := url.Parse(f.cfg.SyslogAddr)
	if err != nil {
		return err
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	switch u.Scheme {
	case "tls":
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", u.Host)
	default:
		conn, err = d.DialContext(ctx, u.Scheme, u.Host)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	for _, e := range entries {
		msg := SyslogMessage(f.hostname, e)
		if u.Scheme != "udp" {
			// Octet counting (RFC 6587) frames messages on streams.
			msg = fmt.Sprintf("%d %s", len(msg), msg)
		}
		if _, err := io.WriteString(conn, msg); err != nil {
			return err
		}
	}
	return nil
}
