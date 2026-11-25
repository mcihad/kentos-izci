package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
)

// UserServer implements izci.api.v1.UserService.
type UserServer struct {
	*base
	apiv1connect.UnimplementedUserServiceHandler
	// TelegramLinkCode is provided by the Telegram integration when a bot is
	// configured.
	TelegramLinkCode func(ctx context.Context, userID uuid.UUID) (*apiv1.CreateTelegramAccountLinkCodeResponse, error)
}

var _ apiv1connect.UserServiceHandler = (*UserServer)(nil)

// UpdateProfile implements UserService.UpdateProfile.
func (s *UserServer) UpdateProfile(ctx context.Context, req *connect.Request[apiv1.UpdateProfileRequest]) (*connect.Response[apiv1.UpdateProfileResponse], error) {
	p := auth.PrincipalFrom(ctx)
	u, err := store.UpdateUserProfile(ctx, s.db.Pool, p.User.ID, req.Msg.GetName())
	if err != nil {
		return nil, internal(ctx, s.log, "update profile", err)
	}
	providers, _ := store.UserProviders(ctx, s.db.Pool, u.ID)
	return connect.NewResponse(&apiv1.UpdateProfileResponse{User: toUser(u, providers)}), nil
}

// ListAccessTokens implements UserService.ListAccessTokens.
func (s *UserServer) ListAccessTokens(ctx context.Context, _ *connect.Request[apiv1.ListAccessTokensRequest]) (*connect.Response[apiv1.ListAccessTokensResponse], error) {
	p := auth.PrincipalFrom(ctx)
	tokens, err := store.ListPersonalTokens(ctx, s.db.Pool, p.User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "list tokens", err)
	}
	out := &apiv1.ListAccessTokensResponse{}
	orgs := map[uuid.UUID]*store.Organization{}
	for _, t := range tokens {
		var org *store.Organization
		if t.OrgID != nil {
			if org = orgs[*t.OrgID]; org == nil {
				org, _ = store.GetOrganization(ctx, s.db.Pool, *t.OrgID)
				orgs[*t.OrgID] = org
			}
		}
		out.Tokens = append(out.Tokens, toAccessToken(t, org))
	}
	return connect.NewResponse(out), nil
}

// CreateAccessToken implements UserService.CreateAccessToken.
func (s *UserServer) CreateAccessToken(ctx context.Context, req *connect.Request[apiv1.CreateAccessTokenRequest]) (*connect.Response[apiv1.CreateAccessTokenResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if p.Token != nil {
		return nil, errPermission("access tokens cannot create other tokens")
	}
	if err := requireRecentAuth(ctx); err != nil {
		return nil, err
	}
	m := req.Msg
	var org *store.Organization
	var orgID *uuid.UUID
	if m.GetOrganization() != "" {
		t, err := s.auth.ResolveTenant(ctx, p, m.GetOrganization())
		if err != nil {
			return nil, tenantError(ctx, s.log, err)
		}
		org, orgID = t.Org, &t.Org.ID
	}
	var expires *time.Time
	if d := m.GetExpiresIn().AsDuration(); d > 0 {
		if d > 366*24*time.Hour {
			return nil, fieldError("expires_in", "tokens may live at most one year")
		}
		e := time.Now().Add(d)
		expires = &e
	}
	// A token bound to an organization lives no longer than the
	// organization accepts tokens.
	if org != nil {
		if limit := org.Settings.GetTokenMaxDays(); limit > 0 &&
			(expires == nil || m.GetExpiresIn().AsDuration() > time.Duration(limit)*24*time.Hour) {
			return nil, fieldError("expires_in", fmt.Sprintf("organization %s accepts access tokens for at most %d days", org.Slug, limit))
		}
	}
	for _, sc := range m.GetScopes() {
		if sc == apiv1.TokenScope_TOKEN_SCOPE_UNSPECIFIED {
			return nil, fieldError("scopes", "scope must be set")
		}
	}
	tok, secret, err := store.CreatePersonalToken(ctx, s.db.Pool, p.User.ID, orgID, m.GetName(), m.GetScopes(), expires)
	if err != nil {
		return nil, internal(ctx, s.log, "create token", err)
	}
	s.account.TokenCreated(ctx, p.User.Email, tok.Name, scopeNames(tok.Scopes), tok.ExpiresAt)
	return connect.NewResponse(&apiv1.CreateAccessTokenResponse{Token: toAccessToken(tok, org), Secret: secret}), nil
}

// RevokeAccessToken implements UserService.RevokeAccessToken.
func (s *UserServer) RevokeAccessToken(ctx context.Context, req *connect.Request[apiv1.RevokeAccessTokenRequest]) (*connect.Response[apiv1.RevokeAccessTokenResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	if err := store.RevokePersonalToken(ctx, s.db.Pool, auth.PrincipalFrom(ctx).User.ID, id); errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("token")
	} else if err != nil {
		return nil, internal(ctx, s.log, "revoke token", err)
	}
	return connect.NewResponse(&apiv1.RevokeAccessTokenResponse{}), nil
}

// ListSessions implements UserService.ListSessions.
func (s *UserServer) ListSessions(ctx context.Context, _ *connect.Request[apiv1.ListSessionsRequest]) (*connect.Response[apiv1.ListSessionsResponse], error) {
	p := auth.PrincipalFrom(ctx)
	sessions, err := store.ListSessions(ctx, s.db.Pool, p.User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "list sessions", err)
	}
	out := &apiv1.ListSessionsResponse{}
	for _, sess := range sessions {
		out.Sessions = append(out.Sessions, toSessionInfo(sess, p.Session != nil && sess.ID == p.Session.ID))
	}
	return connect.NewResponse(out), nil
}

