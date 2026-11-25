package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/secret"
)

// PendingSignup is a self sign-up waiting for its e-mail address to be
// confirmed.
type PendingSignup struct {
	ID               uuid.UUID
	Email            string
	Name             string
	PasswordHash     string
	OrganizationName string
	OrganizationSlug string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// CreatePendingSignup stores a sign-up for ttl and returns the token of
// its link.
func CreatePendingSignup(ctx context.Context, q Querier, in PendingSignup, ttl time.Duration) (string, error) {
	token := secret.NewSignupToken()
	_, err := q.Exec(ctx, `
		INSERT INTO pending_signups (email, name, password_hash, organization_name, organization_slug, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + $7::interval)`,
		in.Email, in.Name, in.PasswordHash, in.OrganizationName, in.OrganizationSlug, secret.Hash(token), ttl.String())
	return token, err
}

// TakePendingSignup returns the unexpired sign-up of a link token and
// removes it together with every other sign-up waiting for the same
// address (ErrNotFound for unknown, used or expired tokens).
func TakePendingSignup(ctx context.Context, q Querier, token string) (*PendingSignup, error) {
	var p PendingSignup
	err := q.QueryRow(ctx, `
		DELETE FROM pending_signups WHERE token_hash = $1 AND expires_at > now()
		RETURNING id, email, name, password_hash, organization_name, organization_slug, created_at, expires_at`,
		secret.Hash(token)).Scan(&p.ID, &p.Email, &p.Name, &p.PasswordHash, &p.OrganizationName, &p.OrganizationSlug,
		&p.CreatedAt, &p.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	if _, err := q.Exec(ctx, `DELETE FROM pending_signups WHERE email = $1`, p.Email); err != nil {
		return nil, err
	}
	return &p, nil
}

// CountOwnedOrganizations counts the organizations a user owns (system
// scope).
func CountOwnedOrganizations(ctx context.Context, q Querier, userID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE user_id = $1 AND role = $2`,
		userID, int16(apiv1.Role_ROLE_OWNER)).Scan(&n)
	return n, err
}
