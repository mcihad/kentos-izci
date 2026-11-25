package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/accountmail"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/alert"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/inbox"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/realtime"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/version"
)

// base holds dependencies shared by all service implementations.
type base struct {
	db       *db.DB
	auth     *auth.Service
	cfg      *config.Config
	log      *slog.Logger
	hub      *realtime.Hub
	pipeline *ingest.Pipeline
	keys     *ingest.KeyResolver
	done     <-chan struct{}
	notifier Notifier
	alerts   *alert.Engine
	ai       *ai.Runner
	github   *github.Access
	delivery *notify.Dispatcher
	jira     *notify.Jira
	email    *notify.Email
	mailer   Mailer
	inbox    *inbox.Service
	// account sends invitations, password resets and security notices.
	account *accountmail.Sender
}

// AuthServer implements izci.api.v1.AuthService.
type AuthServer struct {
	*base
	apiv1connect.UnimplementedAuthServiceHandler
}

var _ apiv1connect.AuthServiceHandler = (*AuthServer)(nil)

// GetSetupState implements AuthService.GetSetupState.
func (s *AuthServer) GetSetupState(ctx context.Context, _ *connect.Request[apiv1.GetSetupStateRequest]) (*connect.Response[apiv1.GetSetupStateResponse], error) {
	required, err := s.auth.SetupRequired(ctx)
	if err != nil {
		return nil, internal(ctx, s.log, "setup state", err)
	}
	return connect.NewResponse(&apiv1.GetSetupStateResponse{SetupRequired: required, Version: version.Get().Version, PublicUrl: s.cfg.PublicURL}), nil
}

// CompleteSetup implements AuthService.CompleteSetup.
func (s *AuthServer) CompleteSetup(ctx context.Context, req *connect.Request[apiv1.CompleteSetupRequest]) (*connect.Response[apiv1.CompleteSetupResponse], error) {
	m := req.Msg
	// Cheap checks first: password hashing is expensive and must not run
	// for requests that cannot succeed.
	required, err := s.auth.SetupRequired(ctx)
	if err != nil {
		return nil, internal(ctx, s.log, "setup state", err)
	}
	if !required {
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_SETUP_COMPLETED, "setup was already completed")
	}
	if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	if !s.auth.SetupTokenValid(m.GetSetupToken()) {
		return nil, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_SETUP_TOKEN_INVALID,
			"the setup code is wrong; izci prints it in its log while no user exists")
	}
	if err := auth.ValidatePassword(m.GetPassword()); err != nil {
		return nil, weakPassword()
	}
	hash, err := auth.HashPassword(m.GetPassword())
	if err != nil {
		return nil, internal(ctx, s.log, "hash password", err)
	}
	projectSlug := slug.Make(m.GetProjectName(), 50)
	if projectSlug == "" {
		projectSlug = "app"
	}
	var user *store.User
	var apiKey string
	err = s.db.InSystem(ctx, func(tx pgx.Tx) error {
		// Serialize concurrent setup attempts, then re-check.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('izci-setup'))`); err != nil {
			return err
		}
		n, err := store.CountUsers(ctx, tx)
		if err != nil {
			return err
		}
		if n > 0 {
			return errSetupDone
		}
		if user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: strings.ToLower(m.GetEmail()), Name: m.GetName(), PasswordHash: &hash, Superadmin: true}); err != nil {
			return err
		}
		org, err := store.CreateOrganization(ctx, tx, m.GetOrganizationSlug(), m.GetOrganizationName())
		if err != nil {
			return err
		}
		if err := store.AddMember(ctx, tx, org.ID, user.ID, apiv1.Role_ROLE_OWNER); err != nil {
			return err
		}
		project, err := store.CreateProject(ctx, tx, org.ID, projectSlug, m.GetProjectName(), strings.ToLower(m.GetProjectPlatform()))
		if err != nil {
			return err
		}
		_, apiKey, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: org.ID, ProjectID: project.ID, Name: "default",
			Scopes:    []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST},
			CreatedBy: &user.ID})
		if err != nil {
			return err
		}
		orgID := org.ID
		return store.InsertAudit(ctx, tx, store.AuditEntry{OrgID: &orgID, ActorID: &user.ID, Action: "instance.setup",
			TargetType: "organization", TargetID: org.ID.String(), TargetName: org.Name, IP: infoFrom(ctx).IP})
	})
	if errors.Is(err, errSetupDone) {
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_SETUP_COMPLETED, "setup was already completed")
	}
	if err != nil {
		return nil, internal(ctx, s.log, "complete setup", err)
	}
	resp := connect.NewResponse(&apiv1.CompleteSetupResponse{ProjectSlug: projectSlug, ApiKey: apiKey})
	if resp.Msg.Session, err = s.startSession(ctx, resp.Header(), user, "setup"); err != nil {
		return nil, internal(ctx, s.log, "start session", err)
	}
	return resp, nil
}

