package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// CookieName is the browser session cookie.
const CookieName = "izci_session"

// Sign-in errors.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountDisabled    = errors.New("account is disabled")
	ErrPasswordLoginOff   = errors.New("password sign-in is disabled")
	ErrOrgNotFound        = errors.New("organization not found or not accessible")
)

// TooManyAttemptsError reports login throttling.
type TooManyAttemptsError struct{ RetryAfter time.Duration }

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("too many sign-in attempts, retry in %s", e.RetryAfter.Round(time.Second))
}

// Service implements authentication.
type Service struct {
	db   *db.DB
	cfg  *config.Config
	box  *secret.Box
	log  *slog.Logger
	oidc map[string]*oidcProvider
}

// NewService creates the authentication service.
func NewService(database *db.DB, cfg *config.Config, box *secret.Box, log *slog.Logger) *Service {
	s := &Service{
		db: database, cfg: cfg, box: box, log: log,
		oidc: map[string]*oidcProvider{},
	}
	for _, p := range cfg.OIDC {
		s.oidc[p.ID] = &oidcProvider{cfg: p}
	}
	return s
}

// Config exposes the configuration.
func (s *Service) Config() *config.Config { return s.cfg }

// Box exposes the secret box.
func (s *Service) Box() *secret.Box { return s.box }

// Authenticate resolves the credentials of a request. It returns nil (and
// no error) when the request carries none; invalid credentials are treated
// the same way so that callers fail with UNAUTHENTICATED.
func (s *Service) Authenticate(ctx context.Context, h http.Header) (*Principal, error) {
	if authz := strings.TrimSpace(h.Get("Authorization")); len(authz) > 7 && strings.EqualFold(authz[:7], "bearer ") {
		token := strings.TrimSpace(authz[7:])
		if !secret.HasPrefix(token, secret.PrefixAccessToken) {
			return nil, nil
		}
		tw, err := store.LookupPersonalToken(ctx, s.db.Pool, token)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if tw.User.Disabled {
			return nil, nil
		}
		_ = store.TouchPersonalToken(ctx, s.db.Pool, tw.Token.ID)
		return &Principal{User: tw.User, Token: tw.Token}, nil
	}
	cookies, err := http.ParseCookie(h.Get("Cookie"))
	if err != nil {
		return nil, nil
	}
	for _, c := range cookies {
		if c.Name != CookieName || !secret.HasPrefix(c.Value, secret.PrefixSession) {
			continue
		}
		sw, err := store.LookupSession(ctx, s.db.Pool, c.Value)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if sw.User.Disabled {
			return nil, nil
		}
		// Sliding expiration, written at most once a minute.
		if time.Since(sw.Session.LastSeenAt) > time.Minute {
			if err := store.TouchSession(ctx, s.db.Pool, sw.Session.ID, s.cfg.Auth.SessionTTL); err == nil {
				sw.Session.ExpiresAt = time.Now().Add(s.cfg.Auth.SessionTTL)
			}
		}
		return &Principal{User: sw.User, Session: sw.Session}, nil
	}
	return nil, nil
}

// ErrInvalidAccessToken means a personal access token is unknown, expired,
// revoked or belongs to a disabled user.
var ErrInvalidAccessToken = errors.New("invalid access token")

// VerifyAccessToken checks a personal access token (MCP clients, which
// cannot use session cookies).
func (s *Service) VerifyAccessToken(ctx context.Context, token string) error {
	p, err := s.Authenticate(ctx, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		return err
	}
	if p == nil || p.Token == nil {
		return ErrInvalidAccessToken
	}
	return nil
}

// Login verifies email and password. Failures are limited per account and
// client address, per account overall and per address (see throttle.go);
// only failures count, and a success clears its account and address.
func (s *Service) Login(ctx context.Context, email, password string, ip netip.Addr) (*store.User, error) {
	if !s.cfg.Auth.PasswordLogin {
		return nil, ErrPasswordLoginOff
	}
	email = strings.ToLower(strings.TrimSpace(email))
	limits := map[string]int{"email:" + email: loginEmailLimit}
	pair := "login:" + email
	if ip.IsValid() {
		addr := addressKey(ip)
		pair += "|" + addr
		limits["ip:"+addr] = loginIPLimit
	}
	limits[pair] = loginPairLimit
	wait, err := s.limited(ctx, limits)
	if err != nil {
		return nil, err
	}
	if wait > 0 {
		return nil, &TooManyAttemptsError{RetryAfter: wait}
	}
	fail := func() (*store.User, error) {
		keys := make([]string, 0, len(limits))
		for k := range limits {
			keys = append(keys, k)
		}
		if err := s.count(ctx, keys...); err != nil {
			s.log.Warn("count failed sign-in", slog.Any("err", err))
		}
		return nil, ErrInvalidCredentials
	}
	u, err := store.GetUserByEmail(ctx, s.db.Pool, email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.PasswordHash == nil) {
		_, _ = VerifyPassword(dummyHash, password) // equalize timing
		return fail()
	}
	if err != nil {
		return nil, err
	}
	ok, err := VerifyPassword(*u.PasswordHash, password)
	if err != nil || !ok {
		return fail()
	}
	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	if err := s.forget(ctx, pair); err != nil {
		s.log.Warn("clear sign-in failures", slog.Any("err", err))
	}
	// With two-factor authentication the sign-in completes in
	// CompleteChallenge.
	if !u.TwoFactor() {
		_ = store.MarkUserLogin(ctx, s.db.Pool, u.ID)
	}
	return u, nil
}

