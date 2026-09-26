package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/mcihad/kentos-izci/internal/store"
)

// IntegrationServer implements izci.api.v1.IntegrationService and the
// GitHub App installation callback.
type IntegrationServer struct {
	*base
	apiv1connect.UnimplementedIntegrationServiceHandler
}

var _ apiv1connect.IntegrationServiceHandler = (*IntegrationServer)(nil)

// installStateTTL bounds how long a GitHub App installation may take.
const installStateTTL = time.Hour

func errAppNotConfigured() error {
	return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_GITHUB_APP_NOT_CONFIGURED,
		"no GitHub App is configured on this server")
}

// githubError maps errors of GitHub calls to API errors.
func githubError(ctx context.Context, log *slog.Logger, op string, err error) error {
	var gh *github.Error
	switch {
	case errors.Is(err, github.ErrAppNotConfigured):
		return errAppNotConfigured()
	case errors.As(err, &gh) && (gh.Status == http.StatusUnauthorized || gh.Status == http.StatusForbidden):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_GITHUB_TOKEN_INVALID,
			"GitHub rejected the credentials: "+gh.Message)
	case errors.As(err, &gh):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED,
			fmt.Sprintf("GitHub returned %d: %s", gh.Status, gh.Message), "status", strconv.Itoa(gh.Status))
	case strings.HasPrefix(err.Error(), "github "):
		return apiError(connect.CodeUnavailable, apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED, "GitHub is not reachable")
	}
	return internal(ctx, log, op, err)
}

func toIntegration(i *store.Integration, users map[uuid.UUID]*apiv1.UserRef) *apiv1.Integration {
	out := &apiv1.Integration{Id: i.ID.String(), Provider: i.Provider, Auth: apiv1.IntegrationAuth(i.Auth), Name: i.Name,
		AccountLogin: i.AccountLogin, AccountAvatarUrl: i.AccountAvatarURL, Healthy: i.Healthy, StatusMessage: i.StatusMessage,
		CreatedAt: ts(i.CreatedAt), VerifiedAt: tsp(i.VerifiedAt)}
	if i.InstallationID != nil {
		out.InstallationId = *i.InstallationID
	}
	if i.CreatedBy != nil {
		out.CreatedBy = users[*i.CreatedBy]
	}
	return out
}

// ListIntegrations implements IntegrationService.ListIntegrations.
func (s *IntegrationServer) ListIntegrations(ctx context.Context, _ *connect.Request[apiv1.ListIntegrationsRequest]) (*connect.Response[apiv1.ListIntegrationsResponse], error) {
	var out apiv1.ListIntegrationsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		list, err := store.ListIntegrations(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, i := range list {
			if i.CreatedBy != nil {
				ids = append(ids, *i.CreatedBy)
			}
		}
		users, err := userRefs(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, i := range list {
			out.Integrations = append(out.Integrations, toIntegration(i, users))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "ListIntegrations", err)
	}
	return connect.NewResponse(&out), nil
}

// ConnectGitHubToken implements IntegrationService.ConnectGitHubToken.
func (s *IntegrationServer) ConnectGitHubToken(ctx context.Context, req *connect.Request[apiv1.ConnectGitHubTokenRequest]) (*connect.Response[apiv1.ConnectGitHubTokenResponse], error) {
	token := strings.TrimSpace(req.Msg.GetToken())
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, fieldError("name", "name is required")
	}
	account, err := s.github.Verify(ctx, token)
	if err != nil {
		return nil, githubError(ctx, s.log, "ConnectGitHubToken", err)
	}
	p := auth.PrincipalFrom(ctx)
	var out apiv1.ConnectGitHubTokenResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		id := uuid.Must(uuid.NewV7())
		i, err := store.CreateIntegration(ctx, tx, &store.Integration{ID: id, OrgID: t.OrgID(), Auth: store.IntegrationToken, Name: name,
			AccountLogin: account.Login, AccountAvatarURL: account.AvatarURL, Secret: s.github.SealToken(id, token), CreatedBy: &p.User.ID})
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "integration.connect", "integration", id.String(), name, map[string]string{"account": account.Login}); err != nil {
			return err
		}
		out.Integration = toIntegration(i, map[uuid.UUID]*apiv1.UserRef{p.User.ID: toUserRef(p.User)})
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "ConnectGitHubToken", err)
	}
	return connect.NewResponse(&out), nil
}