var errSetupDone = errors.New("setup already done")

func weakPassword() error {
	return apiError(connect.CodeInvalidArgument, apiv1.ErrorReason_ERROR_REASON_WEAK_PASSWORD,
		"password is too weak", "min_length", strconv.Itoa(auth.MinPasswordLength))
}

// ListAuthProviders implements AuthService.ListAuthProviders.
func (s *AuthServer) ListAuthProviders(context.Context, *connect.Request[apiv1.ListAuthProvidersRequest]) (*connect.Response[apiv1.ListAuthProvidersResponse], error) {
	out := &apiv1.ListAuthProvidersResponse{PasswordLoginEnabled: s.cfg.Auth.PasswordLogin, SignupMode: signupMode(s.cfg.Auth.Signup),
		PasswordResetAvailable: s.cfg.Auth.PasswordLogin && s.account.Available()}
	for _, p := range s.auth.Providers() {
		out.Providers = append(out.Providers, &apiv1.AuthProvider{Id: p.ID, DisplayName: p.Name, Icon: p.Icon, LoginUrl: p.LoginURL})
	}
	return connect.NewResponse(out), nil
}

func signupMode(s string) apiv1.SignupMode {
	switch s {
	case "open":
		return apiv1.SignupMode_SIGNUP_MODE_OPEN
	case "closed":
		return apiv1.SignupMode_SIGNUP_MODE_CLOSED
	}
	return apiv1.SignupMode_SIGNUP_MODE_INVITE
}

// Login implements AuthService.Login.
func (s *AuthServer) Login(ctx context.Context, req *connect.Request[apiv1.LoginRequest]) (*connect.Response[apiv1.LoginResponse], error) {
	u, err := s.auth.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword(), infoFrom(ctx).IP)
	var tooMany *auth.TooManyAttemptsError
	if reason := loginFailure(err); reason != "" {
		email := strings.ToLower(strings.TrimSpace(req.Msg.GetEmail()))
		e := store.AuditEntry{Action: "auth.login_failed", TargetType: "user", TargetName: email,
			Metadata: map[string]string{"reason": reason}, IP: infoFrom(ctx).IP}
		// Attempts on an existing account are attributed to it.
		if known, err := store.GetUserByEmail(ctx, s.db.Pool, email); err == nil {
			e.ActorID, e.TargetID = &known.ID, known.ID.String()
		}
		s.auth.Audit(ctx, e)
	}
	switch {
	case errors.As(err, &tooMany):
		return nil, tooManyAttempts(tooMany)
	case errors.Is(err, auth.ErrInvalidCredentials):
		return nil, apiError(connect.CodeUnauthenticated, apiv1.ErrorReason_ERROR_REASON_INVALID_CREDENTIALS, err.Error())
	case errors.Is(err, auth.ErrAccountDisabled):
		return nil, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_ACCOUNT_DISABLED, err.Error())
	case errors.Is(err, auth.ErrPasswordLoginOff):
		return nil, errPermission(err.Error())
	case err != nil:
		return nil, internal(ctx, s.log, "login", err)
	}
	resp := connect.NewResponse(&apiv1.LoginResponse{})
	if u.TwoFactor() {
		cookie, err := s.auth.BeginChallenge(ctx, u.ID)
		if err != nil {
			return nil, internal(ctx, s.log, "begin two-factor sign-in", err)
		}
		resp.Header().Add("Set-Cookie", cookie.String())
		resp.Msg.TwoFactorRequired = true
		return resp, nil
	}
	if resp.Msg.Session, err = s.startSession(ctx, resp.Header(), u, "password"); err != nil {
		return nil, internal(ctx, s.log, "start session", err)
	}
	return resp, nil
}

