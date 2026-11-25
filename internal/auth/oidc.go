package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/mcihad/kentos-izci/internal/clientip"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

const (
	stateCookie = "izci_oidc"
	stateTTL    = 10 * time.Minute
)

type oidcProvider struct {
	cfg config.OIDCProvider

	mu        sync.Mutex
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauth     *oauth2.Config
	failedAt  time.Time
	lastError error
}

// ProviderInfo describes a configured provider for the login page.
type ProviderInfo struct {
	ID, Name, Icon, LoginURL string
}

// Providers lists the configured OpenID Connect providers.
func (s *Service) Providers() []ProviderInfo {
	var out []ProviderInfo
	for _, p := range s.cfg.OIDC {
		out = append(out, ProviderInfo{ID: p.ID, Name: p.Name, Icon: p.Icon, LoginURL: "/auth/oidc/" + p.ID + "/start"})
	}
	return out
}

// discover loads provider metadata lazily and retries at most every 30s.
func (s *Service) discover(ctx context.Context, id string) (*oidcProvider, error) {
	p, ok := s.oidc[id]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", id)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider != nil {
		return p, nil
	}
	if time.Since(p.failedAt) < 30*time.Second && p.lastError != nil {
		return nil, p.lastError
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	prov, err := oidc.NewProvider(dctx, p.cfg.Issuer)
	if err != nil {
		p.failedAt, p.lastError = time.Now(), fmt.Errorf("discover %s: %w", p.cfg.Issuer, err)
		return nil, p.lastError
	}
	p.provider = prov
	p.verifier = prov.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})
	p.oauth = &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  s.cfg.PublicURL + "/auth/oidc/" + id + "/callback",
		Scopes:       p.cfg.Scopes,
	}
	return p, nil
}

// RegisterRoutes mounts the browser redirect endpoints.
func (s *Service) RegisterRoutes(handle func(pattern string, h http.Handler)) {
	handle("GET /auth/oidc/{provider}/start", http.HandlerFunc(s.oidcStart))
	handle("GET /auth/oidc/{provider}/callback", http.HandlerFunc(s.oidcCallback))
}

type oidcState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
	Expires  int64  `json:"e"`
	// LinkSession is set when a signed-in user links the identity to
	// their account: the session that started the flow, which must still
	// be the caller's when it returns.
	LinkSession string `json:"l,omitempty"`
	// ReauthSession is set when a signed-in user proves their identity
	// again for a sensitive change, as for LinkSession.
	ReauthSession string `json:"r,omitempty"`
}