// StartSession creates a session for the user and returns its cookie.
func (s *Service) StartSession(ctx context.Context, userID uuid.UUID, ip netip.Addr, userAgent string) (*http.Cookie, *store.Session, error) {
	sess, token, err := store.CreateSession(ctx, s.db.Pool, userID, ip, userAgent, s.cfg.Auth.SessionTTL)
	if err != nil {
		return nil, nil, err
	}
	return s.sessionCookie(token, sess.ExpiresAt), sess, nil
}

func (s *Service) secureCookies() bool { return strings.HasPrefix(s.cfg.PublicURL, "https://") }

func (s *Service) sessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name: CookieName, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	}
}

// ClearCookie expires the session cookie.
func (s *Service) ClearCookie() *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode}
}

// EndSession deletes the principal's session.
func (s *Service) EndSession(ctx context.Context, p *Principal) error {
	if p == nil || p.Session == nil {
		return nil
	}
	_, err := store.DeleteSession(ctx, s.db.Pool, p.User.ID, p.Session.ID)
	return err
}

// ResolveTenant returns the organization named by slug and the caller's
// role in it. Superadmins act as owners of every organization; tokens bound
// to an organization cannot reach others. Organizations requiring
// two-factor authentication turn away users without it
// (ErrTwoFactorRequired).
func (s *Service) ResolveTenant(ctx context.Context, p *Principal, slug string) (*Tenant, error) {
	org, err := store.GetOrganizationBySlug(ctx, s.db.Pool, strings.ToLower(strings.TrimSpace(slug)))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.Token != nil && p.Token.OrgID != nil && *p.Token.OrgID != org.ID {
		return nil, ErrOrgNotFound
	}
	role, err := store.MemberRole(ctx, s.db.Pool, org.ID, p.User.ID)
	switch {
	case errors.Is(err, store.ErrNotFound) && p.User.Superadmin:
		role = apiv1.Role_ROLE_OWNER
	case errors.Is(err, store.ErrNotFound):
		return nil, ErrOrgNotFound
	case err != nil:
		return nil, err
	}
	if org.Settings.GetRequireTwoFactor() && !p.User.TwoFactor() {
		return nil, ErrTwoFactorRequired
	}
	return &Tenant{Org: org, Role: role}, nil
}

// TokenOrg returns the organization a token is bound to, for requests
// without an Izci-Org header.
func (s *Service) TokenOrg(ctx context.Context, p *Principal) (*Tenant, error) {
	if p.Token == nil || p.Token.OrgID == nil {
		return nil, nil
	}
	org, err := store.GetOrganization(ctx, s.db.Pool, *p.Token.OrgID)
	if err != nil {
		return nil, err
	}
	return s.ResolveTenant(ctx, p, org.Slug)
}

// SetupToken is the code the setup screen asks for: IZCI_SETUP_TOKEN, or
// one derived from IZCI_SECRET_KEY, the same on every instance.
func (s *Service) SetupToken() string {
	if t := strings.TrimSpace(s.cfg.Auth.SetupToken.Value()); t != "" {
		return t
	}
	return s.box.Code("setup", 16)
}

// SetupTokenValid compares a typed setup code; case, spaces and dashes do
// not matter.
func (s *Service) SetupTokenValid(code string) bool {
	norm := func(v string) string {
		return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(v)))
	}
	got := norm(code)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(norm(s.SetupToken()))) == 1
}

// SetupRequired reports whether no user exists yet.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := store.CountUsers(ctx, s.db.Pool)
	return n == 0, err
}

// JoinAutomatically adds a user to organizations whose auto-join domains
// match the email domain.
func (s *Service) JoinAutomatically(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string) error {
	_, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok || domain == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO org_members (org_id, user_id, role)
		SELECT o.id, $1, coalesce(CASE o.settings->>'auto_join_role'
		         WHEN 'ROLE_VIEWER' THEN 1 WHEN 'ROLE_MEMBER' THEN 2 WHEN 'ROLE_ADMIN' THEN 3 END, 2)
		FROM organizations o
		WHERE o.settings->'auto_join_domains' ? $2
		ON CONFLICT (org_id, user_id) DO NOTHING`, userID, domain)
	return err
}
