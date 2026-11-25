package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Mailer sends account e-mails: the links that confirm self sign-ups.
// notify.Email implements it.
type Mailer interface {
	SendMail(ctx context.Context, to, subject, text, htmlBody string) error
}

// signupLinkTTL is how long a sign-up link works.
const signupLinkTTL = 24 * time.Hour

// SignUp implements AuthService.SignUp. The account is created only when
// the link e-mailed to the address is opened (VerifySignup): otherwise
// anyone could hold an account under someone else's address and share it
// with its owner once they sign in through an identity provider.
func (s *AuthServer) SignUp(ctx context.Context, req *connect.Request[apiv1.SignUpRequest]) (*connect.Response[apiv1.SignUpResponse], error) {
	if s.cfg.Auth.Signup != "open" {
		return nil, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_SIGNUP_DISABLED, "self sign-up is disabled")
	}
	if s.mailer == nil && s.cfg.IsProduction() {
		// Configuration validation rules this out.
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_SIGNUP_DISABLED,
			"self sign-up needs an SMTP server to confirm e-mail addresses")
	}
	if err := s.setupDone(ctx); err != nil {
		return nil, err
	}
	if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	m := req.Msg
	email := strings.ToLower(strings.TrimSpace(m.GetEmail()))
	if err := auth.ValidatePassword(m.GetPassword()); err != nil {
		return nil, weakPassword()
	}
	// Early answers for a taken address or slug; VerifySignup checks again.
	var taken apiv1.ErrorReason
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		switch _, err := store.GetUserByEmail(ctx, tx, email); {
		case err == nil:
			taken = apiv1.ErrorReason_ERROR_REASON_EMAIL_TAKEN
			return nil
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
		switch _, err := store.GetOrganizationBySlug(ctx, tx, m.GetOrganizationSlug()); {
		case err == nil:
			taken = apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, internal(ctx, s.log, "sign up", err)
	case taken == apiv1.ErrorReason_ERROR_REASON_EMAIL_TAKEN:
		return nil, apiError(connect.CodeAlreadyExists, taken, "email already registered")
	case taken == apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN:
		return nil, apiError(connect.CodeAlreadyExists, taken, "organization slug is taken")
	}
	if err := s.auth.ThrottleSignup(ctx, email); err != nil {
		return nil, throttled(err)
	}
	hash, err := auth.HashPassword(m.GetPassword())
	if err != nil {
		return nil, internal(ctx, s.log, "hash password", err)
	}
	token, err := store.CreatePendingSignup(ctx, s.db.Pool, store.PendingSignup{
		Email: email, Name: strings.TrimSpace(m.GetName()), PasswordHash: hash,
		OrganizationName: strings.TrimSpace(m.GetOrganizationName()), OrganizationSlug: m.GetOrganizationSlug(),
	}, signupLinkTTL)
	if err != nil {
		return nil, internal(ctx, s.log, "store sign-up", err)
	}
	if err := s.sendSignupLink(ctx, email, s.cfg.PublicURL+"/verify/"+token); err != nil {
		s.log.WarnContext(ctx, "sign-up link not sent", slog.Any("err", err))
		return nil, apiError(connect.CodeUnavailable, apiv1.ErrorReason_ERROR_REASON_EMAIL_DELIVERY_FAILED,
			"the confirmation e-mail could not be sent; try again later")
	}
	return connect.NewResponse(&apiv1.SignUpResponse{VerificationSent: true}), nil
}

// VerifySignup implements AuthService.VerifySignup.
func (s *AuthServer) VerifySignup(ctx context.Context, req *connect.Request[apiv1.VerifySignupRequest]) (*connect.Response[apiv1.VerifySignupResponse], error) {
	if s.cfg.Auth.Signup != "open" {
		return nil, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_SIGNUP_DISABLED, "self sign-up is disabled")
	}
	if err := s.setupDone(ctx); err != nil {
		return nil, err
	}
	if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	var user *store.User
	// A failure rolls everything back, the sign-up included: the link
	// keeps working.
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		p, err := store.TakePendingSignup(ctx, tx, req.Msg.GetToken())
		if err != nil {
			return err
		}
		if user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: p.Email, Name: p.Name, PasswordHash: &p.PasswordHash}); err != nil {
			return err
		}
		slug, err := freeSlug(ctx, tx, p.OrganizationSlug)
		if err != nil {
			return err
		}
		org, err := store.CreateOrganization(ctx, tx, slug, p.OrganizationName)
		if err != nil {
			return err
		}
		return store.AddMember(ctx, tx, org.ID, user.ID, apiv1.Role_ROLE_OWNER)
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, apiError(connect.CodeNotFound, apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID,
			"the sign-up link is unknown, used or expired; sign up again")
	case store.UniqueViolation(err, "users_email_key"):
		return nil, apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_EMAIL_TAKEN, "email already registered")
	case err != nil:
		return nil, internal(ctx, s.log, "verify sign-up", err)
	}
	resp := connect.NewResponse(&apiv1.VerifySignupResponse{})
	if resp.Msg.Session, err = s.startSession(ctx, resp.Header(), user); err != nil {
		return nil, internal(ctx, s.log, "start session", err)
	}
	return resp, nil
}

