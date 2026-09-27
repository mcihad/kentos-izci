package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Automatic assignment modes (izci.api.v1.AutoAssign).
const (
	AutoAssignOff           int16 = 1
	AutoAssignOwners        int16 = 2
	AutoAssignSuspectCommit int16 = 3
)

// Ownership is a project's ownership configuration.
type Ownership struct {
	ProjectID          uuid.UUID
	OrgID              uuid.UUID
	Rules              string
	Codeowners         string
	CodeownersSource   string
	CodeownersSyncedAt *time.Time
	AutoAssign         int16
	UpdatedAt          time.Time
	// PathMappings of the linked repository (JSON), for CODEOWNERS.
	PathMappings []byte
}

const ownershipColumns = `o.project_id, o.org_id, o.rules, o.codeowners, o.codeowners_source, o.codeowners_synced_at,
	o.auto_assign, o.updated_at, coalesce(r.path_mappings, '[]'::jsonb)`

func scanOwnership(row interface{ Scan(...any) error }) (*Ownership, error) {
	var o Ownership
	if err := row.Scan(&o.ProjectID, &o.OrgID, &o.Rules, &o.Codeowners, &o.CodeownersSource, &o.CodeownersSyncedAt,
		&o.AutoAssign, &o.UpdatedAt, &o.PathMappings); err != nil {
		return nil, notFound(err)
	}
	return &o, nil
}

// GetOwnership returns a project's ownership configuration, or an empty
// one when none is saved.
func GetOwnership(ctx context.Context, q Querier, orgID, projectID uuid.UUID) (*Ownership, error) {
	o, err := scanOwnership(q.QueryRow(ctx, `
		SELECT `+ownershipColumns+` FROM project_ownership o
		LEFT JOIN project_repositories r ON r.project_id = o.project_id
		WHERE o.project_id = $1`, projectID))
	if errors.Is(err, ErrNotFound) {
		o = &Ownership{ProjectID: projectID, OrgID: orgID, PathMappings: []byte("[]")}
		_ = q.QueryRow(ctx, `SELECT path_mappings FROM project_repositories WHERE project_id = $1`, projectID).Scan(&o.PathMappings)
		return o, nil
	}
	return o, err
}

// OwnershipOf returns the saved configurations of projects.
func OwnershipOf(ctx context.Context, q Querier, projectIDs []uuid.UUID) (map[uuid.UUID]*Ownership, error) {
	rows, err := q.Query(ctx, `
		SELECT `+ownershipColumns+` FROM project_ownership o
		LEFT JOIN project_repositories r ON r.project_id = o.project_id
		WHERE o.project_id = ANY($1)`, projectIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*Ownership{}
	for rows.Next() {
		o, err := scanOwnership(rows)
		if err != nil {
			return nil, err
		}
		out[o.ProjectID] = o
	}
	return out, rows.Err()
}

// SaveOwnership stores a project's ownership configuration.
func SaveOwnership(ctx context.Context, q Querier, o *Ownership) error {
	return q.QueryRow(ctx, `
		INSERT INTO project_ownership (project_id, org_id, rules, codeowners, codeowners_source, codeowners_synced_at, auto_assign)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_id) DO UPDATE SET rules = excluded.rules, codeowners = excluded.codeowners,
		  codeowners_source = excluded.codeowners_source, codeowners_synced_at = excluded.codeowners_synced_at,
		  auto_assign = excluded.auto_assign, updated_at = now()
		RETURNING updated_at`,
		o.ProjectID, o.OrgID, o.Rules, o.Codeowners, o.CodeownersSource, o.CodeownersSyncedAt, o.AutoAssign).Scan(&o.UpdatedAt)
}

// OwnerAlias maps an owner written as "@login", "@org/team" or "#team" to
// organization members.
type OwnerAlias struct {
	Alias     string
	UserIDs   []uuid.UUID
	UpdatedAt time.Time
}

// ListOwnerAliases lists an organization's aliases by name.
func ListOwnerAliases(ctx context.Context, q Querier, orgID uuid.UUID) ([]*OwnerAlias, error) {
	rows, err := q.Query(ctx, `SELECT alias, user_ids, updated_at FROM owner_aliases WHERE org_id = $1 ORDER BY alias`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*OwnerAlias, error) {
		var a OwnerAlias
		err := r.Scan(&a.Alias, &a.UserIDs, &a.UpdatedAt)
		return &a, err
	})
}