func (s *Service) oidcStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	p, err := s.discover(r.Context(), id)
	if err != nil {
		s.log.Warn("oidc start failed", slog.String("provider", id), slog.Any("err", err))
		loginRedirect(w, r, "oidc_unavailable")
		return
	}
	st := oidcState{
		Provider: id, State: secret.Random(32), Nonce: secret.Random(32), Verifier: oauth2.GenerateVerifier(),
		Next: safeNext(r.URL.Query().Get("next")), Expires: time.Now().Add(stateTTL).Unix(),
	}
	// ?link=1 links the identity to the signed-in account and ?reauth=1
	// re-authenticates its session, instead of signing in.
	if link, reauth := r.URL.Query().Get("link") == "1", r.URL.Query().Get("reauth") == "1"; link || reauth {
		pr, err := s.Authenticate(r.Context(), r.Header)
		if err != nil || pr == nil || pr.Session == nil {
			http.Redirect(w, r, "/login?redirect="+url.QueryEscape(st.Next), http.StatusFound)
			return
		}
		switch {
		case reauth:
			st.ReauthSession = pr.Session.ID.String()
		case !RecentlyAuthenticated(pr):
			// A new sign-in method is a sensitive change.
			http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_reauth_required"), http.StatusFound)
			return
		default:
			st.LinkSession = pr.Session.ID.String()
		}
	}
	payload, _ := json.Marshal(st)
	value := base64.RawURLEncoding.EncodeToString(payload)
	// Path=/ is what the __Host- prefix requires.
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(stateCookie), Value: value + "." + s.box.Sign(value, 32), Path: "/",
		MaxAge: int(stateTTL.Seconds()), HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	authURL := p.oauth.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Service) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("provider")
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(stateCookie), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})

	st, ok := s.readState(r)
	if !ok || st.Provider != id || r.URL.Query().Get("state") != st.State {
		loginRedirect(w, r, "oidc_state")
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.log.Info("oidc provider returned an error", slog.String("provider", id), slog.String("error", e))
		loginRedirect(w, r, "oidc_denied")
		return
	}
	p, err := s.discover(ctx, id)
	if err != nil {
		loginRedirect(w, r, "oidc_unavailable")
		return
	}
	tok, err := p.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		s.log.Warn("oidc code exchange failed", slog.String("provider", id), slog.Any("err", err))
		loginRedirect(w, r, "oidc_exchange")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil || idt.Nonce != st.Nonce {
		s.log.Warn("oidc id token rejected", slog.String("provider", id), slog.Any("err", err))
		loginRedirect(w, r, "oidc_token")
		return
	}
	var claims struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     *bool  `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Picture           string `json:"picture"`
	}
	if err := idt.Claims(&claims); err != nil {
		loginRedirect(w, r, "oidc_token")
		return
	}
	if claims.Email == "" {
		if ui, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
			claims.Email = ui.Email
			if ui.EmailVerified {
				v := true
				claims.EmailVerified = &v
			}
			var extra struct {
				Name    string `json:"name"`
				Picture string `json:"picture"`
			}
			if ui.Claims(&extra) == nil {
				if claims.Name == "" {
					claims.Name = extra.Name
				}
				if claims.Picture == "" {
					claims.Picture = extra.Picture
				}
			}
		}
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if st.LinkSession != "" {
		s.linkIdentity(w, r, st, id, claims.Subject, claims.Email)
		return
	}
	if st.ReauthSession != "" {
		s.reauthIdentity(w, r, st, id, claims.Subject)
		return
	}
	// The address decides account creation, linking and organization
	// membership, so it counts only when the provider vouches for it.
	trusted := claims.Email != "" && trustedEmail(claims.EmailVerified, p.cfg.TrustEmail)
	if claims.Email == "" || (len(p.cfg.AllowedDomains) > 0 && !trusted) {
		loginRedirect(w, r, "oidc_email")
		return
	}
	if !domainAllowed(claims.Email, p.cfg.AllowedDomains) {
		loginRedirect(w, r, "oidc_domain")
		return
	}
	if claims.Name == "" {
		claims.Name = claims.PreferredUsername
	}

	var user *store.User
	err = s.db.InSystem(ctx, func(tx pgx.Tx) error {
		u, err := store.UserByIdentity(ctx, tx, id, claims.Subject)
		switch {
		case err == nil:
			user = u
		case !errors.Is(err, store.ErrNotFound):
			return err
		case !trusted:
			return errUnverifiedEmail
		default:
			existing, err := store.GetUserByEmail(ctx, tx, claims.Email)
			switch {
			case err == nil && p.cfg.TrustEmail:
				user = existing
			case err == nil:
				// Linking by address would hand the account to whoever
				// controls the address at this provider; the owner links
				// it from their profile instead.
				return errLinkRequired
			case !errors.Is(err, store.ErrNotFound):
				return err
			case !p.cfg.AutoCreate:
				return errNoAccount
			default:
				// The first account comes from the setup screen (with its
				// setup code), which makes it an instance administrator.
				if n, err := store.CountUsers(ctx, tx); err != nil {
					return err
				} else if n == 0 {
					return errSetupPending
				}
				if user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: claims.Email, Name: claims.Name, AvatarURL: claims.Picture}); err != nil {
					return err
				}
			}
		}
		if err := store.LinkIdentity(ctx, tx, id, claims.Subject, user.ID, claims.Email); err != nil {
			return err
		}
		if !trusted {
			return nil
		}
		if p.cfg.DefaultOrg != "" {
			if org, err := store.GetOrganizationBySlug(ctx, tx, p.cfg.DefaultOrg); err == nil {
				if _, err := store.MemberRole(ctx, tx, org.ID, user.ID); errors.Is(err, store.ErrNotFound) {
					if err := store.AddMember(ctx, tx, org.ID, user.ID, RoleFromName(p.cfg.DefaultRole)); err != nil {
						return err
					}
				}
			}
		}
		return s.JoinAutomatically(ctx, tx, user.ID, claims.Email)
	})
	switch {
	case errors.Is(err, errNoAccount):
		loginRedirect(w, r, "oidc_no_account")
		return
	case errors.Is(err, errSetupPending):
		loginRedirect(w, r, "oidc_setup_required")
		return
	case errors.Is(err, errUnverifiedEmail):
		if claims.EmailVerified == nil {
			s.log.Warn("oidc provider sent no email_verified claim; set OIDC_<ID>_TRUST_EMAIL=true if it only issues verified addresses",
				slog.String("provider", id))
		}
		loginRedirect(w, r, "oidc_email")
		return
	case errors.Is(err, errLinkRequired):
		loginRedirect(w, r, "oidc_link_required")
		return
	case err != nil:
		s.log.Error("oidc sign-in failed", slog.String("provider", id), slog.Any("err", err))
		loginRedirect(w, r, "oidc_internal")
		return
	}
	if user.Disabled {
		loginRedirect(w, r, "account_disabled")
		return
	}
	// Accounts with two-factor authentication finish on the login page,
	// which asks for the code.
	if user.TwoFactor() {
		cookie, err := s.BeginChallenge(ctx, user.ID)
		if err != nil {
			s.log.Error("oidc sign-in failed", slog.String("provider", id), slog.Any("err", err))
			loginRedirect(w, r, "oidc_internal")
			return
		}
		http.SetCookie(w, cookie)
		target := "/login?two_factor=1"
		if st.Next != "/" {
			target += "&redirect=" + url.QueryEscape(st.Next)
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	_ = store.MarkUserLogin(ctx, s.db.Pool, user.ID)
	ip := clientip.Resolve(r.RemoteAddr, r.Header, s.cfg.HTTP.TrustedProxies)
	cookie, _, err := s.StartSession(ctx, user.ID, ip, r.UserAgent(), "oidc:"+id)
	if err != nil {
		loginRedirect(w, r, "oidc_internal")
		return
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, st.Next, http.StatusFound)
}

var (
	errNoAccount       = errors.New("no account for this identity")
	errSetupPending    = errors.New("the server is not set up yet")
	errUnverifiedEmail = errors.New("email not verified by the provider")
	errLinkRequired    = errors.New("an account with this email exists; link the identity from it")
	errIdentityTaken   = errors.New("the identity is linked to another account")
)

// trustedEmail reports whether the provider vouches for the e-mail
// address: email_verified is true, or the claim is missing and the
// operator trusts the provider (OIDC_<ID>_TRUST_EMAIL).
func trustedEmail(verified *bool, trust bool) bool {
	if verified != nil {
		return *verified
	}
	return trust
}

// linkIdentity finishes the link flow: the identity joins the account of
// the session that started it, which must still be signed in.
func (s *Service) linkIdentity(w http.ResponseWriter, r *http.Request, st *oidcState, provider, subject, email string) {
	ctx := r.Context()
	pr, err := s.Authenticate(ctx, r.Header)
	if err != nil || pr == nil || pr.Session == nil || pr.Session.ID.String() != st.LinkSession {
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_link_session"), http.StatusFound)
		return
	}
	err = s.db.InSystem(ctx, func(tx pgx.Tx) error {
		u, err := store.UserByIdentity(ctx, tx, provider, subject)
		switch {
		case err == nil && u.ID != pr.User.ID:
			return errIdentityTaken
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return err
		}
		return store.LinkIdentity(ctx, tx, provider, subject, pr.User.ID, email)
	})
	switch {
	case errors.Is(err, errIdentityTaken):
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_identity_taken"), http.StatusFound)
	case err != nil:
		s.log.Error("oidc link failed", slog.String("provider", provider), slog.Any("err", err))
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_internal"), http.StatusFound)
	default:
		http.Redirect(w, r, withQuery(st.Next, "oidc_linked", provider), http.StatusFound)
	}
}

// reauthIdentity finishes re-authentication through the provider: the
// identity must belong to the session's user.
func (s *Service) reauthIdentity(w http.ResponseWriter, r *http.Request, st *oidcState, provider, subject string) {
	ctx := r.Context()
	pr, err := s.Authenticate(ctx, r.Header)
	if err != nil || pr == nil || pr.Session == nil || pr.Session.ID.String() != st.ReauthSession {
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_link_session"), http.StatusFound)
		return
	}
	u, err := store.UserByIdentity(ctx, s.db.Pool, provider, subject)
	if err != nil || u.ID != pr.User.ID {
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_reauth_mismatch"), http.StatusFound)
		return
	}
	if _, err := store.MarkSessionReauthenticated(ctx, s.db.Pool, pr.Session.ID); err != nil {
		s.log.Error("oidc re-authentication failed", slog.String("provider", provider), slog.Any("err", err))
		http.Redirect(w, r, withQuery(st.Next, "oidc_error", "oidc_internal"), http.StatusFound)
		return
	}
	http.Redirect(w, r, withQuery(st.Next, "oidc_reauth", "1"), http.StatusFound)
}

// withQuery adds a query parameter to a same-site path.
func withQuery(path, key, value string) string {
	u, err := url.Parse(path)
	if err != nil {
		return "/"
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Service) readState(r *http.Request) (*oidcState, bool) {
	c, err := r.Cookie(s.cookieName(stateCookie))
	if err != nil {
		return nil, false
	}
	value, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !s.box.Verify(value, sig, 32) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	var st oidcState
	if json.Unmarshal(raw, &st) != nil || time.Now().Unix() > st.Expires {
		return nil, false
	}
	return &st, true
}

// safeNext accepts only same-site relative paths.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return next
}

func domainAllowed(email string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	_, domain, _ := strings.Cut(email, "@")
	for _, d := range allowed {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// loginRedirect sends the browser to the login page with an error code the
// UI translates.
func loginRedirect(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}
