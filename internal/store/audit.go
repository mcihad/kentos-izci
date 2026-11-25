package store

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

// InsertUserAudit records an entry about a user's account at instance level
// (for server administrators and the SIEM) and in the audit log of every
// organization the user belongs to, so that their administrators see it.
// Run in system scope.
func InsertUserAudit(ctx context.Context, q Querier, userID uuid.UUID, e AuditEntry) error {
	e.OrgID = nil
	if err := InsertAudit(ctx, q, e); err != nil {
		return err
	}
	var ip *netip.Addr
	if e.IP.IsValid() {
		ip = &e.IP
	}
	meta := e.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	_, err := q.Exec(ctx, `
		INSERT INTO audit_log (org_id, actor_id, action, target_type, target_id, target_name, metadata, ip_address)
		SELECT m.org_id, $2, $3, $4, $5, $6, $7, $8 FROM org_members m WHERE m.user_id = $1`,
		userID, e.ActorID, e.Action, e.TargetType, e.TargetID, e.TargetName, meta, ip)
	return err
}

// AuditRecord is an audit entry with the names a reader needs.
type AuditRecord struct {
	ID          uuid.UUID
	OrgID       *uuid.UUID
	OrgSlug     string
	ActorID     *uuid.UUID
	ActorName   string
	ActorEmail  string
	ActorAvatar string
	Action      string
	TargetType  string
	TargetID    string
	TargetName  string
	Metadata    map[string]string
	IP          string
	CreatedAt   time.Time
}

const auditSelect = `
	SELECT a.id, a.org_id, coalesce(o.slug, ''), a.actor_id, coalesce(u.name, ''), coalesce(u.email, ''), coalesce(u.avatar_url, ''),
	       a.action, a.target_type, a.target_id, a.target_name, a.metadata, coalesce(host(a.ip_address), ''), a.created_at
	FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id LEFT JOIN organizations o ON o.id = a.org_id`

func scanAudit(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}) ([]AuditRecord, error) {
	defer rows.Close()
	var out []AuditRecord
	for rows.Next() {
		var r AuditRecord
		var meta []byte
		if err := rows.Scan(&r.ID, &r.OrgID, &r.OrgSlug, &r.ActorID, &r.ActorName, &r.ActorEmail, &r.ActorAvatar, &r.Action,
			&r.TargetType, &r.TargetID, &r.TargetName, &meta, &r.IP, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &r.Metadata)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListInstanceAudit pages through the instance-level entries (logins,
// account security, administration, CLI), newest first, before the cursor
// when one is given. Run in system scope.
func ListInstanceAudit(ctx context.Context, q Querier, before *time.Time, beforeID uuid.UUID, limit int) ([]AuditRecord, error) {
	rows, err := q.Query(ctx, auditSelect+`
		WHERE a.org_id IS NULL AND ($1::timestamptz IS NULL OR (a.created_at, a.id) < ($1, $2))
		ORDER BY a.created_at DESC, a.id DESC LIMIT $3`, before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return scanAudit(rows)
}

// AuditAfter returns entries of every scope after the cursor and created
// before until, oldest first, for forwarding. Run in system scope.
func AuditAfter(ctx context.Context, q Querier, at time.Time, id uuid.UUID, until time.Time, limit int) ([]AuditRecord, error) {
	rows, err := q.Query(ctx, auditSelect+`
		WHERE (a.created_at, a.id) > ($1, $2) AND a.created_at < $3
		ORDER BY a.created_at, a.id LIMIT $4`, at, id, until, limit)
	if err != nil {
		return nil, err
	}
	return scanAudit(rows)
}