// loginFailure names why a sign-in failed, for the audit log; "" when it
// did not, or failed for reasons that are not the user's.
func loginFailure(err error) string {
	var tooMany *auth.TooManyAttemptsError
	switch {
	case errors.As(err, &tooMany):
		return "too_many_attempts"
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "invalid_credentials"
	case errors.Is(err, auth.ErrAccountDisabled):
		return "account_disabled"
	}
	return ""
}

// startSession creates a session, sets its cookie on the response and
// returns the session message; method says how the user signed in
// ("password", "two_factor", "setup", "signup", "invitation").
func (s *AuthServer) startSession(ctx context.Context, h http.Header, u *store.User, method string) (*apiv1.Session, error) {
	info := infoFrom(ctx)
	cookie, sess, err := s.auth.StartSession(ctx, u.ID, info.IP, info.UserAgent, method)
	if err != nil {
		return nil, err
	}
	h.Add("Set-Cookie", cookie.String())
	return s.session(ctx, u, sess)
}

func (s *AuthServer) session(ctx context.Context, u *store.User, sess *store.Session) (*apiv1.Session, error) {
	providers, err := store.UserProviders(ctx, s.db.Pool, u.ID)
	if err != nil {
		return nil, err
	}
	memberships, err := store.ListMemberships(ctx, s.db.Pool, u.ID)
	if err != nil {
		return nil, err
	}
	out := &apiv1.Session{User: toUser(u, providers), SuperadminNeedsTwoFactor: s.auth.SuperadminNeedsTwoFactor(u)}
	if sess != nil {
		out.ReauthenticatedAt = ts(sess.ReauthenticatedAt)
	}
	if u.TwoFactor() {
		left, err := store.RecoveryCodesLeft(ctx, s.db.Pool, u.ID)
		if err != nil {
			return nil, err
		}
		out.User.RecoveryCodesLeft = uint32(left)
	}
	if sess != nil {
		out.ExpiresAt = ts(sess.ExpiresAt)
	}
	for _, m := range memberships {
		out.Memberships = append(out.Memberships, &apiv1.Membership{Organization: toOrgRef(m.Organization), Role: m.Role,
			RequiresTwoFactor: m.Organization.Settings.GetRequireTwoFactor(), TokenMaxDays: m.Organization.Settings.GetTokenMaxDays()})
	}
	return out, nil
}

// Logout implements AuthService.Logout.
func (s *AuthServer) Logout(ctx context.Context, _ *connect.Request[apiv1.LogoutRequest]) (*connect.Response[apiv1.LogoutResponse], error) {
	if err := s.auth.EndSession(ctx, auth.PrincipalFrom(ctx)); err != nil {
		return nil, internal(ctx, s.log, "logout", err)
	}
	if p := auth.PrincipalFrom(ctx); p != nil && p.Session != nil {
		s.auth.Audit(ctx, store.AuditEntry{ActorID: &p.User.ID, Action: "auth.logout", TargetType: "user", TargetID: p.User.ID.String(),
			TargetName: p.User.Email, IP: infoFrom(ctx).IP})
	}
	resp := connect.NewResponse(&apiv1.LogoutResponse{})
	resp.Header().Add("Set-Cookie", s.auth.ClearCookie().String())
	return resp, nil
}