// installState binds a GitHub App installation to the organization and
// user that started it.
type installState struct {
	org     uuid.UUID
	user    uuid.UUID
	expires time.Time
}

func (s *IntegrationServer) signState(st installState) string {
	msg := st.org.String() + "|" + st.user.String() + "|" + strconv.FormatInt(st.expires.Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(msg)) + "." + s.auth.Box().Sign("github-install|"+msg, 16)
}

func (s *IntegrationServer) readState(raw string) (installState, bool) {
	enc, sig, ok := strings.Cut(raw, ".")
	msg, err := base64.RawURLEncoding.DecodeString(enc)
	if !ok || err != nil || !s.auth.Box().Verify("github-install|"+string(msg), sig, 16) {
		return installState{}, false
	}
	parts := strings.Split(string(msg), "|")
	if len(parts) != 3 {
		return installState{}, false
	}
	org, err1 := uuid.Parse(parts[0])
	user, err2 := uuid.Parse(parts[1])
	exp, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || time.Now().After(time.Unix(exp, 0)) {
		return installState{}, false
	}
	return installState{org: org, user: user, expires: time.Unix(exp, 0)}, true
}

// StartGitHubAppInstallation implements IntegrationService.StartGitHubAppInstallation.
func (s *IntegrationServer) StartGitHubAppInstallation(ctx context.Context, _ *connect.Request[apiv1.StartGitHubAppInstallationRequest]) (*connect.Response[apiv1.StartGitHubAppInstallationResponse], error) {
	if s.github.App == nil {
		return nil, errAppNotConfigured()
	}
	state := s.signState(installState{org: auth.TenantFrom(ctx).OrgID(), user: auth.PrincipalFrom(ctx).User.ID,
		expires: time.Now().Add(installStateTTL)})
	return connect.NewResponse(&apiv1.StartGitHubAppInstallationResponse{InstallUrl: s.github.App.InstallURL(state)}), nil
}

// GitHubCallback completes an installation: GitHub redirects the browser
// here with the installation id, an OAuth code and the state. The
// installation is linked only when the signed-in izci user started it,
// administers the organization, and the GitHub user who came back can
// access the installation.
func (s *IntegrationServer) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	back := func(slug, result string) {
		target := "/"
		if slug != "" {
			target = "/o/" + url.PathEscape(slug) + "/settings"
		}
		http.Redirect(w, r, target+"?tab=integrations&github="+url.QueryEscape(result), http.StatusFound)
	}
	st, ok := s.readState(q.Get("state"))
	if !ok || s.github.App == nil {
		back("", "invalid")
		return
	}
	org, err := store.GetOrganization(ctx, s.db.Pool, st.org)
	if err != nil {
		back("", "invalid")
		return
	}
	p, err := s.auth.Authenticate(ctx, r.Header)
	if err != nil || p == nil || p.Session == nil || p.User.ID != st.user {
		back(org.Slug, "session")
		return
	}
	t, err := s.auth.ResolveTenant(ctx, p, org.Slug)
	if err != nil || t.Role < apiv1.Role_ROLE_ADMIN {
		back(org.Slug, "forbidden")
		return
	}
	if q.Get("setup_action") == "request" {
		// A GitHub organization owner has to approve the installation first.
		back(org.Slug, "requested")
		return
	}
	installation, err := strconv.ParseInt(q.Get("installation_id"), 10, 64)
	code := q.Get("code")
	if err != nil || installation <= 0 || code == "" {
		back(org.Slug, "oauth")
		return
	}
	account, err := s.github.App.VerifyInstaller(ctx, code, installation)
	if err != nil {
		s.log.Warn("github app installation rejected", slog.String("org", org.Slug), slog.Any("err", err))
		back(org.Slug, "denied")
		return
	}
	ctx = auth.WithTenant(auth.WithPrincipal(ctx, p), t)
	err = s.db.InSystem(ctx, func(tx pgx.Tx) error {
		i, err := store.LinkAppInstallation(ctx, tx, &store.Integration{OrgID: org.ID, Name: account.Login + " (GitHub App)",
			AccountLogin: account.Login, AccountAvatarURL: account.AvatarURL, InstallationID: &installation, CreatedBy: &p.User.ID})
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, "integration.connect", "integration", i.ID.String(), i.Name,
			map[string]string{"account": account.Login, "installation": strconv.FormatInt(installation, 10)})
	})
	switch {
	case errors.Is(err, store.ErrInstallationTaken):
		back(org.Slug, "taken")
	case err != nil:
		s.log.Error("link github app installation", slog.Any("err", err))
		back(org.Slug, "error")
	default:
		back(org.Slug, "connected")
	}
}

