package api_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

type env struct {
	srv *httptest.Server
	cfg *config.Config
}

func newEnv(t *testing.T) *env {
	t.Helper()
	database := testutil.NewDB(t)
	cfg := &config.Config{
		Env: config.EnvTest, PublicURL: "http://izci.test",
		Auth: config.AuthConfig{Signup: "invite", SessionTTL: time.Hour, PasswordLogin: true, InvitationTTL: time.Hour},
	}
	box, _ := secret.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	svc := auth.NewService(database, cfg, box, log)
	mux := http.NewServeMux()
	if _, err := api.Mount(mux.Handle, api.Deps{DB: database, Auth: svc, Config: cfg, Log: log}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, cfg: cfg}
}

// client is a browser-like API client with its own cookie jar.
type client struct {
	auth apiv1connect.AuthServiceClient
	user apiv1connect.UserServiceClient
	org  apiv1connect.OrganizationServiceClient
	hdr  map[string]string
}

func (e *env) client(hdr map[string]string) *client {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	opt := connect.WithInterceptors(headerInterceptor(hdr))
	return &client{
		auth: apiv1connect.NewAuthServiceClient(hc, e.srv.URL, opt),
		user: apiv1connect.NewUserServiceClient(hc, e.srv.URL, opt),
		org:  apiv1connect.NewOrganizationServiceClient(hc, e.srv.URL, opt),
		hdr:  hdr,
	}
}

type headerInterceptor map[string]string

func (h headerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		for k, v := range h {
			req.Header().Set(k, v)
		}
		return next(ctx, req)
	}
}
func (h headerInterceptor) WrapStreamingClient(n connect.StreamingClientFunc) connect.StreamingClientFunc {
	return n
}
func (h headerInterceptor) WrapStreamingHandler(n connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return n
}

func reason(err error) apiv1.ErrorReason {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return apiv1.ErrorReason_ERROR_REASON_UNSPECIFIED
	}
	for _, d := range cerr.Details() {
		if v, derr := d.Value(); derr == nil {
			if ed, ok := v.(*apiv1.ErrorDetail); ok {
				return ed.GetReason()
			}
		}
	}
	return apiv1.ErrorReason_ERROR_REASON_UNSPECIFIED
}

func field(err error) string {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		for _, d := range cerr.Details() {
			if v, derr := d.Value(); derr == nil {
				if ed, ok := v.(*apiv1.ErrorDetail); ok {
					return ed.GetField()
				}
			}
		}
	}
	return ""
}

func req[T any](m *T) *connect.Request[T] { return connect.NewRequest(m) }

func TestSetupLoginAndSessions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin := e.client(map[string]string{"Izci-Org": "kentos"})

	st, err := admin.auth.GetSetupState(ctx, req(&apiv1.GetSetupStateRequest{}))
	if err != nil || !st.Msg.GetSetupRequired() {
		t.Fatalf("setup state: %v %v", st, err)
	}
	if _, err := admin.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Ayşe", Email: "ayse@example.com",
		Password: "123", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT || field(err) != "password" {
		t.Fatalf("short password must fail validation on the password field: %v (%s)", err, field(err))
	}
	setup, err := admin.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Ayşe Yılmaz", Email: "Ayse@Example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "KentOS Kalem", ProjectPlatform: "dotnet"}))
	if err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	if !setup.Msg.GetSession().GetUser().GetSuperadmin() || setup.Msg.GetProjectSlug() != "kentos-kalem" || !strings.HasPrefix(setup.Msg.GetApiKey(), "izk_") {
		t.Fatalf("setup response: %v", setup.Msg)
	}
	if _, err := admin.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "X", Email: "x@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "X", OrganizationSlug: "xx", ProjectName: "X"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_SETUP_COMPLETED {
		t.Fatalf("second setup: %v", err)
	}

	// The cookie from setup authenticates.
	sess, err := admin.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if err != nil || sess.Msg.GetSession().GetUser().GetEmail() != "ayse@example.com" || sess.Msg.GetSession().GetMemberships()[0].GetRole() != apiv1.Role_ROLE_OWNER {
		t.Fatalf("session: %v %v", sess, err)
	}
	org, err := admin.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
	if err != nil || org.Msg.GetOrganization().GetProjectCount() != 1 || org.Msg.GetOrganization().GetSettings().GetTimezone() != "Europe/Istanbul" {
		t.Fatalf("organization: %v %v", org, err)
	}

	if _, err := admin.auth.Logout(ctx, req(&apiv1.LogoutRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("after logout: %v", err)
	}

	if _, err := admin.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "ayse@example.com", Password: "wrong password!"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_CREDENTIALS {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := admin.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "AYSE@example.com", Password: "uzun ve güçlü parola"})); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Personal access tokens: bound to the org, read-only scope.
	tok, err := admin.user.CreateAccessToken(ctx, req(&apiv1.CreateAccessTokenRequest{Name: "cli", Organization: "kentos",
		Scopes: []apiv1.TokenScope{apiv1.TokenScope_TOKEN_SCOPE_READ}}))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	bot := e.client(map[string]string{"Authorization": "Bearer " + tok.Msg.GetSecret()})
	if o, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil || o.Msg.GetOrganization().GetSlug() != "kentos" {
		t.Fatalf("token without Izci-Org must use its organization: %v %v", o, err)
	}
	if _, err := bot.org.UpdateOrganization(ctx, req(&apiv1.UpdateOrganizationRequest{Name: "Hacked"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("read-only token must not write: %v", err)
	}
	if _, err := bot.user.CreateAccessToken(ctx, req(&apiv1.CreateAccessTokenRequest{Name: "x", Scopes: []apiv1.TokenScope{apiv1.TokenScope_TOKEN_SCOPE_READ}})); err == nil {
		t.Fatal("tokens must not mint tokens")
	}
	if _, err := admin.user.RevokeAccessToken(ctx, req(&apiv1.RevokeAccessTokenRequest{Id: tok.Msg.GetToken().GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("revoked token: %v", err)
	}

	// Login throttling.
	attacker := e.client(nil)
	var last error
	for range 12 {
		_, last = attacker.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "target@example.com", Password: "guess-guess"}))
	}
	if reason(last) != apiv1.ErrorReason_ERROR_REASON_TOO_MANY_ATTEMPTS {
		t.Fatalf("expected throttling, got %v", last)
	}
}

