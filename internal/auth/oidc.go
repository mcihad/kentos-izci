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
	payload, _ := json.Marshal(st)
	value := base64.RawURLEncoding.EncodeToString(payload)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: value + "." + s.box.Sign(value, 32), Path: "/auth/oidc/",
		MaxAge: int(stateTTL.Seconds()), HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	authURL := p.oauth.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Service) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("provider")
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/auth/oidc/", MaxAge: -1, HttpOnly: true,
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
	if claims.Email == "" {
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
		default:
			existing, err := store.GetUserByEmail(ctx, tx, claims.Email)
			switch {
			case err == nil && (claims.EmailVerified == nil || *claims.EmailVerified):
				user = existing
			case err == nil:
				return errUnverifiedEmail
			case !errors.Is(err, store.ErrNotFound):
				return err
			case !p.cfg.AutoCreate:
				return errNoAccount
			default:
				if user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: claims.Email, Name: claims.Name, AvatarURL: claims.Picture}); err != nil {
					return err
				}
			}
		}
		if err := store.LinkIdentity(ctx, tx, id, claims.Subject, user.ID, claims.Email); err != nil {
			return err
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
	case errors.Is(err, errUnverifiedEmail):
		loginRedirect(w, r, "oidc_email")
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
	_ = store.MarkUserLogin(ctx, s.db.Pool, user.ID)
	ip := clientip.Resolve(r.RemoteAddr, r.Header, s.cfg.HTTP.TrustedProxies)
	cookie, _, err := s.StartSession(ctx, user.ID, ip, r.UserAgent())
	if err != nil {
		loginRedirect(w, r, "oidc_internal")
		return
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, st.Next, http.StatusFound)
}

var (
	errNoAccount       = errors.New("no account for this identity")
	errUnverifiedEmail = errors.New("email not verified by the provider")
)

func (s *Service) readState(r *http.Request) (*oidcState, bool) {
	c, err := r.Cookie(stateCookie)
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
