package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/secret"
)

// APIKey is a project ingest key (without its secret).
type APIKey struct {
	ID                 uuid.UUID
	OrgID              uuid.UUID
	ProjectID          uuid.UUID
	Name               string
	Prefix             string
	Scopes             []apiv1.ApiKeyScope
	AllowedOrigins     []string
	RateLimitPerMinute int32
	Enabled            bool
	CreatedBy          *uuid.UUID
	CreatedAt          time.Time
	LastUsedAt         *time.Time
	RevokedAt          *time.Time
}

// HasScope reports whether the key grants scope.
func (k *APIKey) HasScope(scope apiv1.ApiKeyScope) bool { return slices.Contains(k.Scopes, scope) }

// Usable reports whether the key may authenticate requests.
func (k *APIKey) Usable() bool { return k.Enabled && k.RevokedAt == nil }

const apiKeyColumns = `id, org_id, project_id, name, prefix, scopes, allowed_origins,
	rate_limit_per_minute, enabled, created_by, created_at, last_used_at, revoked_at`

func scanAPIKey(row interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var scopes []int16
	if err := row.Scan(&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.Prefix, &scopes, &k.AllowedOrigins,
		&k.RateLimitPerMinute, &k.Enabled, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return nil, notFound(err)
	}
	k.Scopes = make([]apiv1.ApiKeyScope, len(scopes))
	for i, s := range scopes {
		k.Scopes[i] = apiv1.ApiKeyScope(s)
	}
	return &k, nil
}

// NewAPIKeyInput describes a key to create.
type NewAPIKeyInput struct {
	OrgID              uuid.UUID
	ProjectID          uuid.UUID
	Name               string
	Scopes             []apiv1.ApiKeyScope
	AllowedOrigins     []string
	RateLimitPerMinute int32
	CreatedBy          *uuid.UUID
}

// CreateAPIKey generates a key, stores its hash and returns the key with its
// secret, which is never retrievable again.
func CreateAPIKey(ctx context.Context, q Querier, in NewAPIKeyInput) (*APIKey, string, error) {
	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}
	}
	dbScopes := make([]int16, len(scopes))
	for i, s := range scopes {
		dbScopes[i] = int16(s)
	}
	origins := in.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	token := secret.NewAPIKey()
	row := q.QueryRow(ctx, `
		INSERT INTO api_keys (org_id, project_id, name, prefix, key_hash, scopes, allowed_origins, rate_limit_per_minute, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+apiKeyColumns,
		in.OrgID, in.ProjectID, in.Name, secret.DisplayPrefix(token), secret.Hash(token), dbScopes, origins,
		in.RateLimitPerMinute, in.CreatedBy)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, "", fmt.Errorf("create api key: %w", err)
	}
	return k, token, nil
}

// ListAPIKeys lists the keys of a project, newest first.
func ListAPIKeys(ctx context.Context, q Querier, projectID uuid.UUID, includeRevoked bool) ([]*APIKey, error) {
	rows, err := q.Query(ctx, `
		SELECT `+apiKeyColumns+` FROM api_keys
		WHERE project_id = $1 AND ($2 OR revoked_at IS NULL)
		ORDER BY created_at DESC`, projectID, includeRevoked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey revokes a key of a project.
func RevokeAPIKey(ctx context.Context, q Querier, projectID, keyID uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now(), enabled = false
		WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL`, keyID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolvedKey is an API key together with the project and organization data
// the ingest path needs. Resolution happens before any tenant is known, so it
// must run in system scope.
type ResolvedKey struct {
	Key             *APIKey
	ProjectSlug     string
	ProjectName     string
	ProjectPublicID int64
	ProjectSettings *apiv1.ProjectSettings
	OrgSlug         string
	OrgLimits       *apiv1.OrganizationLimits
}

// ResolveAPIKey finds a key by its secret. Revoked and disabled keys are
// returned too (callers check Usable) so that they can be cached negatively.
func ResolveAPIKey(ctx context.Context, q Querier, token string) (*ResolvedKey, error) {
	return resolveKey(ctx, q, `k.key_hash = $1`, secret.Hash(token))
}

// ResolveAPIKeyForProject finds a key by secret restricted to a project's
// public (numeric) id, as used by Sentry DSNs.
func ResolveAPIKeyForProject(ctx context.Context, q Querier, token string, publicID int64) (*ResolvedKey, error) {
	return resolveKey(ctx, q, `k.key_hash = $1 AND p.public_id = $2`, secret.Hash(token), publicID)
}

func resolveKey(ctx context.Context, q Querier, where string, args ...any) (*ResolvedKey, error) {
	row := q.QueryRow(ctx, `
		SELECT k.id, k.org_id, k.project_id, k.name, k.prefix, k.scopes, k.allowed_origins,
		       k.rate_limit_per_minute, k.enabled, k.created_by, k.created_at, k.last_used_at, k.revoked_at,
		       p.slug, p.name, p.public_id, p.settings, o.slug, o.limits
		FROM api_keys k
		JOIN projects p ON p.id = k.project_id AND p.deleted_at IS NULL
		JOIN organizations o ON o.id = k.org_id
		WHERE `+where, args...)
	var r ResolvedKey
	var k APIKey
	var scopes []int16
	var settings, limits []byte
	if err := row.Scan(&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.Prefix, &scopes, &k.AllowedOrigins,
		&k.RateLimitPerMinute, &k.Enabled, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt,
		&r.ProjectSlug, &r.ProjectName, &r.ProjectPublicID, &settings, &r.OrgSlug, &limits); err != nil {
		return nil, notFound(err)
	}
	for _, s := range scopes {
		k.Scopes = append(k.Scopes, apiv1.ApiKeyScope(s))
	}
	r.Key = &k
	r.ProjectSettings = DefaultProjectSettings()
	if err := unmarshalJSON(settings, r.ProjectSettings); err != nil {
		return nil, fmt.Errorf("decode project settings: %w", err)
	}
	r.OrgLimits = &apiv1.OrganizationLimits{}
	if err := unmarshalJSON(limits, r.OrgLimits); err != nil {
		return nil, fmt.Errorf("decode organization limits: %w", err)
	}
	return &r, nil
}

// TouchAPIKeys records usage of keys (batched by the caller, at most once a
// minute per key).
func TouchAPIKeys(ctx context.Context, q Querier, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = ANY($1)`, ids, at)
	return err
}
