package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/store"
)

func metadata(err error) map[string]string {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return nil
	}
	for _, d := range cerr.Details() {
		if v, derr := d.Value(); derr == nil {
			if ed, ok := v.(*apiv1.ErrorDetail); ok {
				return ed.GetMetadata()
			}
		}
	}
	return nil
}

func TestOwnership(t *testing.T) {
	var treeCalls atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/kentos/kalem/contents/CODEOWNERS" && r.URL.Query().Get("ref") == "main":
			_ = json.NewEncoder(w).Encode(map[string]string{"type": "file", "encoding": "base64",
				"content": base64.StdEncoding.EncodeToString([]byte("# Owners\n* @kentos/platform\n/src/ owner@example.com\n"))})
		case strings.HasPrefix(r.URL.Path, "/repos/kentos/kalem/git/trees/"):
			treeCalls.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(gh.Close)
	var access *github.Access
	e := newEnv(t, func(e *env, d *api.Deps) {
		e.cfg.GitHub = config.GitHubConfig{APIURL: gh.URL, WebURL: "https://github.com"}
		access = github.NewAccess(gh.URL, "https://github.com", nil, e.box, gh.Client())
		d.GitHub = access
	})
	ctx := context.Background()
	owner, issueID := setupOrg(t, e)
	sess, _ := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	ownerID := sess.Msg.GetSession().GetUser().GetId()

	got, err := owner.ownership.GetOwnership(ctx, req(&apiv1.GetOwnershipRequest{Project: "kalem"}))
	if err != nil || got.Msg.GetOwnership().GetGithubLinked() || got.Msg.GetOwnership().GetUpdatedAt() != nil {
		t.Fatalf("empty ownership: %v %v", got, err)
	}

	// Invalid rules are refused with the line and what is wrong.
	_, err = owner.ownership.UpdateOwnership(ctx, req(&apiv1.UpdateOwnershipRequest{Project: "kalem",
		Rules: ptr("path:src/* owner@example.com\nfoo:bar owner@example.com")}))
	if md := metadata(err); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT || field(err) != "rules" ||
		md["line"] != "2" || md["kind"] != "RULE_PROBLEM_KIND_UNKNOWN_TYPE" || md["value"] != "foo" {
		t.Fatalf("invalid rules: %v %v", err, md)
	}

	upd, err := owner.ownership.UpdateOwnership(ctx, req(&apiv1.UpdateOwnershipRequest{Project: "kalem",
		Rules:      ptr("# Test events\ntags.izci_test:true @backend owner@example.com\n"),
		AutoAssign: apiv1.AutoAssign_AUTO_ASSIGN_OWNERS.Enum(),
		Codeowners: ptr("/src/ @Backend\n/docs/ notanowner\n")}))
	o := upd.Msg.GetOwnership()
	if err != nil || o.GetRuleCount() != 1 || o.GetAutoAssign() != apiv1.AutoAssign_AUTO_ASSIGN_OWNERS || o.GetCodeowners().GetRuleCount() != 1 ||
		!slices.Equal(o.GetUnmappedOwners(), []string{"@backend"}) || len(o.GetProblems()) != 1 ||
		o.GetProblems()[0].GetKind() != apiv1.RuleProblemKind_RULE_PROBLEM_KIND_INVALID_OWNER || o.GetProblems()[0].GetLine() != 2 {
		t.Fatalf("update: %v %v", upd, err)
	}

	// Aliases map "@backend" to members.
	if _, err := owner.ownership.SetOwnerAlias(ctx, req(&apiv1.SetOwnerAliasRequest{Alias: "@Backend", UserIds: []string{uuid.NewString()}})); field(err) != "user_ids" {
		t.Fatalf("alias to a non-member: %v", err)
	}
	alias, err := owner.ownership.SetOwnerAlias(ctx, req(&apiv1.SetOwnerAliasRequest{Alias: "@Backend", UserIds: []string{ownerID}}))
	if err != nil || alias.Msg.GetAlias().GetAlias() != "@backend" || alias.Msg.GetAlias().GetUsers()[0].GetId() != ownerID {
		t.Fatalf("alias: %v %v", alias, err)
	}
	got, _ = owner.ownership.GetOwnership(ctx, req(&apiv1.GetOwnershipRequest{Project: "kalem"}))
	if len(got.Msg.GetOwnership().GetUnmappedOwners()) != 0 {
		t.Fatalf("unmapped after alias: %v", got.Msg.GetOwnership().GetUnmappedOwners())
	}
	list, _ := owner.ownership.ListOwnerAliases(ctx, req(&apiv1.ListOwnerAliasesRequest{}))
	if len(list.Msg.GetAliases()) != 1 {
		t.Fatalf("aliases: %v", list.Msg)
	}

	// Owners of an issue by its latest event.
	owners, err := owner.ownership.GetIssueOwners(ctx, req(&apiv1.GetIssueOwnersRequest{IssueId: issueID}))
	if err != nil || len(owners.Msg.GetOwners()) != 1 || owners.Msg.GetOwners()[0].GetUser().GetId() != ownerID ||
		!owners.Msg.GetOwners()[0].GetSuggested() || owners.Msg.GetDecidingRule() != "tags.izci_test:true" ||
		owners.Msg.GetSuspectCommit().GetState() != apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NOT_CONFIGURED {
		t.Fatalf("issue owners: %v %v", owners, err)
	}
	found, err := owner.ownership.FindSuspectCommit(ctx, req(&apiv1.FindSuspectCommitRequest{IssueId: issueID}))
	if err != nil || found.Msg.GetSuspectCommit().GetState() != apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_NOT_CONFIGURED {
		t.Fatalf("suspect without repository: %v %v", found, err)
	}
	if _, err := owner.ownership.ImportCodeowners(ctx, req(&apiv1.ImportCodeownersRequest{Project: "kalem"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_REPOSITORY_NOT_LINKED {
		t.Fatalf("import without repository: %v", err)
	}

	// With a repository linked through GitHub: CODEOWNERS comes from the
	// first place GitHub looks that has one.
	err = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		var orgID, projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT org_id, id FROM projects WHERE slug = 'kalem'`).Scan(&orgID, &projectID); err != nil {
			return err
		}
		id := uuid.New()
		if _, err := store.CreateIntegration(ctx, tx, &store.Integration{ID: id, OrgID: orgID, Auth: store.IntegrationToken, Name: "kentos",
			AccountLogin: "kentos", Secret: access.SealToken(id, "ghp_test")}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_repositories (project_id, org_id, integration_id, provider, owner, name, url, default_branch, path_mappings)
			VALUES ($1, $2, $3, 'github', 'kentos', 'kalem', 'https://github.com/kentos/kalem', 'main', '[]')`, projectID, orgID, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := owner.ownership.ImportCodeowners(ctx, req(&apiv1.ImportCodeownersRequest{Project: "kalem"}))
	if co := imported.Msg.GetOwnership().GetCodeowners(); err != nil || co.GetSource() != "kentos/kalem:CODEOWNERS" || co.GetRuleCount() != 2 ||
		!imported.Msg.GetOwnership().GetGithubLinked() || !slices.Equal(imported.Msg.GetOwnership().GetUnmappedOwners(), []string{"@kentos/platform"}) {
		t.Fatalf("import: %v %v", imported, err)
	}

	// A failed search is stored and answered again for a minute without
	// asking GitHub.
	owners, _ = owner.ownership.GetIssueOwners(ctx, req(&apiv1.GetIssueOwnersRequest{IssueId: issueID}))
	if owners.Msg.GetSuspectCommit().GetState() != apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_UNKNOWN {
		t.Fatalf("suspect before search: %v", owners.Msg.GetSuspectCommit())
	}
	for range 2 {
		found, err = owner.ownership.FindSuspectCommit(ctx, req(&apiv1.FindSuspectCommitRequest{IssueId: issueID}))
		if sc := found.Msg.GetSuspectCommit(); err != nil || sc.GetState() != apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_FAILED ||
			!strings.Contains(sc.GetDetail(), "403") {
			t.Fatalf("failed search: %v %v", found, err)
		}
	}
	if n := treeCalls.Load(); n != 1 {
		t.Fatalf("GitHub asked %d times", n)
	}

	// Viewers read, admins change.
	viewer := e.client(map[string]string{"Izci-Org": "kentos"})
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "viewer@example.com", Role: apiv1.Role_ROLE_VIEWER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	if _, err := viewer.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Viewer", Password: "izleyicinin parolası"})); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.ownership.GetIssueOwners(ctx, req(&apiv1.GetIssueOwnersRequest{IssueId: issueID})); err != nil {
		t.Fatalf("viewer reads owners: %v", err)
	}
	if _, err := viewer.ownership.UpdateOwnership(ctx, req(&apiv1.UpdateOwnershipRequest{Project: "kalem", Rules: ptr("")})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer changes rules: %v", err)
	}
}
