package store

import (
	"context"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// DeletedIssue is an issue removed by DeleteIssues.
type DeletedIssue struct {
	ID          uuid.UUID
	ProjectSlug string
	ShortSeq    int32
}

// DeleteIssues deletes issues with their comments, follows, notifications
// and user feedback, and queues their events, counters and facets (which
// carry no foreign keys) for purging (db.PurgeDeletedIssues). Unknown ids
// are skipped.
func DeleteIssues(ctx context.Context, q Querier, ids []uuid.UUID) ([]DeletedIssue, error) {
	for _, sql := range []string{
		`DELETE FROM user_feedback WHERE issue_id = ANY($1)`,
		`DELETE FROM notification_outbox WHERE issue_id = ANY($1)`,
	} {
		if _, err := q.Exec(ctx, sql, ids); err != nil {
			return nil, err
		}
	}
	rows, err := q.Query(ctx, `
		WITH d AS (
		  DELETE FROM issues WHERE id = ANY($1) RETURNING id, org_id, project_id, short_seq
		), queued AS (
		  INSERT INTO deleted_issues (issue_id, org_id, project_id) SELECT id, org_id, project_id FROM d
		)
		SELECT d.id, p.slug, coalesce(d.short_seq, 0) FROM d JOIN projects p ON p.id = d.project_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeletedIssue
	for rows.Next() {
		var d DeletedIssue
		if err := rows.Scan(&d.ID, &d.ProjectSlug, &d.ShortSeq); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkOrganizationDeleted hides an organization at once: members,
// invitations and tokens bound to it go, its projects are marked deleted
// and its slug is freed. db.PurgeDeletedProjects and
// db.PurgeDeletedOrganizations remove the rest.
func MarkOrganizationDeleted(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE organizations SET deleted_at = now(), slug = 'x' || replace(id::text, '-', ''), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	for _, sql := range []string{
		`UPDATE projects SET deleted_at = now() WHERE org_id = $1 AND deleted_at IS NULL`,
		`DELETE FROM org_members WHERE org_id = $1`,
		`DELETE FROM invitations WHERE org_id = $1`,
		`DELETE FROM personal_tokens WHERE org_id = $1`,
	} {
		if _, err := q.Exec(ctx, sql, id); err != nil {
			return err
		}
	}
	return nil
}

// OrgRef names an organization.
type OrgRef struct {
	Slug, Name string
}

// SoleOwnedOrganizations lists the organizations the user is the only
// owner of.
func SoleOwnedOrganizations(ctx context.Context, q Querier, userID uuid.UUID) ([]OrgRef, error) {
	rows, err := q.Query(ctx, `
		SELECT o.slug, o.name FROM org_members m JOIN organizations o ON o.id = m.org_id AND o.deleted_at IS NULL
		WHERE m.user_id = $1 AND m.role = $2
		  AND NOT EXISTS (SELECT 1 FROM org_members x WHERE x.org_id = m.org_id AND x.role = $2 AND x.user_id <> $1)
		ORDER BY lower(o.name)`, userID, int16(apiv1.Role_ROLE_OWNER))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgRef
	for rows.Next() {
		var o OrgRef
		if err := rows.Scan(&o.Slug, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CountSuperadmins counts the active server administrators.
func CountSuperadmins(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM users WHERE superadmin AND NOT disabled`).Scan(&n)
	return n, err
}

// DeleteUser deletes an account. Memberships, sessions, tokens, identities
// and personal settings go with it (foreign keys); comments, audit entries
// and other organization records keep their content without the author,
// and issues lose their assignment to the user. Run in system scope: the
// user's records span organizations.
func DeleteUser(ctx context.Context, q Querier, id uuid.UUID) error {
	for _, sql := range []string{
		`UPDATE issues SET assignee_id = NULL WHERE assignee_id = $1`,
		`UPDATE issues SET resolved_by = NULL WHERE resolved_by = $1`,
		`UPDATE owner_aliases SET user_ids = array_remove(user_ids, $1) WHERE $1 = ANY(user_ids)`,
	} {
		if _, err := q.Exec(ctx, sql, id); err != nil {
			return err
		}
	}
	tag, err := q.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
