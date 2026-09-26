package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Integration auth kinds (izci.api.v1.IntegrationAuth).
const (
	IntegrationToken int16 = 1
	IntegrationApp   int16 = 2
)

// Integration is an organization's connection to GitHub.
type Integration struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	Provider         string
	Auth             int16
	Name             string
	AccountLogin     string
	AccountAvatarURL string
	InstallationID   *int64
	// Secret is the sealed personal access token (token auth only).
	Secret        []byte
	Healthy       bool
	StatusMessage string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	VerifiedAt    *time.Time
}

const integrationColumns = `id, org_id, provider, auth, name, account_login, account_avatar_url, installation_id, secret,
	healthy, status_message, created_by, created_at, verified_at`

func scanIntegration(row interface{ Scan(...any) error }) (*Integration, error) {
	var i Integration
	if err := row.Scan(&i.ID, &i.OrgID, &i.Provider, &i.Auth, &i.Name, &i.AccountLogin, &i.AccountAvatarURL, &i.InstallationID,
		&i.Secret, &i.Healthy, &i.StatusMessage, &i.CreatedBy, &i.CreatedAt, &i.VerifiedAt); err != nil {
		return nil, notFound(err)
	}
	return &i, nil
}

// CreateIntegration inserts an integration with its (pre-generated) id.
func CreateIntegration(ctx context.Context, q Querier, i *Integration) (*Integration, error) {
	return scanIntegration(q.QueryRow(ctx, `
		INSERT INTO integrations (id, org_id, provider, auth, name, account_login, account_avatar_url, installation_id, secret,
		  created_by, verified_at)
		VALUES ($1, $2, 'github', $3, $4, $5, $6, $7, $8, $9, now())
		RETURNING `+integrationColumns,
		i.ID, i.OrgID, i.Auth, i.Name, i.AccountLogin, i.AccountAvatarURL, i.InstallationID, i.Secret, i.CreatedBy))
}

// ErrInstallationTaken means a GitHub App installation is linked to another
// organization.
var ErrInstallationTaken = errors.New("the installation is linked to another organization")

// LinkAppInstallation stores a GitHub App installation for an organization,
// or refreshes it when the organization linked it before. It must run in a
// system transaction to see installations of other organizations.
func LinkAppInstallation(ctx context.Context, q Querier, i *Integration) (*Integration, error) {
	out, err := scanIntegration(q.QueryRow(ctx, `
		INSERT INTO integrations (org_id, provider, auth, name, account_login, account_avatar_url, installation_id, created_by, verified_at)
		VALUES ($1, 'github', 2, $2, $3, $4, $5, $6, now())
		ON CONFLICT (provider, installation_id) WHERE installation_id IS NOT NULL DO UPDATE
		  SET name = excluded.name, account_login = excluded.account_login, account_avatar_url = excluded.account_avatar_url,
		      healthy = true, status_message = '', verified_at = now()
		  WHERE integrations.org_id = excluded.org_id
		RETURNING `+integrationColumns,
		i.OrgID, i.Name, i.AccountLogin, i.AccountAvatarURL, i.InstallationID, i.CreatedBy))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInstallationTaken
	}
	return out, err
}

// ListIntegrations returns an organization's integrations, newest first.
func ListIntegrations(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Integration, error) {
	rows, err := q.Query(ctx, `SELECT `+integrationColumns+` FROM integrations WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// GetIntegration returns an integration by id.
func GetIntegration(ctx context.Context, q Querier, id uuid.UUID) (*Integration, error) {
	return scanIntegration(q.QueryRow(ctx, `SELECT `+integrationColumns+` FROM integrations WHERE id = $1`, id))
}

// DeleteIntegration removes an integration; linked repositories keep their
// link without it.
func DeleteIntegration(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM integrations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetIntegrationHealth records whether the last use of an integration
// worked.
func SetIntegrationHealth(ctx context.Context, q Querier, id uuid.UUID, healthy bool, msg string) error {
	_, err := q.Exec(ctx, `UPDATE integrations SET healthy = $2, status_message = $3,
		verified_at = CASE WHEN $2 THEN now() ELSE verified_at END WHERE id = $1`, id, healthy, msg)
	return err
}

// Repository is a project's linked source repository.
type Repository struct {
	ProjectID     uuid.UUID
	OrgID         uuid.UUID
	IntegrationID *uuid.UUID
	Owner         string
	Name          string
	URL           string
	DefaultBranch string
	PathMappings  []byte
}

// GetRepository returns the repository linked to a project.
func GetRepository(ctx context.Context, q Querier, projectID uuid.UUID) (*Repository, error) {
	var r Repository
	err := q.QueryRow(ctx, `SELECT project_id, org_id, integration_id, owner, name, url, default_branch, path_mappings
		FROM project_repositories WHERE project_id = $1`, projectID).Scan(&r.ProjectID, &r.OrgID, &r.IntegrationID, &r.Owner,
		&r.Name, &r.URL, &r.DefaultBranch, &r.PathMappings)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}
