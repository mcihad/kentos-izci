package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReleaseInput is release metadata registered by CI.
type ReleaseInput struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Version     string
	CommitSHA   string
	Repository  string
	Ref         string
	URL         string
	Environment string
	ReleasedAt  time.Time
	ReleasedBy  string
}

// RegisterRelease creates or updates a release (non-empty fields win) and
// records a deploy when an environment is given. It returns the release id
// and whether the release was new.
func RegisterRelease(ctx context.Context, q Querier, in ReleaseInput) (uuid.UUID, bool, error) {
	var id uuid.UUID
	var created bool
	err := q.QueryRow(ctx, `
		INSERT INTO releases AS r (org_id, project_id, version, commit_sha, repository, ref, url, first_seen, last_seen)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (project_id, version) DO UPDATE SET
		  commit_sha = coalesce(nullif(excluded.commit_sha, ''), r.commit_sha),
		  repository = coalesce(nullif(excluded.repository, ''), r.repository),
		  ref        = coalesce(nullif(excluded.ref, ''), r.ref),
		  url        = coalesce(nullif(excluded.url, ''), r.url)
		RETURNING r.id, old.id IS NULL`,
		in.OrgID, in.ProjectID, in.Version, in.CommitSHA, in.Repository, in.Ref, in.URL, in.ReleasedAt).Scan(&id, &created)
	if err != nil {
		return uuid.Nil, false, err
	}
	if in.Environment != "" {
		if _, err := q.Exec(ctx, `
			INSERT INTO deploys (org_id, release_id, environment, deployed_at, deployed_by)
			VALUES ($1, $2, $3, $4, $5)`, in.OrgID, id, in.Environment, in.ReleasedAt, in.ReleasedBy); err != nil {
			return uuid.Nil, false, err
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO environments AS e (project_id, name, org_id) VALUES ($1, $2, $3)
			ON CONFLICT (project_id, name) DO NOTHING`, in.ProjectID, in.Environment, in.OrgID); err != nil {
			return uuid.Nil, false, err
		}
	}
	return id, created, nil
}

// ReleaseInfo is a release with its deploys, as the Sentry web API reports
// it to sentry-cli.
type ReleaseInfo struct {
	ID          uuid.UUID
	ProjectSlug string
	ProjectName string
	Version     string
	CommitSHA   string
	Repository  string
	URL         string
	NewIssues   int
	CreatedAt   time.Time
	LastSeen    time.Time
	Deploys     int
	LastDeploy  *time.Time
	LastEnv     string
}

// GetReleaseInfo returns a project's release by version.
func GetReleaseInfo(ctx context.Context, q Querier, projectID uuid.UUID, version string) (*ReleaseInfo, error) {
	var r ReleaseInfo
	err := q.QueryRow(ctx, `
		SELECT r.id, p.slug, p.name, r.version, r.commit_sha, r.repository, r.url, r.new_issues, r.created_at, r.last_seen,
		  (SELECT count(*) FROM deploys d WHERE d.release_id = r.id),
		  (SELECT max(d.deployed_at) FROM deploys d WHERE d.release_id = r.id),
		  coalesce((SELECT d.environment FROM deploys d WHERE d.release_id = r.id ORDER BY d.deployed_at DESC LIMIT 1), '')
		FROM releases r JOIN projects p ON p.id = r.project_id
		WHERE r.project_id = $1 AND r.version = $2`, projectID, version).Scan(&r.ID, &r.ProjectSlug, &r.ProjectName, &r.Version,
		&r.CommitSHA, &r.Repository, &r.URL, &r.NewIssues, &r.CreatedAt, &r.LastSeen, &r.Deploys, &r.LastDeploy, &r.LastEnv)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// ProjectsWithRelease returns the projects of an organization that have a
// release version.
func ProjectsWithRelease(ctx context.Context, q Querier, orgID uuid.UUID, version string) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT r.project_id FROM releases r JOIN projects p ON p.id = r.project_id
		WHERE r.org_id = $1 AND r.version = $2 AND p.deleted_at IS NULL`, orgID, version)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