// SetOwnerAlias maps an alias to members; no members removes it.
func SetOwnerAlias(ctx context.Context, q Querier, orgID uuid.UUID, alias string, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		_, err := q.Exec(ctx, `DELETE FROM owner_aliases WHERE org_id = $1 AND alias = $2`, orgID, alias)
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO owner_aliases (org_id, alias, user_ids) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, alias) DO UPDATE SET user_ids = excluded.user_ids, updated_at = now()`, orgID, alias, userIDs)
	return err
}

// OwnerMembers resolves owners, lowercased e-mail addresses and aliases, to
// organization members, keeping the order of each alias's members.
// Owners without members are missing from the result.
func OwnerMembers(ctx context.Context, q Querier, orgID uuid.UUID, owners []string) (map[string][]*User, error) {
	out := map[string][]*User{}
	if len(owners) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT lower(u.email), `+prefixed("u.", userColumns)+`
		FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND lower(u.email) = ANY($2)
		UNION ALL
		SELECT a.alias, `+prefixed("u.", userColumns)+`
		FROM owner_aliases a
		CROSS JOIN LATERAL unnest(a.user_ids) WITH ORDINALITY AS x(id, n)
		JOIN org_members m ON m.org_id = a.org_id AND m.user_id = x.id
		JOIN users u ON u.id = x.id
		WHERE a.org_id = $1 AND a.alias = ANY($2)`, orgID, owners)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		u, err := scanUser(leadingScanner{rows, []any{&key}})
		if err != nil {
			return nil, err
		}
		out[key] = append(out[key], u)
	}
	return out, rows.Err()
}

// leadingScanner puts destinations before those of a scan helper, for
// queries that return extra columns before a standard column list.
type leadingScanner struct {
	row  interface{ Scan(...any) error }
	lead []any
}

func (l leadingScanner) Scan(dest ...any) error { return l.row.Scan(append(l.lead, dest...)...) }

// Suspect commit states.
const (
	SuspectFound           int16 = 1
	SuspectNoFrames        int16 = 2
	SuspectNotInRepository int16 = 3
	SuspectFailed          int16 = 4
)

// SuspectCommit is the commit that last changed the failing lines of an
// issue, or why none was found.
type SuspectCommit struct {
	IssueID     uuid.UUID
	OrgID       uuid.UUID
	State       int16
	Detail      string
	SHA         string
	Message     string
	URL         string
	AuthorName  string
	AuthorEmail string
	AuthorLogin string
	CommittedAt *time.Time
	Path        string
	Line        int32
	ComputedAt  time.Time
}

// GetSuspectCommit returns the stored suspect commit of an issue.
func GetSuspectCommit(ctx context.Context, q Querier, issueID uuid.UUID) (*SuspectCommit, error) {
	var c SuspectCommit
	err := q.QueryRow(ctx, `
		SELECT issue_id, org_id, state, detail, sha, message, url, author_name, author_email, author_login, committed_at,
		       path, line, computed_at
		FROM issue_suspect_commits WHERE issue_id = $1`, issueID).Scan(&c.IssueID, &c.OrgID, &c.State, &c.Detail, &c.SHA,
		&c.Message, &c.URL, &c.AuthorName, &c.AuthorEmail, &c.AuthorLogin, &c.CommittedAt, &c.Path, &c.Line, &c.ComputedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// SaveSuspectCommit stores the result of a search for the suspect commit.
func SaveSuspectCommit(ctx context.Context, q Querier, c *SuspectCommit) error {
	return q.QueryRow(ctx, `
		INSERT INTO issue_suspect_commits (issue_id, org_id, state, detail, sha, message, url, author_name, author_email,
		  author_login, committed_at, path, line)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (issue_id) DO UPDATE SET state = excluded.state, detail = excluded.detail, sha = excluded.sha,
		  message = excluded.message, url = excluded.url, author_name = excluded.author_name,
		  author_email = excluded.author_email, author_login = excluded.author_login, committed_at = excluded.committed_at,
		  path = excluded.path, line = excluded.line, computed_at = now()
		RETURNING computed_at`,
		c.IssueID, c.OrgID, c.State, c.Detail, c.SHA, c.Message, c.URL, c.AuthorName, c.AuthorEmail, c.AuthorLogin,
		c.CommittedAt, c.Path, c.Line).Scan(&c.ComputedAt)
}

// AssignIssue assigns an unassigned issue and records who and why in its
// activity; it reports whether the issue was unassigned.
func AssignIssue(ctx context.Context, q Querier, orgID, issueID uuid.UUID, user *User, data map[string]string) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE issues SET assignee_id = $3, updated_at = now() WHERE id = $1 AND org_id = $2 AND assignee_id IS NULL`,
		issueID, orgID, user.ID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if data == nil {
		data = map[string]string{}
	}
	data["assignee"] = user.Email
	return true, AddIssueActivity(ctx, q, orgID, []uuid.UUID{issueID}, ActivityAssigned, nil, data)
}
