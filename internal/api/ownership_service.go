package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ownership"
	"github.com/mcihad/kentos-izci/internal/store"
)

// OwnershipServer implements izci.api.v1.OwnershipService.
type OwnershipServer struct {
	*base
	ownership *ownership.Service
	apiv1connect.UnimplementedOwnershipServiceHandler
}

var _ apiv1connect.OwnershipServiceHandler = (*OwnershipServer)(nil)

// codeownersPaths are where GitHub looks for CODEOWNERS, in its order.
var codeownersPaths = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// suspectRefresh is how long a search result is reused instead of asking
// GitHub again.
const suspectRefresh = time.Minute

func (s *OwnershipServer) project(ctx context.Context, tx pgx.Tx, t *auth.Tenant, slug string) (*store.Project, error) {
	p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(slug))
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("project")
	}
	return p, err
}

// toOwnership describes a configuration, with its problems and the owners
// that resolve to no member.
func (s *OwnershipServer) toOwnership(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, o *store.Ownership) (*apiv1.Ownership, error) {
	rules, rerrs := ownership.ParseRules(o.Rules)
	co, cerrs := ownership.ParseCodeowners(o.Codeowners)
	out := &apiv1.Ownership{Rules: o.Rules, AutoAssign: apiv1.AutoAssign(o.AutoAssign), RuleCount: uint32(len(rules))}
	if !o.UpdatedAt.IsZero() {
		out.UpdatedAt = ts(o.UpdatedAt)
	}
	if o.Codeowners != "" {
		out.Codeowners = &apiv1.Codeowners{Content: o.Codeowners, Source: o.CodeownersSource, SyncedAt: tsp(o.CodeownersSyncedAt),
			RuleCount: uint32(len(co))}
	}
	for field, errs := range map[string][]*ownership.LineError{"rules": rerrs, "codeowners": cerrs} {
		for _, e := range errs {
			out.Problems = append(out.Problems, &apiv1.RuleProblem{Field: field, Line: uint32(e.Line),
				Kind: apiv1.RuleProblemKind(e.Problem), Value: e.Value})
		}
	}
	slices.SortFunc(out.Problems, func(a, b *apiv1.RuleProblem) int {
		if c := strings.Compare(a.Field, b.Field); c != 0 {
			return -c // rules first
		}
		return int(a.Line) - int(b.Line)
	})
	var owners []string
	for _, r := range append(co, rules...) {
		for _, ow := range r.Owners {
			if n := ownership.NormalizeOwner(ow); !slices.Contains(owners, n) {
				owners = append(owners, n)
			}
		}
	}
	members, err := store.OwnerMembers(ctx, tx, orgID, owners)
	if err != nil {
		return nil, err
	}
	for _, ow := range owners {
		if len(members[ow]) == 0 {
			out.UnmappedOwners = append(out.UnmappedOwners, ow)
		}
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_repositories WHERE project_id = $1 AND integration_id IS NOT NULL)`,
		o.ProjectID).Scan(&out.GithubLinked)
	return out, err
}

// GetOwnership implements OwnershipService.GetOwnership.
func (s *OwnershipServer) GetOwnership(ctx context.Context, req *connect.Request[apiv1.GetOwnershipRequest]) (*connect.Response[apiv1.GetOwnershipResponse], error) {
	var out apiv1.GetOwnershipResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		o, err := store.GetOwnership(ctx, tx, t.OrgID(), p.ID)
		if err != nil {
			return err
		}
		out.Ownership, err = s.toOwnership(ctx, tx, t.OrgID(), o)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get ownership", err)
	}
	return connect.NewResponse(&out), nil
}

// ruleError reports an invalid rule line.
func ruleError(e *ownership.LineError) error {
	err := connect.NewError(connect.CodeInvalidArgument, errors.New("rules: "+e.Error()))
	d := &apiv1.ErrorDetail{Reason: apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT, Field: "rules", Metadata: map[string]string{
		"line": strconv.Itoa(e.Line), "kind": apiv1.RuleProblemKind(e.Problem).String(), "value": e.Value}}
	if detail, derr := connect.NewErrorDetail(d); derr == nil {
		err.AddDetail(detail)
	}
	return err
}

// UpdateOwnership implements OwnershipService.UpdateOwnership.
func (s *OwnershipServer) UpdateOwnership(ctx context.Context, req *connect.Request[apiv1.UpdateOwnershipRequest]) (*connect.Response[apiv1.UpdateOwnershipResponse], error) {
	m := req.Msg
	if m.Rules != nil {
		if _, errs := ownership.ParseRules(m.GetRules()); len(errs) > 0 {
			return nil, ruleError(errs[0])
		}
	}
	var out apiv1.UpdateOwnershipResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, m.GetProject())
		if err != nil {
			return err
		}
		o, err := store.GetOwnership(ctx, tx, t.OrgID(), p.ID)
		if err != nil {
			return err
		}
		meta := map[string]string{}
		if m.Rules != nil {
			o.Rules = strings.TrimRight(strings.ReplaceAll(m.GetRules(), "\r\n", "\n"), "\n")
			meta["rules"] = "updated"
		}
		if m.AutoAssign != nil {
			o.AutoAssign = int16(m.GetAutoAssign())
			meta["auto_assign"] = m.GetAutoAssign().String()
		}
		if m.Codeowners != nil {
			o.Codeowners, o.CodeownersSource, o.CodeownersSyncedAt = strings.ReplaceAll(m.GetCodeowners(), "\r\n", "\n"), "", nil
			if strings.TrimSpace(o.Codeowners) == "" {
				o.Codeowners, meta["codeowners"] = "", "removed"
			} else {
				now := time.Now()
				o.CodeownersSyncedAt, meta["codeowners"] = &now, "pasted"
			}
		}
		if err := store.SaveOwnership(ctx, tx, o); err != nil {
			return err
		}
		if out.Ownership, err = s.toOwnership(ctx, tx, t.OrgID(), o); err != nil {
			return err
		}
		return s.audit(ctx, tx, "ownership.update", "project", p.ID.String(), p.Name, meta)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update ownership", err)
	}
	return connect.NewResponse(&out), nil
}

// ImportCodeowners implements OwnershipService.ImportCodeowners.
func (s *OwnershipServer) ImportCodeowners(ctx context.Context, req *connect.Request[apiv1.ImportCodeownersRequest]) (*connect.Response[apiv1.ImportCodeownersResponse], error) {
	var (
		projectID   uuid.UUID
		repo        *store.Repository
		integration *store.Integration
	)
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		projectID = p.ID
		repo, err = store.GetRepository(ctx, tx, p.ID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && repo.IntegrationID == nil) {
			return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_REPOSITORY_NOT_LINKED,
				"no repository is linked through a GitHub connection")
		}
		if err != nil {
			return err
		}
		integration, err = store.GetIntegration(ctx, tx, *repo.IntegrationID)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "import codeowners", err)
	}
	client, err := s.github.Client(integration)
	if err != nil {
		return nil, githubError(ctx, s.log, "import codeowners", err)
	}
	var content []byte
	var source string
	for _, path := range codeownersPaths {
		content, err = client.FileContent(ctx, repo.Owner, repo.Name, path, repo.DefaultBranch)
		var gh *github.Error
		if errors.As(err, &gh) && gh.Status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, githubError(ctx, s.log, "import codeowners", err)
		}
		source = repo.Owner + "/" + repo.Name + ":" + path
		break
	}
	if source == "" {
		return nil, errNotFound("CODEOWNERS file")
	}
	if len(content) > ownership.MaxTextBytes {
		return nil, fieldError("codeowners", "CODEOWNERS is larger than 256 KiB")
	}
	var out apiv1.ImportCodeownersResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		o, err := store.GetOwnership(ctx, tx, t.OrgID(), projectID)
		if err != nil {
			return err
		}
		now := time.Now()
		o.Codeowners, o.CodeownersSource, o.CodeownersSyncedAt = strings.ReplaceAll(string(content), "\r\n", "\n"), source, &now
		if err := store.SaveOwnership(ctx, tx, o); err != nil {
			return err
		}
		if out.Ownership, err = s.toOwnership(ctx, tx, t.OrgID(), o); err != nil {
			return err
		}
		return s.audit(ctx, tx, "ownership.import_codeowners", "project", projectID.String(), source, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "import codeowners", err)
	}
	return connect.NewResponse(&out), nil
}

// ListOwnerAliases implements OwnershipService.ListOwnerAliases.
func (s *OwnershipServer) ListOwnerAliases(ctx context.Context, _ *connect.Request[apiv1.ListOwnerAliasesRequest]) (*connect.Response[apiv1.ListOwnerAliasesResponse], error) {
	var out apiv1.ListOwnerAliasesResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		aliases, err := store.ListOwnerAliases(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		names := make([]string, len(aliases))
		for i, a := range aliases {
			names[i] = a.Alias
		}
		members, err := store.OwnerMembers(ctx, tx, t.OrgID(), names)
		if err != nil {
			return err
		}
		for _, a := range aliases {
			out.Aliases = append(out.Aliases, toOwnerAlias(a, members[a.Alias]))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list owner aliases", err)
	}
	return connect.NewResponse(&out), nil
}

func toOwnerAlias(a *store.OwnerAlias, users []*store.User) *apiv1.OwnerAlias {
	out := &apiv1.OwnerAlias{Alias: a.Alias, UpdatedAt: ts(a.UpdatedAt)}
	for _, u := range users {
		out.Users = append(out.Users, toUserRef(u))
	}
	return out
}

// SetOwnerAlias implements OwnershipService.SetOwnerAlias.
func (s *OwnershipServer) SetOwnerAlias(ctx context.Context, req *connect.Request[apiv1.SetOwnerAliasRequest]) (*connect.Response[apiv1.SetOwnerAliasResponse], error) {
	alias := ownership.NormalizeOwner(req.Msg.GetAlias())
	var ids []uuid.UUID
	for _, raw := range req.Msg.GetUserIds() {
		id, _ := uuid.Parse(raw)
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var out apiv1.SetOwnerAliasResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		var members int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY($2)`, t.OrgID(), ids).Scan(&members); err != nil {
			return err
		}
		if members != len(ids) {
			return fieldError("user_ids", "every user must be a member of the organization")
		}
		if err := store.SetOwnerAlias(ctx, tx, t.OrgID(), alias, ids); err != nil {
			return err
		}
		if len(ids) > 0 {
			users, err := store.OwnerMembers(ctx, tx, t.OrgID(), []string{alias})
			if err != nil {
				return err
			}
			out.Alias = toOwnerAlias(&store.OwnerAlias{Alias: alias, UserIDs: ids, UpdatedAt: time.Now()}, users[alias])
		}
		return s.audit(ctx, tx, "owner_alias.update", "owner_alias", alias, alias, map[string]string{"members": strconv.Itoa(len(ids))})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "set owner alias", err)
	}
	return connect.NewResponse(&out), nil
}

