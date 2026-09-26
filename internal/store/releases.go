package store

import (
	"context"
	"time"

	"github.com/google/uuid"
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
