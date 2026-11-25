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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// CookieName is the browser session cookie. On https it is sent as
// "__Host-izci_session" (see cookieName).
const CookieName = "izci_session"

// hostPrefix makes browsers accept a cookie only from this host over
// https, with Path=/ and no Domain: an application on a sibling subdomain
// cannot plant its own session or challenge cookie ("cookie tossing").
const hostPrefix = "__Host-"

// cookieName is the name a cookie has on this server: with the __Host-
// prefix on https, as it is on plain http (development).
func (s *Service) cookieName(name string) string {
	if s.secureCookies() {
		return hostPrefix + name
	}
	return name
}

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
	// notifier hears about sign-ins from new devices; nil reports none.
	notifier SignInNotifier
	// superadminSeen remembers when a server administrator was last
	// recorded entering an organization they are not a member of.
	superadminSeen sync.Map
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
		_ = store.TouchPersonalToken(ctx, s.db.Pool, tw.Token.ID, clientIP(ctx))
		return &Principal{User: tw.User, Token: tw.Token}, nil
	}
	cookies, err := http.ParseCookie(h.Get("Cookie"))
	if err != nil {
		return nil, nil
	}
	for _, c := range cookies {
		if c.Name != s.cookieName(CookieName) || !secret.HasPrefix(c.Value, secret.PrefixSession) {
			continue
		}
		sw, err := store.LookupSession(ctx, s.db.Pool, c.Value, s.sessionMaxAge())
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
			if expires, err := store.TouchSession(ctx, s.db.Pool, sw.Session.ID, s.cfg.Auth.SessionTTL, s.sessionMaxAge()); err == nil {
				sw.Session.ExpiresAt = expires
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

// SignInNotifier hears about sign-ins from a device (browser and
// operating system) a member had not used before (accountmail.Sender).
type SignInNotifier interface {
	NewSignIn(ctx context.Context, to, device string, ip netip.Addr, at time.Time)
}

// SetSignInNotifier sets who hears about sign-ins from new devices.
func (s *Service) SetSignInNotifier(n SignInNotifier) { s.notifier = n }

// StartSession creates a session for the user and returns its cookie. A
// sign-in from a device the member had not used is reported to them
// (except their very first sign-in).
func (s *Service) StartSession(ctx context.Context, userID uuid.UUID, ip netip.Addr, userAgent, method string) (*http.Cookie, *store.Session, error) {
	maxAge := s.sessionMaxAge()
	sess, token, err := store.CreateSession(ctx, s.db.Pool, userID, ip, userAgent, min(s.cfg.Auth.SessionTTL, maxAge))
	if err != nil {
		return nil, nil, err
	}
	ua := ingest.ParseUserAgent(userAgent)
	s.Audit(ctx, store.AuditEntry{ActorID: &userID, Action: "auth.login", TargetType: "user", TargetID: userID.String(),
		Metadata: map[string]string{"method": method, "device": strings.Trim(ua.BrowserLabel()+" · "+ua.OSLabel(), " ·")}, IP: ip})
	s.seenDevice(ctx, userID, ip, userAgent)
	// The browser keeps the cookie until the session's last possible
	// moment; the server ends it earlier when it goes unused.
	return s.sessionCookie(token, sess.CreatedAt.Add(maxAge)), sess, nil
}

// Audit records an instance-level entry (sign-ins, account security);
// failures are logged, never returned: the action itself is done.
func (s *Service) Audit(ctx context.Context, e store.AuditEntry) {
	e.OrgID = nil
	if len(e.TargetName) > 320 {
		e.TargetName = e.TargetName[:320]
	}
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error { return store.InsertAudit(ctx, tx, e) })
	if err != nil {
		s.log.Warn("audit", slog.String("action", e.Action), slog.Any("err", err))
	}
}

// auditSuperadminAccess records in the organization's audit log, at most
// hourly per administrator, that a server administrator acted in an
// organization they are not a member of.
func (s *Service) auditSuperadminAccess(ctx context.Context, u *store.User, org *store.Organization) {
	key := u.ID.String() + "|" + org.ID.String()
	now := time.Now()
	if last, ok := s.superadminSeen.Load(key); ok && now.Sub(last.(time.Time)) < time.Hour {
		return
	}
	s.superadminSeen.Store(key, now)
	err := s.db.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		return store.InsertAudit(ctx, tx, store.AuditEntry{OrgID: &org.ID, ActorID: &u.ID, Action: "superadmin.access",
			TargetType: "organization", TargetID: org.ID.String(), TargetName: org.Slug, IP: clientIP(ctx)})
	})
	if err != nil {
		s.log.Warn("audit superadmin access", slog.Any("err", err))
	}
}

// AuditUser records an entry about a user's account at instance level and
// in every organization they belong to.
func (s *Service) AuditUser(ctx context.Context, userID uuid.UUID, e store.AuditEntry) {
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error { return store.InsertUserAudit(ctx, tx, userID, e) })
	if err != nil {
		s.log.Warn("audit", slog.String("action", e.Action), slog.Any("err", err))
	}
}