// GetIssueOwners implements OwnershipService.GetIssueOwners.
func (s *OwnershipServer) GetIssueOwners(ctx context.Context, req *connect.Request[apiv1.GetIssueOwnersRequest]) (*connect.Response[apiv1.GetIssueOwnersResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	owners, err := s.ownership.IssueOwners(ctx, t.OrgID(), issueID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("issue")
	}
	if err != nil {
		return nil, internal(ctx, s.log, "issue owners", err)
	}
	out := &apiv1.GetIssueOwnersResponse{UnmappedOwners: owners.Unmapped}
	if owners.Winner != nil {
		out.DecidingRule = owners.Winner.Text()
	}
	for _, ow := range owners.Owners {
		o := &apiv1.IssueOwner{User: toUserRef(ow.User), Suggested: owners.Assignee != nil && owners.Assignee.ID == ow.User.ID}
		for _, r := range ow.Rules {
			o.Rules = append(o.Rules, r.Text())
		}
		out.Owners = append(out.Owners, o)
	}
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		sc, err := store.GetSuspectCommit(ctx, tx, issueID)
		if errors.Is(err, store.ErrNotFound) {
			state := apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NOT_CONFIGURED
			var linked bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issues i JOIN project_repositories r ON r.project_id = i.project_id
				WHERE i.id = $1 AND r.integration_id IS NOT NULL)`, issueID).Scan(&linked); err != nil {
				return err
			}
			if linked {
				state = apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_UNKNOWN
			}
			out.SuspectCommit = &apiv1.SuspectCommit{State: state}
			return nil
		}
		if err != nil {
			return err
		}
		out.SuspectCommit, err = s.toSuspect(ctx, tx, t.OrgID(), sc)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "issue owners", err)
	}
	return connect.NewResponse(out), nil
}

func (s *OwnershipServer) toSuspect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, sc *store.SuspectCommit) (*apiv1.SuspectCommit, error) {
	out := &apiv1.SuspectCommit{Sha: sc.SHA, Message: sc.Message, Url: sc.URL, AuthorName: sc.AuthorName, AuthorEmail: sc.AuthorEmail,
		AuthorLogin: sc.AuthorLogin, CommittedAt: tsp(sc.CommittedAt), Path: sc.Path, Line: uint32(sc.Line), Detail: sc.Detail,
		ComputedAt: ts(sc.ComputedAt)}
	switch sc.State {
	case store.SuspectFound:
		out.State = apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_FOUND
		u, err := s.ownership.CommitAuthor(ctx, tx, orgID, sc)
		if err != nil {
			return nil, err
		}
		if u != nil {
			out.Author = toUserRef(u)
		}
	case store.SuspectNoFrames:
		out.State = apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NO_FRAMES
	case store.SuspectNotInRepository:
		out.State = apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NOT_IN_REPOSITORY
	default:
		out.State = apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_FAILED
	}
	return out, nil
}

// FindSuspectCommit implements OwnershipService.FindSuspectCommit.
func (s *OwnershipServer) FindSuspectCommit(ctx context.Context, req *connect.Request[apiv1.FindSuspectCommitRequest]) (*connect.Response[apiv1.FindSuspectCommitResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	var out apiv1.FindSuspectCommitResponse
	// A fresh result is returned as is: the button cannot hammer GitHub.
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		sc, err := store.GetSuspectCommit(ctx, tx, issueID)
		if err != nil || time.Since(sc.ComputedAt) > suspectRefresh {
			return nil
		}
		out.SuspectCommit, err = s.toSuspect(ctx, tx, t.OrgID(), sc)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "find suspect commit", err)
	}
	if out.SuspectCommit != nil {
		return connect.NewResponse(&out), nil
	}
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sc, err := s.ownership.FindSuspectCommit(sctx, t.OrgID(), issueID)
	switch {
	case errors.Is(err, ownership.ErrNotConfigured):
		out.SuspectCommit = &apiv1.SuspectCommit{State: apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NOT_CONFIGURED}
		return connect.NewResponse(&out), nil
	case errors.Is(err, store.ErrNotFound):
		return nil, errNotFound("issue")
	case err != nil:
		return nil, internal(ctx, s.log, "find suspect commit", err)
	}
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		var err error
		out.SuspectCommit, err = s.toSuspect(ctx, tx, t.OrgID(), sc)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "find suspect commit", err)
	}
	return connect.NewResponse(&out), nil
}
