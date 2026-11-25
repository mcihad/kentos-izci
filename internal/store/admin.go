package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// extraScanner appends destinations after those of a scan helper, so that
// queries can return extra columns after a standard column list.
type extraScanner struct {
	row   interface{ Scan(...any) error }
	extra []any
}

func (e extraScanner) Scan(dest ...any) error { return e.row.Scan(append(dest, e.extra...)...) }

// OrganizationStats is an organization with instance-level statistics.
type OrganizationStats struct {
	Org         *Organization
	Projects    int32
	Members     int32
	MonthEvents int64
	LastEventAt *time.Time
}

// ListOrganizationStats pages through all organizations ordered by name.
// It reads across tenants and must run in system scope. pattern is an
// ILIKE pattern ("" for all).
func ListOrganizationStats(ctx context.Context, q Querier, pattern string, limit, offset int) ([]*OrganizationStats, error) {
	rows, err := q.Query(ctx, `
		SELECT `+prefixed("o.", orgColumns)+`,
		  (SELECT count(*) FROM projects p WHERE p.org_id = o.id AND p.deleted_at IS NULL)::int,
		  (SELECT count(*) FROM org_members m WHERE m.org_id = o.id)::int,
		  (SELECT coalesce(sum(u.accepted), 0) FROM usage_daily u WHERE u.org_id = o.id AND u.day >= date_trunc('month', now())::date)::bigint,
		  (SELECT max(p.last_event_at) FROM projects p WHERE p.org_id = o.id AND p.deleted_at IS NULL)
		FROM organizations o
		WHERE o.deleted_at IS NULL AND ($1 = '' OR (o.slug || ' ' || o.name) ILIKE $1)
		ORDER BY lower(o.name), o.id LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OrganizationStats
	for rows.Next() {
		var s OrganizationStats
		org, err := scanOrganization(extraScanner{rows, []any{&s.Projects, &s.Members, &s.MonthEvents, &s.LastEventAt}})
		if err != nil {
			return nil, err
		}
		s.Org = org
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UserStats is a user with instance-level details.
type UserStats struct {
	User          *User
	Providers     []string
	Organizations int32
}

// ListUserStats pages through all users ordered by email. pattern is an
// ILIKE pattern ("" for all).
func ListUserStats(ctx context.Context, q Querier, pattern string, limit, offset int) ([]*UserStats, error) {
	rows, err := q.Query(ctx, `
		SELECT `+prefixed("u.", userColumns)+`,
		  coalesce((SELECT array_agg(i.provider ORDER BY i.provider) FROM oidc_identities i WHERE i.user_id = u.id), '{}'),
		  (SELECT count(*) FROM org_members m WHERE m.user_id = u.id)::int
		FROM users u
		WHERE $1 = '' OR (u.email || ' ' || u.name) ILIKE $1
		ORDER BY lower(u.email), u.id LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*UserStats
	for rows.Next() {
		var s UserStats
		u, err := scanUser(extraScanner{rows, []any{&s.Providers, &s.Organizations}})
		if err != nil {
			return nil, err
		}
		s.User = u
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UserStatsByID returns the details of one user.
func UserStatsByID(ctx context.Context, q Querier, id uuid.UUID) (*UserStats, error) {
	var s UserStats
	u, err := scanUser(extraScanner{q.QueryRow(ctx, `
		SELECT `+prefixed("u.", userColumns)+`,
		  coalesce((SELECT array_agg(i.provider ORDER BY i.provider) FROM oidc_identities i WHERE i.user_id = u.id), '{}'),
		  (SELECT count(*) FROM org_members m WHERE m.user_id = u.id)::int
		FROM users u WHERE u.id = $1`, id), []any{&s.Providers, &s.Organizations}})
	if err != nil {
		return nil, err
	}
	s.User = u
	return &s, nil
}