// RevokeSession implements UserService.RevokeSession.
func (s *UserServer) RevokeSession(ctx context.Context, req *connect.Request[apiv1.RevokeSessionRequest]) (*connect.Response[apiv1.RevokeSessionResponse], error) {
	p := auth.PrincipalFrom(ctx)
	var n int64
	var err error
	if req.Msg.GetAllOthers() {
		keep := uuid.Nil
		if p.Session != nil {
			keep = p.Session.ID
		}
		n, err = store.DeleteOtherSessions(ctx, s.db.Pool, p.User.ID, keep)
	} else {
		id, perr := uuid.Parse(req.Msg.GetId())
		if perr != nil {
			return nil, fieldError("id", "invalid session id")
		}
		n, err = store.DeleteSession(ctx, s.db.Pool, p.User.ID, id)
	}
	if err != nil {
		return nil, internal(ctx, s.log, "revoke session", err)
	}
	return connect.NewResponse(&apiv1.RevokeSessionResponse{Revoked: uint32(n)}), nil
}

// CreateTelegramAccountLinkCode implements UserService.CreateTelegramAccountLinkCode.
func (s *UserServer) CreateTelegramAccountLinkCode(ctx context.Context, _ *connect.Request[apiv1.CreateTelegramAccountLinkCodeRequest]) (*connect.Response[apiv1.CreateTelegramAccountLinkCodeResponse], error) {
	if s.TelegramLinkCode == nil {
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_TELEGRAM_NOT_CONFIGURED, "Telegram is not configured")
	}
	out, err := s.TelegramLinkCode(ctx, auth.PrincipalFrom(ctx).User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "telegram link code", err)
	}
	return connect.NewResponse(out), nil
}

// UnlinkTelegramAccount implements UserService.UnlinkTelegramAccount.
func (s *UserServer) UnlinkTelegramAccount(ctx context.Context, _ *connect.Request[apiv1.UnlinkTelegramAccountRequest]) (*connect.Response[apiv1.UnlinkTelegramAccountResponse], error) {
	_, err := s.db.Pool.Exec(ctx, `UPDATE users SET telegram_user_id = NULL, telegram_username = '', telegram_linked_at = NULL WHERE id = $1`,
		auth.PrincipalFrom(ctx).User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "unlink telegram", err)
	}
	return connect.NewResponse(&apiv1.UnlinkTelegramAccountResponse{}), nil
}

// scopeNames lists token scopes as people read them (Turkish).
func scopeNames(scopes []apiv1.TokenScope) string {
	names := map[apiv1.TokenScope]string{apiv1.TokenScope_TOKEN_SCOPE_READ: "okuma", apiv1.TokenScope_TOKEN_SCOPE_WRITE: "yazma",
		apiv1.TokenScope_TOKEN_SCOPE_AI: "yapay zekâ"}
	var out []string
	for _, s := range scopes {
		if n, ok := names[s]; ok {
			out = append(out, n)
		}
	}
	return strings.Join(out, " + ")
}