// GetSession implements AuthService.GetSession.
func (s *AuthServer) GetSession(ctx context.Context, _ *connect.Request[apiv1.GetSessionRequest]) (*connect.Response[apiv1.GetSessionResponse], error) {
	p := auth.PrincipalFrom(ctx)
	sess, err := s.session(ctx, p.User, p.Session)
	if err != nil {
		return nil, internal(ctx, s.log, "get session", err)
	}
	return connect.NewResponse(&apiv1.GetSessionResponse{Session: sess}), nil
}

// ChangePassword implements AuthService.ChangePassword.
func (s *AuthServer) ChangePassword(ctx context.Context, req *connect.Request[apiv1.ChangePasswordRequest]) (*connect.Response[apiv1.ChangePasswordResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if p.Session == nil {
		return nil, errPermission("passwords are changed in a browser session")
	}
	if p.User.PasswordHash != nil {
		ok, err := auth.VerifyPassword(*p.User.PasswordHash, req.Msg.GetCurrentPassword())
		if err != nil || !ok {
			return nil, apiError(connect.CodeInvalidArgument, apiv1.ErrorReason_ERROR_REASON_INVALID_CREDENTIALS, "current password is wrong")
		}
	} else if err := requireRecentAuth(ctx); err != nil {
		// A first password adds a way to sign in.
		return nil, err
	}
	if err := auth.ValidatePassword(req.Msg.GetNewPassword()); err != nil {
		return nil, weakPassword()
	}
	hash, err := auth.HashPassword(req.Msg.GetNewPassword())
	if err != nil {
		return nil, internal(ctx, s.log, "hash password", err)
	}
	if err := store.UpdateUserPassword(ctx, s.db.Pool, p.User.ID, hash); err != nil {
		return nil, internal(ctx, s.log, "change password", err)
	}
	keep := uuid.Nil
	if p.Session != nil {
		keep = p.Session.ID
	}
	if _, err := store.DeleteOtherSessions(ctx, s.db.Pool, p.User.ID, keep); err != nil {
		return nil, internal(ctx, s.log, "end sessions", err)
	}
	s.account.PasswordChanged(ctx, p.User.Email, time.Now())
	s.auth.AuditUser(ctx, p.User.ID, store.AuditEntry{ActorID: &p.User.ID, Action: "user.password.change", TargetType: "user",
		TargetID: p.User.ID.String(), TargetName: p.User.Email, IP: infoFrom(ctx).IP})
	return connect.NewResponse(&apiv1.ChangePasswordResponse{}), nil
}

// GetInvitation implements AuthService.GetInvitation.
func (s *AuthServer) GetInvitation(ctx context.Context, req *connect.Request[apiv1.GetInvitationRequest]) (*connect.Response[apiv1.GetInvitationResponse], error) {
	var out apiv1.GetInvitationResponse
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		inv, err := store.InvitationByToken(ctx, tx, req.Msg.GetToken())
		if err != nil {
			return err
		}
		org, err := store.GetOrganization(ctx, tx, inv.OrgID)
		if err != nil {
			return err
		}
		var inviter *store.User
		if inv.InvitedBy != nil {
			inviter, _ = store.GetUser(ctx, tx, *inv.InvitedBy)
		}
		out.Invitation = toInvitation(inv, org, inviter, "")
		_, err = store.GetUserByEmail(ctx, tx, inv.Email)
		out.AccountExists = err == nil
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, invitationInvalid()
	}
	if err != nil {
		return nil, internal(ctx, s.log, "get invitation", err)
	}
	return connect.NewResponse(&out), nil
}

