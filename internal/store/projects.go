package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Project is one monitored application.
type Project struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	PublicID    int64
	Slug        string
	Name        string
	Platform    string
	Settings    *apiv1.ProjectSettings
	IssueSeq    int32
	LastEventAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const projectColumns = `id, org_id, public_id, slug, name, platform, settings, issue_seq, last_event_at, created_at, updated_at`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var settings []byte
	if err := row.Scan(&p.ID, &p.OrgID, &p.PublicID, &p.Slug, &p.Name, &p.Platform, &settings,
		&p.IssueSeq, &p.LastEventAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	p.Settings = DefaultProjectSettings()
	if err := unmarshalJSON(settings, p.Settings); err != nil {
		return nil, fmt.Errorf("decode project settings: %w", err)
	}
	return &p, nil
}

// DefaultProjectSettings are the settings of a new project.
func DefaultProjectSettings() *apiv1.ProjectSettings {
	return &apiv1.ProjectSettings{
		Privacy: &apiv1.PrivacySettings{
			IpStorage:          apiv1.IpStorage_IP_STORAGE_FULL,
			StoreRequestBodies: true,
		},
		InApp: &apiv1.InAppRules{},
		Ai: &apiv1.AiSettings{
			AutoAnalyze: apiv1.AutoAnalyze_AUTO_ANALYZE_OFF,
			MinLevel:    ingestv1.Level_LEVEL_ERROR,
			AutoAction:  apiv1.AutoAction_AUTO_ACTION_NONE,
		},
	}
}

// CreateProject inserts a project. A taken slug yields a unique violation
// on "projects_org_slug_key".
func CreateProject(ctx context.Context, q Querier, orgID uuid.UUID, slug, name, platform string) (*Project, error) {
	settings, err := marshalJSON(DefaultProjectSettings())
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		INSERT INTO projects (org_id, slug, name, platform, settings)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+projectColumns, orgID, slug, name, platform, settings)
	return scanProject(row)
}

// GetProject returns a project by id.
func GetProject(ctx context.Context, q Querier, id uuid.UUID) (*Project, error) {
	return scanProject(q.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1 AND deleted_at IS NULL`, id))
}

// GetProjectBySlug returns a project of an organization by slug.
func GetProjectBySlug(ctx context.Context, q Querier, orgID uuid.UUID, slug string) (*Project, error) {
	return scanProject(q.QueryRow(ctx, `
		SELECT `+projectColumns+` FROM projects
		WHERE org_id = $1 AND slug = $2 AND deleted_at IS NULL`, orgID, slug))
}

// ListProjects returns the projects of an organization ordered by name.
func ListProjects(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Project, error) {
	rows, err := q.Query(ctx, `
		SELECT `+projectColumns+` FROM projects
		WHERE org_id = $1 AND deleted_at IS NULL
		ORDER BY lower(name)`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject changes name, platform and settings.
func UpdateProject(ctx context.Context, q Querier, id uuid.UUID, name, platform string, settings *apiv1.ProjectSettings) (*Project, error) {
	js, err := marshalJSON(settings)
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		UPDATE projects SET name = $2, platform = $3, settings = $4, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+projectColumns, id, name, platform, js)
	return scanProject(row)
}

// MarkProjectDeleted hides a project; a background job purges its data.
func MarkProjectDeleted(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `UPDATE projects SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
