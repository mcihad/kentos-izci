package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
)

// tenantError maps the errors of auth.ResolveTenant.
func tenantError(ctx context.Context, log *slog.Logger, err error) error {
	switch {
	case errors.Is(err, auth.ErrOrgNotFound):
		return apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_ORGANIZATION_REQUIRED, "not a member of this organization")
	case errors.Is(err, auth.ErrTwoFactorRequired):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED,
			"the organization requires two-factor authentication; turn it on for your account")
	}
	return internal(ctx, log, "resolve organization", err)
}

func tooManyAttempts(e *auth.TooManyAttemptsError) error {
	return apiError(connect.CodeResourceExhausted, apiv1.ErrorReason_ERROR_REASON_TOO_MANY_ATTEMPTS, e.Error(),
		"retry_after_seconds", strconv.Itoa(int(e.RetryAfter.Seconds())+1))
}

// secondFactorError maps the two-factor errors of the auth service. Wrong
// codes are INVALID_ARGUMENT, not UNAUTHENTICATED: the caller's session is
// fine.
func secondFactorError(ctx context.Context, log *slog.Logger, op string, err error) error {
	var tooMany *auth.TooManyAttemptsError
	switch {
	case errors.As(err, &tooMany):
		return tooManyAttempts(tooMany)
	case errors.Is(err, auth.ErrInvalidSecondFactor):
		return apiError(connect.CodeInvalidArgument, apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE, err.Error())
	case errors.Is(err, auth.ErrTwoFactorSetupExpired), errors.Is(err, auth.ErrChallengeExpired):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_EXPIRED, err.Error())
	case errors.Is(err, auth.ErrTwoFactorOn), errors.Is(err, auth.ErrTwoFactorOff):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_CONFLICT, err.Error())
	case errors.Is(err, auth.ErrAccountDisabled):
		return apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_ACCOUNT_DISABLED, err.Error())
	}
	return internal(ctx, log, op, err)
}

// VerifyTwoFactor implements AuthService.VerifyTwoFactor.
func (s *AuthServer) VerifyTwoFactor(ctx context.Context, req *connect.Request[apiv1.VerifyTwoFactorRequest]) (*connect.Response[apiv1.VerifyTwoFactorResponse], error) {
	if err := s.auth.ThrottlePublic(infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	u, err := s.auth.CompleteChallenge(ctx, req.Header(), req.Msg.GetCode())
	if err != nil {
		return nil, secondFactorError(ctx, s.log, "verify two-factor", err)
	}
	resp := connect.NewResponse(&apiv1.VerifyTwoFactorResponse{})
	resp.Header().Add("Set-Cookie", s.auth.ClearChallengeCookie().String())
	if resp.Msg.Session, err = s.startSession(ctx, resp.Header(), u); err != nil {
		return nil, internal(ctx, s.log, "start session", err)
	}
	return resp, nil
}

// sessionOnly refuses access tokens: two-factor settings change only in a
// browser session.
func sessionOnly(p *auth.Principal) error {
	if p.Session == nil {
		return errPermission("two-factor authentication is managed in a browser session")
	}
	return nil
}

// BeginTwoFactorSetup implements UserService.BeginTwoFactorSetup.
func (s *UserServer) BeginTwoFactorSetup(ctx context.Context, _ *connect.Request[apiv1.BeginTwoFactorSetupRequest]) (*connect.Response[apiv1.BeginTwoFactorSetupResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if err := sessionOnly(p); err != nil {
		return nil, err
	}
	setup, err := s.auth.BeginTOTP(ctx, p.User)
	if err != nil {
		return nil, secondFactorError(ctx, s.log, "begin two-factor setup", err)
	}
	return connect.NewResponse(&apiv1.BeginTwoFactorSetupResponse{Secret: setup.Secret, Uri: setup.URI}), nil
}

// EnableTwoFactor implements UserService.EnableTwoFactor.
func (s *UserServer) EnableTwoFactor(ctx context.Context, req *connect.Request[apiv1.EnableTwoFactorRequest]) (*connect.Response[apiv1.EnableTwoFactorResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if err := sessionOnly(p); err != nil {
		return nil, err
	}
	codes, err := s.auth.EnableTOTP(ctx, p.User.ID, req.Msg.GetCode())
	if err != nil {
		return nil, secondFactorError(ctx, s.log, "enable two-factor", err)
	}
	s.userAudit(ctx, p.User, "user.two_factor.enable")
	return connect.NewResponse(&apiv1.EnableTwoFactorResponse{RecoveryCodes: codes}), nil
}

// DisableTwoFactor implements UserService.DisableTwoFactor.
func (s *UserServer) DisableTwoFactor(ctx context.Context, req *connect.Request[apiv1.DisableTwoFactorRequest]) (*connect.Response[apiv1.DisableTwoFactorResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if err := sessionOnly(p); err != nil {
		return nil, err
	}
	if err := s.auth.DisableTOTP(ctx, p.User.ID, req.Msg.GetCode()); err != nil {
		return nil, secondFactorError(ctx, s.log, "disable two-factor", err)
	}
	s.userAudit(ctx, p.User, "user.two_factor.disable")
	return connect.NewResponse(&apiv1.DisableTwoFactorResponse{}), nil
}

// RegenerateRecoveryCodes implements UserService.RegenerateRecoveryCodes.
func (s *UserServer) RegenerateRecoveryCodes(ctx context.Context, req *connect.Request[apiv1.RegenerateRecoveryCodesRequest]) (*connect.Response[apiv1.RegenerateRecoveryCodesResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if err := sessionOnly(p); err != nil {
		return nil, err
	}
	codes, err := s.auth.RegenerateRecoveryCodes(ctx, p.User.ID, req.Msg.GetCode())
	if err != nil {
		return nil, secondFactorError(ctx, s.log, "regenerate recovery codes", err)
	}
	s.userAudit(ctx, p.User, "user.two_factor.recovery_codes")
	return connect.NewResponse(&apiv1.RegenerateRecoveryCodesResponse{RecoveryCodes: codes}), nil
}

// userAudit records a change to the user's own account security in the
// audit log of every organization they belong to, so that administrators
// see it. Failures are logged, not returned: the change is done.
func (s *UserServer) userAudit(ctx context.Context, u *store.User, action string) {
	memberships, err := store.ListMemberships(ctx, s.db.Pool, u.ID)
	for _, m := range memberships {
		orgID := m.Organization.ID
		err = s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
			return store.InsertAudit(ctx, tx, store.AuditEntry{OrgID: &orgID, ActorID: &u.ID, Action: action,
				TargetType: "user", TargetID: u.ID.String(), TargetName: u.Email, IP: infoFrom(ctx).IP})
		})
		if err != nil {
			break
		}
	}
	if err != nil {
		s.log.WarnContext(ctx, "audit two-factor change", slog.String("action", action), slog.Any("err", err))
	}
}