func invitationInvalid() error {
	return apiError(connect.CodeNotFound, apiv1.ErrorReason_ERROR_REASON_INVITATION_INVALID, "invitation is invalid or expired")
}

// AcceptInvitation implements AuthService.AcceptInvitation.
func (s *AuthServer) AcceptInvitation(ctx context.Context, req *connect.Request[apiv1.AcceptInvitationRequest]) (*connect.Response[apiv1.AcceptInvitationResponse], error) {
	p := auth.PrincipalFrom(ctx)
	var user *store.User
	var created bool
	var hash string
	if p == nil {
		if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
			return nil, throttled(err)
		}
		// A new account: check the invitation before the expensive
		// password hash.
		err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
			inv, err := store.InvitationByToken(ctx, tx, req.Msg.GetToken())
			if err != nil {
				return err
			}
			if _, err := store.GetUserByEmail(ctx, tx, inv.Email); err == nil {
				return errSignInFirst
			}
			return nil
		})
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, invitationInvalid()
		case errors.Is(err, errSignInFirst):
			return nil, errUnauthenticated()
		case err != nil:
			return nil, internal(ctx, s.log, "accept invitation", err)
		}
		if err := auth.ValidatePassword(req.Msg.GetPassword()); err != nil {
			return nil, weakPassword()
		}
		if hash, err = auth.HashPassword(req.Msg.GetPassword()); err != nil {
			return nil, internal(ctx, s.log, "hash password", err)
		}
	}
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		inv, err := store.InvitationByToken(ctx, tx, req.Msg.GetToken())
		if err != nil {
			return err
		}
		if p != nil {
			if !strings.EqualFold(p.User.Email, inv.Email) {
				return errWrongAccount
			}
			user = p.User
		} else {
			if _, err := store.GetUserByEmail(ctx, tx, inv.Email); err == nil {
				return errSignInFirst
			}
			name := strings.TrimSpace(req.Msg.GetName())
			if name == "" {
				name = strings.Split(inv.Email, "@")[0]
			}
			if user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: strings.ToLower(inv.Email), Name: name, PasswordHash: &hash}); err != nil {
				return err
			}
			created = true
		}
		if _, err := store.MemberRole(ctx, tx, inv.OrgID, user.ID); errors.Is(err, store.ErrNotFound) {
			if err := store.AddMember(ctx, tx, inv.OrgID, user.ID, inv.Role); err != nil {
				return err
			}
		}
		orgID := inv.OrgID
		if err := store.InsertAudit(ctx, tx, store.AuditEntry{OrgID: &orgID, ActorID: &user.ID, Action: "member.join",
			TargetType: "user", TargetID: user.ID.String(), TargetName: user.Email, IP: infoFrom(ctx).IP}); err != nil {
			return err
		}
		return store.MarkInvitationAccepted(ctx, tx, inv.ID, user.ID)
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, invitationInvalid()
	case errors.Is(err, errWrongAccount):
		return nil, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_INVITATION_INVALID, "invitation was sent to another email address")
	case errors.Is(err, errSignInFirst):
		return nil, errUnauthenticated()
	case err != nil:
		return nil, internal(ctx, s.log, "accept invitation", err)
	}
	resp := connect.NewResponse(&apiv1.AcceptInvitationResponse{})
	if created {
		resp.Msg.Session, err = s.startSession(ctx, resp.Header(), user, "invitation")
	} else {
		resp.Msg.Session, err = s.session(ctx, user, p.Session)
	}
	if err != nil {
		return nil, internal(ctx, s.log, "session", err)
	}
	return resp, nil
}

var (
	errWrongAccount = errors.New("invitation email mismatch")
	errSignInFirst  = errors.New("sign in first")
)

// throttled maps a TooManyAttemptsError.
func throttled(err error) error {
	var tooMany *auth.TooManyAttemptsError
	if errors.As(err, &tooMany) {
		return tooManyAttempts(tooMany)
	}
	return err
}