func TestInvitationsRolesAndTenancy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "Mehmet@Example.com", Role: apiv1.Role_ROLE_MEMBER}))
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	token := url[strings.LastIndex(url, "/")+1:]
	if !strings.HasPrefix(url, "http://izci.test/davet/izi_") {
		t.Fatalf("invitation url: %s", url)
	}

	guest := e.client(map[string]string{"Izci-Org": "kentos"})
	info, err := guest.auth.GetInvitation(ctx, req(&apiv1.GetInvitationRequest{Token: token}))
	if err != nil || info.Msg.GetInvitation().GetOrganization().GetName() != "KentOS" || info.Msg.GetAccountExists() {
		t.Fatalf("get invitation: %v %v", info, err)
	}
	acc, err := guest.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: token, Name: "Mehmet", Password: "mehmetin parolası"}))
	if err != nil || acc.Msg.GetSession().GetMemberships()[0].GetRole() != apiv1.Role_ROLE_MEMBER {
		t.Fatalf("accept: %v %v", acc, err)
	}
	if _, err := guest.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: token})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVITATION_INVALID {
		t.Fatalf("reused invitation: %v", err)
	}

	members, err := guest.org.ListMembers(ctx, req(&apiv1.ListMembersRequest{}))
	if err != nil || len(members.Msg.GetMembers()) != 2 || len(members.Msg.GetInvitations()) != 0 {
		t.Fatalf("member view of members: %v %v", members, err)
	}
	ownerID := ""
	for _, m := range members.Msg.GetMembers() {
		if m.GetRole() == apiv1.Role_ROLE_OWNER {
			ownerID = m.GetUser().GetId()
		}
	}
	if _, err := guest.org.UpdateMember(ctx, req(&apiv1.UpdateMemberRequest{UserId: ownerID, Role: apiv1.Role_ROLE_VIEWER})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("member changing roles: %v", err)
	}
	if _, err := owner.org.UpdateMember(ctx, req(&apiv1.UpdateMemberRequest{UserId: ownerID, Role: apiv1.Role_ROLE_ADMIN})); reason(err) != apiv1.ErrorReason_ERROR_REASON_LAST_OWNER {
		t.Fatalf("last owner demotion: %v", err)
	}

	// A second organization is invisible to members of the first.
	if _, err := owner.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: "Başka", Slug: "baska"})); err != nil {
		t.Fatalf("create org: %v", err)
	}
	other := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := other.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "mehmet@example.com", Password: "mehmetin parolası"})); err != nil {
		t.Fatal(err)
	}
	if _, err := other.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_ORGANIZATION_REQUIRED {
		t.Fatalf("cross-tenant access: %v", err)
	}
	noOrg := e.client(nil)
	noOrg.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "mehmet@example.com", Password: "mehmetin parolası"}))
	if _, err := noOrg.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_ORGANIZATION_REQUIRED {
		t.Fatalf("missing Izci-Org: %v", err)
	}

	usage, err := owner.org.GetUsage(ctx, req(&apiv1.GetUsageRequest{}))
	if err != nil || usage.Msg.GetMonth() == "" {
		t.Fatalf("usage: %v %v", usage, err)
	}
	audit, err := owner.org.ListAuditLog(ctx, req(&apiv1.ListAuditLogRequest{PageSize: 2}))
	if err != nil || len(audit.Msg.GetEntries()) != 2 || audit.Msg.GetNextPageToken() == "" {
		t.Fatalf("audit page: %v %v", audit, err)
	}
	next, err := owner.org.ListAuditLog(ctx, req(&apiv1.ListAuditLogRequest{PageSize: 10, PageToken: audit.Msg.GetNextPageToken()}))
	if err != nil || len(next.Msg.GetEntries()) == 0 || next.Msg.GetEntries()[0].GetId() == audit.Msg.GetEntries()[1].GetId() {
		t.Fatalf("audit next page: %v %v", next, err)
	}
}
