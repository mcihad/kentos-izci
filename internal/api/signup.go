package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
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
	if s.mailer == nil {
		s.log.WarnContext(ctx, "SMTP is not configured: sign-up link written to the log (development only)",
			slog.String("email", email), slog.String("url", link))
		return nil
	}
	text := "Merhaba,\r\n\r\n" + email + " adresiyle İzci'de hesap açmak istediniz. Hesabınızı ve organizasyonunuzu " +
		"oluşturmak için bu bağlantıyı açın:\r\n\r\n" + link + "\r\n\r\n" + signupMailFooter + "\r\n"
	var h bytes.Buffer
	if err := signupMail.Execute(&h, map[string]string{"Email": email, "Link": link, "Footer": signupMailFooter}); err != nil {
		return err
	}
	return s.mailer.SendMail(ctx, email, "İzci hesabınızı etkinleştirin", text, h.String())
}

const signupMailFooter = "Bağlantı 24 saat geçerlidir ve bir kez kullanılır. Bu isteği siz yapmadıysanız e-postayı " +
	"yok sayın; hesap açılmaz."

var signupMail = template.Must(template.New("signup").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1f2328">
<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e3e6ea;border-radius:8px;padding:28px">
<p style="margin:0 0 16px;font-size:15px;line-height:1.5">Merhaba,</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.5">{{.Email}} adresiyle İzci'de hesap açmak istediniz. Hesabınızı ve organizasyonunuzu oluşturmak için düğmeye basın.</p>
<p style="margin:24px 0"><a href="{{.Link}}" style="display:inline-block;background:#1d4ed8;color:#ffffff;text-decoration:none;padding:10px 18px;border-radius:6px;font-weight:600;font-size:15px">Hesabımı etkinleştir</a></p>
<p style="margin:0 0 8px;font-size:13px;line-height:1.5;color:#59636e">{{.Footer}}</p>
<p style="margin:0;font-size:12px;line-height:1.5;color:#59636e;word-break:break-all">{{.Link}}</p>
</div></body></html>
`))