// ListRepositories implements IntegrationService.ListRepositories.
func (s *IntegrationServer) ListRepositories(ctx context.Context, req *connect.Request[apiv1.ListRepositoriesRequest]) (*connect.Response[apiv1.ListRepositoriesResponse], error) {
	id, err := uuid.Parse(req.Msg.GetIntegrationId())
	if err != nil {
		return nil, fieldError("integration_id", "invalid integration id")
	}
	var integration *store.Integration
	if err := s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
		integration, err = store.GetIntegration(ctx, tx, id)
		return err
	}); err != nil {
		return nil, internal(ctx, s.log, "ListRepositories", err)
	}
	client, err := s.github.Client(integration)
	if err != nil {
		return nil, githubError(ctx, s.log, "ListRepositories", err)
	}
	repos, err := client.ListRepositories(ctx, req.Msg.GetQuery())
	s.recordHealth(ctx, integration, err)
	if err != nil {
		return nil, githubError(ctx, s.log, "ListRepositories", err)
	}
	var out apiv1.ListRepositoriesResponse
	for _, r := range repos {
		out.Repositories = append(out.Repositories, &apiv1.Repository{Owner: r.Owner.Login, Name: r.Name, FullName: r.FullName,
			Url: r.HTMLURL, DefaultBranch: r.DefaultBranch, Private: r.Private, Description: r.Description})
	}
	return connect.NewResponse(&out), nil
}

// recordHealth remembers whether GitHub accepted an integration's
// credentials, so that broken connections show up in the UI.
func (s *IntegrationServer) recordHealth(ctx context.Context, i *store.Integration, err error) {
	var gh *github.Error
	healthy, msg := err == nil, ""
	if errors.As(err, &gh) && (gh.Status == http.StatusUnauthorized || gh.Status == http.StatusForbidden) {
		msg = gh.Message
	} else if err != nil {
		return // a network problem says nothing about the credentials
	}
	if healthy == i.Healthy && msg == i.StatusMessage {
		return
	}
	_ = s.db.InOrg(ctx, i.OrgID, func(tx pgx.Tx) error { return store.SetIntegrationHealth(ctx, tx, i.ID, healthy, msg) })
}

// DeleteIntegration implements IntegrationService.DeleteIntegration.
func (s *IntegrationServer) DeleteIntegration(ctx context.Context, req *connect.Request[apiv1.DeleteIntegrationRequest]) (*connect.Response[apiv1.DeleteIntegrationResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, fieldError("id", "invalid integration id")
	}
	err = s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
		i, err := store.GetIntegration(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := store.DeleteIntegration(ctx, tx, id); err != nil {
			return err
		}
		return s.audit(ctx, tx, "integration.delete", "integration", id.String(), i.Name, map[string]string{"account": i.AccountLogin})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "DeleteIntegration", err)
	}
	return connect.NewResponse(&apiv1.DeleteIntegrationResponse{}), nil
}

// GetGitHubStatus implements IntegrationService.GetGitHubStatus.
func (s *IntegrationServer) GetGitHubStatus(context.Context, *connect.Request[apiv1.GetGitHubStatusRequest]) (*connect.Response[apiv1.GetGitHubStatusResponse], error) {
	out := &apiv1.GetGitHubStatusResponse{}
	if s.github.App != nil {
		out.AppConfigured, out.AppSlug = true, s.github.App.Slug
	}
	return connect.NewResponse(out), nil
}