// seenDevice records the device of a sign-in and reports new ones.
func (s *Service) seenDevice(ctx context.Context, userID uuid.UUID, ip netip.Addr, userAgent string) {
	ua := ingest.ParseUserAgent(userAgent)
	key, label := ua.Browser+"|"+ua.OS, strings.TrimSpace(ua.BrowserLabel()+" · "+ua.OSLabel())
	if ua.Browser == "" && ua.OS == "" {
		key, label = "unknown", "tanınmayan bir istemci"
	}
	isNew, had, err := store.SeenDevice(ctx, s.db.Pool, userID, key)
	if err != nil {
		s.log.Warn("record sign-in device", slog.Any("err", err))
		return
	}
	if !isNew || !had || s.notifier == nil {
		return
	}
	if u, err := store.GetUser(ctx, s.db.Pool, userID); err == nil {
		s.notifier.NewSignIn(ctx, u.Email, strings.Trim(label, " ·"), ip, time.Now())
	}
}

// sessionMaxAge is AUTH_SESSION_MAX_AGE (configurations built in code may
// leave it out; then sessions end after AUTH_SESSION_TTL).
func (s *Service) sessionMaxAge() time.Duration {
	if s.cfg.Auth.SessionMaxAge > 0 {
		return s.cfg.Auth.SessionMaxAge
	}
	return s.cfg.Auth.SessionTTL
}

func (s *Service) secureCookies() bool { return strings.HasPrefix(s.cfg.PublicURL, "https://") }

func (s *Service) sessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name: s.cookieName(CookieName), Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	}
}

// ClearCookie expires the session cookie.
func (s *Service) ClearCookie() *http.Cookie {
	return &http.Cookie{Name: s.cookieName(CookieName), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
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

// SuperadminNeedsTwoFactor reports whether u is an instance administrator
// whose rights wait until they turn on two-factor authentication
// (AUTH_REQUIRE_2FA_SUPERADMIN).
func (s *Service) SuperadminNeedsTwoFactor(u *store.User) bool {
	return u.Superadmin && !u.TwoFactor() && s.cfg.SuperadminTwoFactor()
}

// ResolveTenant returns the organization named by slug and the caller's
// role in it. Superadmins act as owners of every organization (once they
// have two-factor authentication where the server asks for it,
// ErrSuperadminTwoFactor); tokens bound to an organization cannot reach
// others. Organizations requiring two-factor authentication turn away
// users without it (ErrTwoFactorRequired).
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
		if s.SuperadminNeedsTwoFactor(p.User) {
			return nil, ErrSuperadminTwoFactor
		}
		role = apiv1.Role_ROLE_OWNER
		s.auditSuperadminAccess(ctx, p.User, org)
	case errors.Is(err, store.ErrNotFound):
		return nil, ErrOrgNotFound
	case err != nil:
		return nil, err
	}
	if org.Settings.GetRequireTwoFactor() && !p.User.TwoFactor() {
		return nil, ErrTwoFactorRequired
	}
	if p.Token != nil {
		if err := s.tokenPolicy(ctx, p.Token, org); err != nil {
			return nil, err
		}
	}
	return &Tenant{Org: org, Role: role}, nil
}

// TokenPolicyError means the organization's access token policy rejects a
// token.
type TokenPolicyError struct{ Reason string }

func (e *TokenPolicyError) Error() string { return e.Reason }

// tokenPolicy applies the organization's access token policy: tokens older
// than token_max_days do not work in it, tokens unused for token_idle_days
// are revoked. t.LastUsedAt is the use before this request.
func (s *Service) tokenPolicy(ctx context.Context, t *store.PersonalToken, org *store.Organization) error {
	const day = 24 * time.Hour
	if idle := org.Settings.GetTokenIdleDays(); idle > 0 {
		last := t.CreatedAt
		if t.LastUsedAt != nil {
			last = *t.LastUsedAt
		}
		if time.Since(last) > time.Duration(idle)*day {
			if err := store.RevokePersonalToken(ctx, s.db.Pool, t.UserID, t.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			s.log.Info("access token revoked after organization idle limit", slog.String("token", t.Prefix),
				slog.String("org", org.Slug), slog.Int("idle_days", int(idle)))
			return &TokenPolicyError{Reason: fmt.Sprintf("the token was not used for more than %d days and has been revoked "+
				"(policy of organization %s); create a new one", idle, org.Slug)}
		}
	}
	if limit := org.Settings.GetTokenMaxDays(); limit > 0 && time.Since(t.CreatedAt) > time.Duration(limit)*day {
		return &TokenPolicyError{Reason: fmt.Sprintf("organization %s accepts access tokens for %d days after they were created; "+
			"create a new one", org.Slug, limit)}
	}
	return nil
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
		WHERE o.settings->'auto_join_domains' ? $2 AND o.deleted_at IS NULL
		ON CONFLICT (org_id, user_id) DO NOTHING`, userID, domain)
	return err
}