// setupDone turns sign-ups away until the setup screen (with its setup
// code) created the first account, an instance administrator.
func (s *AuthServer) setupDone(ctx context.Context) error {
	required, err := s.auth.SetupRequired(ctx)
	if err != nil {
		return internal(ctx, s.log, "setup state", err)
	}
	if required {
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_SETUP_REQUIRED, "finish the setup first")
	}
	return nil
}

// freeSlug returns slug, or slug with a random suffix when an organization
// took it after the sign-up started.
func freeSlug(ctx context.Context, tx pgx.Tx, slug string) (string, error) {
	candidate := slug
	for range 5 {
		_, err := store.GetOrganizationBySlug(ctx, tx, candidate)
		if errors.Is(err, store.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = strings.TrimRight(slug[:min(len(slug), 45)], "-") + "-" + strings.ToLower(secret.Random(4))
	}
	return "", fmt.Errorf("no free organization slug like %q", slug)
}

// sendSignupLink e-mails a sign-up link. The message carries nothing the
// signer typed (name, organization), so nobody can use it to send their
// own text to other people's addresses. Without SMTP (development only)
// the link goes to the log.
func (s *AuthServer) sendSignupLink(ctx context.Context, email, link string) error {
	if !s.account.Available() {
		s.log.WarnContext(ctx, "SMTP is not configured: sign-up link written to the log (development only)",
			slog.String("email", email), slog.String("url", link))
		return nil
	}
	return s.account.SignupLink(ctx, email, link)
}

// passwordResetTTL is how long a password reset link works.
const passwordResetTTL = time.Hour

// RequestPasswordReset implements AuthService.RequestPasswordReset.
func (s *AuthServer) RequestPasswordReset(ctx context.Context, req *connect.Request[apiv1.RequestPasswordResetRequest]) (*connect.Response[apiv1.RequestPasswordResetResponse], error) {
	if !s.cfg.Auth.PasswordLogin {
		return nil, errPermission("password sign-in is disabled on this server")
	}
	if !s.account.Available() {
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_EMAIL_DELIVERY_FAILED,
			"this server sends no e-mail; an administrator can set a new password (izci user reset-password)")
	}
	if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	email := strings.ToLower(strings.TrimSpace(req.Msg.GetEmail()))
	if err := s.auth.ThrottleEmail(ctx, "reset", email); err != nil {
		return nil, throttled(err)
	}
	u, err := store.GetUserByEmail(ctx, s.db.Pool, email)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return nil, internal(ctx, s.log, "password reset", err)
	case !u.Disabled:
		token, err := store.CreatePasswordReset(ctx, s.db.Pool, u.ID, passwordResetTTL)
		if err != nil {
			return nil, internal(ctx, s.log, "password reset", err)
		}
		// In the background: the answer takes as long whether or not the
		// address has an account.
		link := s.cfg.PublicURL + "/reset-password/" + token
		go func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			if err := s.account.PasswordReset(ctx, u.Email, link); err != nil {
				s.log.Warn("password reset e-mail not sent", slog.Any("err", err))
			}
		}(context.WithoutCancel(ctx))
	}
	return connect.NewResponse(&apiv1.RequestPasswordResetResponse{}), nil
}

// ResetPassword implements AuthService.ResetPassword.
func (s *AuthServer) ResetPassword(ctx context.Context, req *connect.Request[apiv1.ResetPasswordRequest]) (*connect.Response[apiv1.ResetPasswordResponse], error) {
	if !s.cfg.Auth.PasswordLogin {
		return nil, errPermission("password sign-in is disabled on this server")
	}
	if err := s.auth.ThrottlePublic(ctx, infoFrom(ctx).IP); err != nil {
		return nil, throttled(err)
	}
	if err := auth.ValidatePassword(req.Msg.GetNewPassword()); err != nil {
		return nil, weakPassword()
	}
	linkInvalid := apiError(connect.CodeNotFound, apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID,
		"the password reset link is unknown, used or expired; ask for a new one")
	// The link is checked before the costly password hash.
	switch ok, err := store.PasswordResetValid(ctx, s.db.Pool, req.Msg.GetToken()); {
	case err != nil:
		return nil, internal(ctx, s.log, "reset password", err)
	case !ok:
		return nil, linkInvalid
	}
	hash, err := auth.HashPassword(req.Msg.GetNewPassword())
	if err != nil {
		return nil, internal(ctx, s.log, "hash password", err)
	}
	var user *store.User
	err = s.db.InSystem(ctx, func(tx pgx.Tx) error {
		id, err := store.TakePasswordReset(ctx, tx, req.Msg.GetToken())
		if err != nil {
			return err
		}
		if err := store.UpdateUserPassword(ctx, tx, id, hash); err != nil {
			return err
		}
		// Whoever knew the old password is signed out everywhere.
		if _, err := store.DeleteUserSessions(ctx, tx, id); err != nil {
			return err
		}
		user, err = store.GetUser(ctx, tx, id)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, linkInvalid
	case err != nil:
		return nil, internal(ctx, s.log, "reset password", err)
	}
	s.account.PasswordChanged(ctx, user.Email, time.Now())
	return connect.NewResponse(&apiv1.ResetPasswordResponse{}), nil
}
