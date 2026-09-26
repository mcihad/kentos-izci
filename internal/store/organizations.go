package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// Organization is a tenant.
type Organization struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	Plan      string
	Settings  *apiv1.OrganizationSettings
	Limits    *apiv1.OrganizationLimits
	CreatedAt time.Time
	UpdatedAt time.Time
}

const orgColumns = `id, slug, name, plan, settings, limits, created_at, updated_at`

func scanOrganization(row interface{ Scan(...any) error }) (*Organization, error) {
	var o Organization
	var settings, limits []byte
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.Plan, &settings, &limits, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	o.Settings = &apiv1.OrganizationSettings{}
	o.Limits = &apiv1.OrganizationLimits{}
	if err := unmarshalJSON(settings, o.Settings); err != nil {
		return nil, fmt.Errorf("decode organization settings: %w", err)
	}
	if err := unmarshalJSON(limits, o.Limits); err != nil {
		return nil, fmt.Errorf("decode organization limits: %w", err)
	}
	return &o, nil
}

// DefaultOrganizationSettings are applied to new organizations.
func DefaultOrganizationSettings() *apiv1.OrganizationSettings {
	return &apiv1.OrganizationSettings{Timezone: "Europe/Istanbul", AutoJoinRole: apiv1.Role_ROLE_MEMBER}
}

// CreateOrganization inserts an organization. A taken slug yields a unique
// violation on "organizations_slug_key".
func CreateOrganization(ctx context.Context, q Querier, slug, name string) (*Organization, error) {
	settings, err := marshalJSON(DefaultOrganizationSettings())
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		INSERT INTO organizations (slug, name, settings)
		VALUES ($1, $2, $3)
		RETURNING `+orgColumns, slug, name, settings)
	return scanOrganization(row)
}

// GetOrganization returns an organization by id.
func GetOrganization(ctx context.Context, q Querier, id uuid.UUID) (*Organization, error) {
	return scanOrganization(q.QueryRow(ctx, `SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id))
}

// GetOrganizationBySlug returns an organization by slug.
func GetOrganizationBySlug(ctx context.Context, q Querier, slug string) (*Organization, error) {
	return scanOrganization(q.QueryRow(ctx, `SELECT `+orgColumns+` FROM organizations WHERE slug = $1`, slug))
}

// UpdateOrganization changes name and settings.
func UpdateOrganization(ctx context.Context, q Querier, id uuid.UUID, name string, settings *apiv1.OrganizationSettings) (*Organization, error) {
	js, err := marshalJSON(settings)
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		UPDATE organizations SET name = $2, settings = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+orgColumns, id, name, js)
	return scanOrganization(row)
}

// UpdateOrganizationPlan changes plan and limits (instance administration).
func UpdateOrganizationPlan(ctx context.Context, q Querier, id uuid.UUID, plan string, limits *apiv1.OrganizationLimits) (*Organization, error) {
	js, err := marshalJSON(limits)
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		UPDATE organizations SET plan = $2, limits = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+orgColumns, id, plan, js)
	return scanOrganization(row)
}

// AddMember adds a user to an organization or changes their role.
func AddMember(ctx context.Context, q Querier, orgID, userID uuid.UUID, role apiv1.Role) error {
	_, err := q.Exec(ctx, `
		INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = excluded.role`, orgID, userID, int16(role))
	return err
}

// MemberRole returns the user's role in the organization, or ErrNotFound.
func MemberRole(ctx context.Context, q Querier, orgID, userID uuid.UUID) (apiv1.Role, error) {
	var role int16
	err := q.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	if err != nil {
		return apiv1.Role_ROLE_UNSPECIFIED, notFound(err)
	}
	return apiv1.Role(role), nil
}

// Membership pairs an organization with a role.
type Membership struct {
	Organization *Organization
	Role         apiv1.Role
}

// ListMemberships lists the organizations of a user, ordered by name.
func ListMemberships(ctx context.Context, q Querier, userID uuid.UUID) ([]Membership, error) {
	rows, err := q.Query(ctx, `
		SELECT o.id, o.slug, o.name, o.plan, o.settings, o.limits, o.created_at, o.updated_at, m.role
		FROM org_members m JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY lower(o.name)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var o Organization
		var settings, limits []byte
		var role int16
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.Plan, &settings, &limits, &o.CreatedAt, &o.UpdatedAt, &role); err != nil {
			return nil, err
		}
		o.Settings, o.Limits = &apiv1.OrganizationSettings{}, &apiv1.OrganizationLimits{}
		if err := unmarshalJSON(settings, o.Settings); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(limits, o.Limits); err != nil {
			return nil, err
		}
		out = append(out, Membership{Organization: &o, Role: apiv1.Role(role)})
	}
	return out, rows.Err()
}
