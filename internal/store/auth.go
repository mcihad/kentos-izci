package store

import (
	"context"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/secret"
)

// ── Users ────────────────────────────────────────────────────────────────

// UpdateUserPassword stores a new password hash.
func UpdateUserPassword(ctx context.Context, q Querier, id uuid.UUID, hash string) error {
	_, err := q.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash)
	return err
}

// UpdateUserProfile changes the display name.
func UpdateUserProfile(ctx context.Context, q Querier, id uuid.UUID, name string) (*User, error) {
	return scanUser(q.QueryRow(ctx, `UPDATE users SET name = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns, id, name))
}

// UpdateUserFlags changes superadmin and disabled flags (nil = unchanged).
func UpdateUserFlags(ctx context.Context, q Querier, id uuid.UUID, superadmin, disabled *bool) (*User, error) {
	return scanUser(q.QueryRow(ctx, `
		UPDATE users SET superadmin = coalesce($2, superadmin), disabled = coalesce($3, disabled), updated_at = now()
		WHERE id = $1 RETURNING `+userColumns, id, superadmin, disabled))
}

// MarkUserLogin records a successful sign-in.
func MarkUserLogin(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE users SET last_login_at = now(), last_seen_at = now() WHERE id = $1`, id)
	return err
}

// ListUsers pages through users ordered by email.
func ListUsers(ctx context.Context, q Querier, query string, limit int, after string) ([]*User, error) {
	rows, err := q.Query(ctx, `
		SELECT `+userColumns+` FROM users
		WHERE ($1 = '' OR email ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR lower(email) > $2)
		ORDER BY lower(email) LIMIT $3`, query, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ── Sessions ─────────────────────────────────────────────────────────────

// Session is a browser session.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	IPAddress  *netip.Addr
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

const sessionColumns = `id, user_id, ip_address, user_agent, created_at, last_seen_at, expires_at`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var s Session
	if err := row.Scan(&s.ID, &s.UserID, &s.IPAddress, &s.UserAgent, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt); err != nil {
		return nil, notFound(err)
	}
	return &s, nil
}

// CreateSession stores a session and returns it with the cookie token.
func CreateSession(ctx context.Context, q Querier, userID uuid.UUID, ip netip.Addr, userAgent string, ttl time.Duration) (*Session, string, error) {
	token := secret.NewSessionToken()
	var ipArg *netip.Addr
	if ip.IsValid() {
		ipArg = &ip
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	s, err := scanSession(q.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval)
		RETURNING `+sessionColumns, userID, secret.Hash(token), ipArg, userAgent, ttl.String()))
	return s, token, err
}

// SessionWithUser is a valid session and its user.
type SessionWithUser struct {
	Session *Session
	User    *User
}

// LookupSession finds an unexpired session by cookie token.
func LookupSession(ctx context.Context, q Querier, token string) (*SessionWithUser, error) {
	row := q.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.ip_address, s.user_agent, s.created_at, s.last_seen_at, s.expires_at,
		       u.id, u.email, u.name, u.avatar_url, u.password_hash, u.superadmin, u.disabled,
		       u.telegram_user_id, u.telegram_username, u.telegram_linked_at, u.last_login_at, u.last_seen_at,
		       u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, secret.Hash(token))
	var s Session
	var u User
	err := row.Scan(&s.ID, &s.UserID, &s.IPAddress, &s.UserAgent, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt,
		&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.PasswordHash, &u.Superadmin, &u.Disabled,
		&u.TelegramUserID, &u.TelegramUsername, &u.TelegramLinkedAt, &u.LastLoginAt, &u.LastSeenAt,
		&u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &SessionWithUser{Session: &s, User: &u}, nil
}

// TouchSession extends a session (sliding expiration).
func TouchSession(ctx context.Context, q Querier, id uuid.UUID, ttl time.Duration) error {
	_, err := q.Exec(ctx, `UPDATE sessions SET last_seen_at = now(), expires_at = now() + $2::interval WHERE id = $1`, id, ttl.String())
	return err
}

// DeleteSession ends one session of a user.
func DeleteSession(ctx context.Context, q Querier, userID, id uuid.UUID) (int64, error) {
	tag, err := q.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, userID)
	return tag.RowsAffected(), err
}

// DeleteOtherSessions ends every session of a user except keep.
func DeleteOtherSessions(ctx context.Context, q Querier, userID, keep uuid.UUID) (int64, error) {
	tag, err := q.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, userID, keep)
	return tag.RowsAffected(), err
}

// ListSessions lists a user's active sessions, most recent first.
func ListSessions(ctx context.Context, q Querier, userID uuid.UUID) ([]*Session, error) {
	rows, err := q.Query(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE user_id = $1 AND expires_at > now() ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── OpenID Connect identities ────────────────────────────────────────────

// UserByIdentity returns the user linked to (provider, subject).
func UserByIdentity(ctx context.Context, q Querier, provider, subject string) (*User, error) {
	return scanUser(q.QueryRow(ctx, `
		SELECT `+prefixed("u.", userColumns)+` FROM oidc_identities i JOIN users u ON u.id = i.user_id
		WHERE i.provider = $1 AND i.subject = $2`, provider, subject))
}

// LinkIdentity links (provider, subject) to a user and records the login.
func LinkIdentity(ctx context.Context, q Querier, provider, subject string, userID uuid.UUID, email string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO oidc_identities (provider, subject, user_id, email, last_login_at) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (provider, subject) DO UPDATE SET email = excluded.email, last_login_at = now()`,
		provider, subject, userID, email)
	return err
}

// UserProviders lists the identity providers linked to a user.
func UserProviders(ctx context.Context, q Querier, userID uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT provider FROM oidc_identities WHERE user_id = $1 ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// ── Personal access tokens ───────────────────────────────────────────────

// PersonalToken is a personal access token (without its secret).
type PersonalToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	OrgID      *uuid.UUID
	Name       string
	Prefix     string
	Scopes     []apiv1.TokenScope
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

const tokenColumns = `id, user_id, org_id, name, prefix, scopes, created_at, last_used_at, expires_at, revoked_at`

func scanToken(row interface{ Scan(...any) error }) (*PersonalToken, error) {
	var t PersonalToken
	var scopes []int16
	if err := row.Scan(&t.ID, &t.UserID, &t.OrgID, &t.Name, &t.Prefix, &scopes, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt); err != nil {
		return nil, notFound(err)
	}
	for _, s := range scopes {
		t.Scopes = append(t.Scopes, apiv1.TokenScope(s))
	}
	return &t, nil
}

// CreatePersonalToken stores a token and returns it with its secret.
func CreatePersonalToken(ctx context.Context, q Querier, userID uuid.UUID, orgID *uuid.UUID, name string, scopes []apiv1.TokenScope, expiresAt *time.Time) (*PersonalToken, string, error) {
	token := secret.NewAccessToken()
	db := make([]int16, len(scopes))
	for i, s := range scopes {
		db[i] = int16(s)
	}
	t, err := scanToken(q.QueryRow(ctx, `
		INSERT INTO personal_tokens (user_id, org_id, name, prefix, token_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+tokenColumns,
		userID, orgID, name, secret.DisplayPrefix(token), secret.Hash(token), db, expiresAt))
	return t, token, err
}

// TokenWithUser is a usable token with its user.
type TokenWithUser struct {
	Token *PersonalToken
	User  *User
}

// LookupPersonalToken finds an unrevoked, unexpired token by secret.
func LookupPersonalToken(ctx context.Context, q Querier, token string) (*TokenWithUser, error) {
	row := q.QueryRow(ctx, `
		SELECT t.id, t.user_id, t.org_id, t.name, t.prefix, t.scopes, t.created_at, t.last_used_at, t.expires_at, t.revoked_at,
		       `+prefixed("u.", userColumns)+`
		FROM personal_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())`, secret.Hash(token))
	var t PersonalToken
	var u User
	var scopes []int16
	err := row.Scan(&t.ID, &t.UserID, &t.OrgID, &t.Name, &t.Prefix, &scopes, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt,
		&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.PasswordHash, &u.Superadmin, &u.Disabled,
		&u.TelegramUserID, &u.TelegramUsername, &u.TelegramLinkedAt, &u.LastLoginAt, &u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	for _, s := range scopes {
		t.Scopes = append(t.Scopes, apiv1.TokenScope(s))
	}
	return &TokenWithUser{Token: &t, User: &u}, nil
}

// TouchPersonalToken records token usage (at most once a minute).
func TouchPersonalToken(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE personal_tokens SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, id)
	return err
}

// ListPersonalTokens lists a user's active tokens, newest first.
func ListPersonalTokens(ctx context.Context, q Querier, userID uuid.UUID) ([]*PersonalToken, error) {
	rows, err := q.Query(ctx, `SELECT `+tokenColumns+` FROM personal_tokens WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PersonalToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokePersonalToken revokes a token of a user.
func RevokePersonalToken(ctx context.Context, q Querier, userID, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `UPDATE personal_tokens SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Invitations ──────────────────────────────────────────────────────────

// Invitation invites an email into an organization.
type Invitation struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	Email      string
	Role       apiv1.Role
	InvitedBy  *uuid.UUID
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
}

const invitationColumns = `id, org_id, email, role, invited_by, created_at, expires_at, accepted_at, revoked_at`

func scanInvitation(row interface{ Scan(...any) error }) (*Invitation, error) {
	var i Invitation
	var role int16
	if err := row.Scan(&i.ID, &i.OrgID, &i.Email, &role, &i.InvitedBy, &i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.RevokedAt); err != nil {
		return nil, notFound(err)
	}
	i.Role = apiv1.Role(role)
	return &i, nil
}

// CreateInvitation stores an invitation and returns it with its token.
func CreateInvitation(ctx context.Context, q Querier, orgID uuid.UUID, email string, role apiv1.Role, invitedBy uuid.UUID, ttl time.Duration) (*Invitation, string, error) {
	token := secret.NewInvitationToken()
	inv, err := scanInvitation(q.QueryRow(ctx, `
		INSERT INTO invitations (org_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval) RETURNING `+invitationColumns,
		orgID, email, int16(role), secret.Hash(token), invitedBy, ttl.String()))
	return inv, token, err
}

// InvitationByToken returns a pending, unexpired invitation (system scope:
// the organization is not known before the token is resolved).
func InvitationByToken(ctx context.Context, q Querier, token string) (*Invitation, error) {
	return scanInvitation(q.QueryRow(ctx, `
		SELECT `+invitationColumns+` FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`, secret.Hash(token)))
}

// PendingInvitations lists an organization's open invitations.
func PendingInvitations(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Invitation, error) {
	rows, err := q.Query(ctx, `
		SELECT `+invitationColumns+` FROM invitations
		WHERE org_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invitation
	for rows.Next() {
		i, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// MarkInvitationAccepted closes an invitation.
func MarkInvitationAccepted(ctx context.Context, q Querier, id, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE invitations SET accepted_at = now(), accepted_by = $2 WHERE id = $1`, id, userID)
	return err
}

// RevokeInvitation cancels a pending invitation.
func RevokeInvitation(ctx context.Context, q Querier, orgID, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `UPDATE invitations SET revoked_at = now() WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Members ──────────────────────────────────────────────────────────────

// Member is a user's membership.
type Member struct {
	User     *User
	Role     apiv1.Role
	JoinedAt time.Time
}

// ListMembers lists an organization's members ordered by name.
func ListMembers(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Member, error) {
	rows, err := q.Query(ctx, `
		SELECT `+prefixed("u.", userColumns)+`, m.role, m.created_at
		FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 ORDER BY lower(coalesce(nullif(u.name, ''), u.email))`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Member
	for rows.Next() {
		var u User
		var m Member
		var role int16
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.PasswordHash, &u.Superadmin, &u.Disabled,
			&u.TelegramUserID, &u.TelegramUsername, &u.TelegramLinkedAt, &u.LastLoginAt, &u.LastSeenAt,
			&u.CreatedAt, &u.UpdatedAt, &role, &m.JoinedAt); err != nil {
			return nil, err
		}
		m.User, m.Role = &u, apiv1.Role(role)
		out = append(out, &m)
	}
	return out, rows.Err()
}

// RemoveMember deletes a membership.
func RemoveMember(ctx context.Context, q Querier, orgID, userID uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountOwners counts owners of an organization.
func CountOwners(ctx context.Context, q Querier, orgID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND role = $2`, orgID, int16(apiv1.Role_ROLE_OWNER)).Scan(&n)
	return n, err
}

// ── Audit log ────────────────────────────────────────────────────────────

// AuditEntry is one audit record.
type AuditEntry struct {
	OrgID      *uuid.UUID
	ActorID    *uuid.UUID
	Action     string
	TargetType string
	TargetID   string
	TargetName string
	Metadata   map[string]string
	IP         netip.Addr
}

// InsertAudit records an audit entry.
func InsertAudit(ctx context.Context, q Querier, e AuditEntry) error {
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, e.OrgID, e.ActorID, e.Action, e.TargetType, e.TargetID, e.TargetName, meta, ip)
	return err
}

// prefixed qualifies every column of a comma-separated list with p.
func prefixed(p, columns string) string {
	var out []byte
	start := 0
	for i := 0; i <= len(columns); i++ {
		if i == len(columns) || columns[i] == ',' {
			col := trimSpace(columns[start:i])
			if len(out) > 0 {
				out = append(out, ", "...)
			}
			out = append(out, p+col...)
			start = i + 1
		}
	}
	return string(out)
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
