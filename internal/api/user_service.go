package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	meta := map[string]string{"scopes": scopeNames(tok.Scopes)}
	if tok.ExpiresAt != nil {
		meta["expires_at"] = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	s.auth.AuditUser(ctx, p.User.ID, store.AuditEntry{ActorID: &p.User.ID, Action: "user.access_token.create", TargetType: "access_token",
		TargetID: tok.ID.String(), TargetName: tok.Name, Metadata: meta, IP: infoFrom(ctx).IP})
	return connect.NewResponse(&apiv1.CreateAccessTokenResponse{Token: toAccessToken(tok, org), Secret: secret}), nil
}

// RevokeAccessToken implements UserService.RevokeAccessToken.
func (s *UserServer) RevokeAccessToken(ctx context.Context, req *connect.Request[apiv1.RevokeAccessTokenRequest]) (*connect.Response[apiv1.RevokeAccessTokenResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	me := auth.PrincipalFrom(ctx).User.ID
	if err := store.RevokePersonalToken(ctx, s.db.Pool, me, id); errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("token")
	} else if err != nil {
		return nil, internal(ctx, s.log, "revoke token", err)
	}
	s.auth.AuditUser(ctx, me, store.AuditEntry{ActorID: &me, Action: "user.access_token.revoke", TargetType: "access_token",
		TargetID: id.String(), IP: infoFrom(ctx).IP})
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
	if n > 0 {
		s.auth.Audit(ctx, store.AuditEntry{ActorID: &p.User.ID, Action: "user.session.revoke", TargetType: "user", TargetID: p.User.ID.String(),
			TargetName: p.User.Email, Metadata: map[string]string{"sessions": strconv.FormatInt(n, 10)}, IP: infoFrom(ctx).IP})
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

// deleteUser deletes an account once nothing depends on it: organizations
// it is the only owner of, or the server's last administrator. Each
// organization's audit log records the departure.
func (b *base) deleteUser(ctx context.Context, u *store.User) error {
	return b.db.InSystem(ctx, func(tx pgx.Tx) error {
		owned, err := store.SoleOwnedOrganizations(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if len(owned) > 0 {
			slugs := make([]string, len(owned))
			for i, o := range owned {
				slugs[i] = o.Slug
			}
			return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_LAST_OWNER,
				"hand over or delete the organizations this account is the only owner of first",
				"organizations", strings.Join(slugs, ","))
		}
		if u.Superadmin && !u.Disabled {
			n, err := store.CountSuperadmins(ctx, tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_LAST_SUPERADMIN,
					"make someone else a server administrator first")
			}
		}
		memberships, err := store.ListMemberships(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		actor := auth.PrincipalFrom(ctx).User.ID
		for _, m := range memberships {
			if err := store.InsertAudit(ctx, tx, store.AuditEntry{OrgID: &m.Organization.ID, ActorID: &actor, Action: "user.delete",
				TargetType: "user", TargetID: u.ID.String(), TargetName: u.Email, IP: infoFrom(ctx).IP}); err != nil {
				return err
			}
		}
		return store.DeleteUser(ctx, tx, u.ID)
	})
}

// DeleteAccount implements UserService.DeleteAccount.
func (s *UserServer) DeleteAccount(ctx context.Context, req *connect.Request[apiv1.DeleteAccountRequest]) (*connect.Response[apiv1.DeleteAccountResponse], error) {
	u := auth.PrincipalFrom(ctx).User
	if !strings.EqualFold(strings.TrimSpace(req.Msg.GetConfirmEmail()), u.Email) {
		return nil, fieldError("confirm_email", "confirmation does not match the account's e-mail address")
	}
	if err := requireRecentAuth(ctx); err != nil {
		return nil, err
	}
	if err := s.deleteUser(ctx, u); err != nil {
		return nil, internal(ctx, s.log, "delete account", err)
	}
	s.auth.Audit(ctx, store.AuditEntry{ActorID: &u.ID, Action: "user.delete", TargetType: "user", TargetID: u.ID.String(),
		TargetName: u.Email, IP: infoFrom(ctx).IP})
	s.account.AccountDeleted(ctx, u.Email, false, time.Now())
	s.log.InfoContext(ctx, "account deleted", slog.String("user_id", u.ID.String()))
	resp := connect.NewResponse(&apiv1.DeleteAccountResponse{})
	resp.Header().Add("Set-Cookie", s.auth.ClearCookie().String())
	return resp, nil
}
