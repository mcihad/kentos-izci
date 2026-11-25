package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

// fakeIdP is a minimal OpenID Connect provider with PKCE.
type fakeIdP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	codes  map[string]codeInfo
	sub    string
	email  string
	logins int
	// verified is the email_verified claim; nil leaves it out.
	verified *bool
}

type codeInfo struct{ nonce, challenge, redirect string }

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	yes := true
	p := &fakeIdP{key: key, codes: map[string]codeInfo{}, sub: "kimlik-user-42", email: "Zeynep@Kentos.com.tr", verified: &yes}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize", "token_endpoint": p.srv.URL + "/token",
			"jwks_uri": p.srv.URL + "/jwks", "userinfo_endpoint": p.srv.URL + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			http.Error(w, "PKCE required", http.StatusBadRequest)
			return
		}
		code := secret.Random(16)
		p.mu.Lock()
		p.codes[code] = codeInfo{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.mu.Lock()
		info, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		p.logins++
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != info.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		p.mu.Lock()
		c := map[string]any{
			"iss": p.srv.URL, "aud": "izci", "sub": p.sub, "email": p.email,
			"name": "Zeynep Kaya", "nonce": info.nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		if p.verified != nil {
			c["email_verified"] = *p.verified
		}
		p.mu.Unlock()
		claims, _ := json.Marshal(c)
		jws, _ := signer.Sign(claims)
		idToken, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func TestOIDCLoginFlow(t *testing.T) {
	idp := newFakeIdP(t)
	database := testutil.NewDB(t)
	ctx := context.Background()
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	izci := httptest.NewServer(mux)
	defer izci.Close()
	cfg := &config.Config{
		Env: config.EnvTest, PublicURL: izci.URL,
		Auth: config.AuthConfig{Signup: "invite", SessionTTL: time.Hour, PasswordLogin: true},
		OIDC: []config.OIDCProvider{{ID: "kimlik", Name: "KentOS Kimlik", Issuer: idp.srv.URL, ClientID: "izci", ClientSecret: "s3cret",
			Scopes: []string{"openid", "profile", "email"}, AutoCreate: true, AllowedDomains: []string{"kentos.com.tr"},
			DefaultOrg: "kentos", DefaultRole: "member"}},
	}
	box, _ := secret.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	svc := auth.NewService(database, cfg, box, log)
	svc.RegisterRoutes(mux.Handle)
	if _, err := api.Mount(mux.Handle, api.Deps{DB: database, Auth: svc, Config: cfg, Log: log}); err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("GET /o/kentos", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("app")) })
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("login")) })

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	res, err := browser.Get(izci.URL + "/auth/oidc/kimlik/start?next=/o/kentos")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Request.URL.Path != "/o/kentos" {
		t.Fatalf("flow ended at %s, want /o/kentos", res.Request.URL)
	}

	authc := apiv1connect.NewAuthServiceClient(browser, izci.URL)
	sess, err := authc.GetSession(ctx, connect.NewRequest(&apiv1.GetSessionRequest{}))
	if err != nil {
		t.Fatalf("session after OIDC login: %v", err)
	}
	u := sess.Msg.GetSession().GetUser()
	if u.GetEmail() != "zeynep@kentos.com.tr" || u.GetName() != "Zeynep Kaya" || u.GetHasPassword() || u.GetLinkedProviders()[0] != "kimlik" {
		t.Fatalf("provisioned user: %v", u)
	}
	ms := sess.Msg.GetSession().GetMemberships()
	if len(ms) != 1 || ms[0].GetOrganization().GetSlug() != "kentos" || ms[0].GetRole() != apiv1.Role_ROLE_MEMBER {
		t.Fatalf("default organization membership: %v", ms)
	}

	// A second sign-in reuses the linked account.
	res, _ = browser.Get(izci.URL + "/auth/oidc/kimlik/start")
	res.Body.Close()
	var users int
	database.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users)
	if users != 1 || idp.logins != 2 {
		t.Fatalf("users=%d logins=%d", users, idp.logins)
	}

	// With two-factor authentication the flow ends on the login page with a
	// challenge cookie, and the code completes the sign-in.
	zeynep, err := store.GetUserByEmail(ctx, database.Pool, "zeynep@kentos.com.tr")
	if err != nil {
		t.Fatal(err)
	}
	setup, err := svc.BeginTOTP(ctx, zeynep)
	if err != nil {
		t.Fatal(err)
	}
	totpSecret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	now := auth.TOTPStep(time.Now())
	if _, err := svc.EnableTOTP(ctx, zeynep.ID, uuid.Nil, auth.TOTPCode(totpSecret, now-1)); err != nil {
		t.Fatal(err)
	}
	jar2, _ := cookiejar.New(nil)
	phone := &http.Client{Jar: jar2}
	res, err = phone.Get(izci.URL + "/auth/oidc/kimlik/start?next=/o/kentos")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Request.URL.Path != "/login" || res.Request.URL.Query().Get("two_factor") != "1" || res.Request.URL.Query().Get("redirect") != "/o/kentos" {
		t.Fatalf("two-factor sign-in ended at %s", res.Request.URL)
	}
	phoneAuth := apiv1connect.NewAuthServiceClient(phone, izci.URL)
	if _, err := phoneAuth.GetSession(ctx, connect.NewRequest(&apiv1.GetSessionRequest{})); err == nil {
		t.Fatal("session before the second step")
	}
	done, err := phoneAuth.VerifyTwoFactor(ctx, connect.NewRequest(&apiv1.VerifyTwoFactorRequest{Code: auth.TOTPCode(totpSecret, now+1)}))
	if err != nil || done.Msg.GetSession().GetUser().GetEmail() != "zeynep@kentos.com.tr" || !done.Msg.GetSession().GetUser().GetTwoFactorEnabled() {
		t.Fatalf("second step: %v %v", done, err)
	}

	// A forged callback (state mismatch) is rejected.
	res, _ = browser.Get(izci.URL + "/auth/oidc/kimlik/callback?code=x&state=forged")
	res.Body.Close()
	if res.Request.URL.Path != "/login" || !strings.Contains(res.Request.URL.RawQuery, "error=oidc_state") {
		t.Fatalf("forged callback ended at %s", res.Request.URL)
	}

	// Domains outside the allow-list are refused.
	idp.sub, idp.email = "other", "someone@gmail.com"
	res, _ = browser.Get(izci.URL + "/auth/oidc/kimlik/start")
	res.Body.Close()
	if !strings.Contains(res.Request.URL.RawQuery, "error=oidc_domain") {
		t.Fatalf("foreign domain ended at %s", res.Request.URL)
	}
}

// set changes who the provider signs in.
func (p *fakeIdP) set(sub, email string, verified *bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sub, p.email, p.verified = sub, email, verified
}

func boolPtr(v bool) *bool { return &v }

// TestOIDCAccountLinking covers who an identity may sign in as: an
// address the provider does not vouch for never creates, links or joins
// anything, and an existing account is linked only by its signed-in owner
// (or by address at a provider the operator trusts).
func TestOIDCAccountLinking(t *testing.T) {
	idp := newFakeIdP(t)
	database := testutil.NewDB(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("ayşenin uzun parolası")
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE organizations SET settings = settings || '{"auto_join_domains": ["kentos.com.tr"]}' WHERE id = $1`, org.ID); err != nil {
			return err
		}
		_, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@kentos.com.tr", Name: "Ayşe", PasswordHash: &hash})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	izci := httptest.NewServer(mux)
	defer izci.Close()
	provider := func(id string, trust bool) config.OIDCProvider {
		return config.OIDCProvider{ID: id, Name: id, Issuer: idp.srv.URL, ClientID: "izci", ClientSecret: "s3cret",
			Scopes: []string{"openid", "email"}, AutoCreate: true, TrustEmail: trust}
	}
	cfg := &config.Config{
		Env: config.EnvTest, PublicURL: izci.URL,
		Auth: config.AuthConfig{Signup: "invite", SessionTTL: time.Hour, PasswordLogin: true},
		OIDC: []config.OIDCProvider{provider("kimlik", false), provider("kurum", true)},
	}
	box, _ := secret.NewBox(make([]byte, 32))
	svc := auth.NewService(database, cfg, box, slog.New(slog.DiscardHandler))
	svc.RegisterRoutes(mux.Handle)
	if _, err := api.Mount(mux.Handle, api.Deps{DB: database, Auth: svc, Config: cfg, Log: slog.New(slog.DiscardHandler)}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/login", "/o/kentos", "/o/kentos/account"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("page")) })
	}
	browser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar}
	}
	visit := func(c *http.Client, path string) *url.URL {
		t.Helper()
		res, err := c.Get(izci.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Request.URL
	}
	users := func() int {
		var n int
		database.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
		return n
	}

	// Without a vouched-for address nothing is created.
	for _, v := range []*bool{boolPtr(false), nil} {
		idp.set("stranger", "yeni@kentos.com.tr", v)
		if u := visit(browser(), "/auth/oidc/kimlik/start"); u.Path != "/login" || u.Query().Get("error") != "oidc_email" {
			t.Fatalf("email_verified=%v ended at %s", v, u)
		}
	}
	if n := users(); n != 1 {
		t.Fatalf("users after unverified sign-ins: %d", n)
	}

	// A verified address of an existing account is not enough: its owner
	// links the identity while signed in.
	idp.set("ayse-at-kimlik", "Ayse@Kentos.com.tr", boolPtr(true))
	if u := visit(browser(), "/auth/oidc/kimlik/start"); u.Query().Get("error") != "oidc_link_required" {
		t.Fatalf("existing account ended at %s", u)
	}
	if u := visit(browser(), "/auth/oidc/kimlik/start?link=1&next=/o/kentos/account"); u.Path != "/login" || u.Query().Get("redirect") != "/o/kentos/account" {
		t.Fatalf("link without a session ended at %s", u)
	}
	ayse := browser()
	authc := apiv1connect.NewAuthServiceClient(ayse, izci.URL)
	if _, err := authc.Login(ctx, connect.NewRequest(&apiv1.LoginRequest{Email: "ayse@kentos.com.tr", Password: "ayşenin uzun parolası"})); err != nil {
		t.Fatal(err)
	}
	// Linking is a sensitive change: an old session confirms its user's
	// identity first.
	age := func() {
		t.Helper()
		if _, err := database.Pool.Exec(ctx, `UPDATE sessions SET reauthenticated_at = now() - interval '1 hour'`); err != nil {
			t.Fatal(err)
		}
	}
	age()
	if u := visit(ayse, "/auth/oidc/kimlik/start?link=1&next=/o/kentos/account"); u.Path != "/o/kentos/account" || u.Query().Get("oidc_error") != "oidc_reauth_required" {
		t.Fatalf("link from an old session ended at %s", u)
	}
	if _, err := authc.Reauthenticate(ctx, connect.NewRequest(&apiv1.ReauthenticateRequest{Password: "ayşenin uzun parolası"})); err != nil {
		t.Fatal(err)
	}
	if u := visit(ayse, "/auth/oidc/kimlik/start?link=1&next=/o/kentos/account"); u.Path != "/o/kentos/account" || u.Query().Get("oidc_linked") != "kimlik" {
		t.Fatalf("link ended at %s", u)
	}
	// The linked identity re-authenticates Ayşe's session; another
	// account's identity does not.
	age()
	if u := visit(ayse, "/auth/oidc/kimlik/start?reauth=1&next=/o/kentos/account"); u.Path != "/o/kentos/account" || u.Query().Get("oidc_reauth") != "1" {
		t.Fatalf("re-authentication ended at %s", u)
	}
	var recent bool
	database.Pool.QueryRow(ctx, `SELECT bool_and(reauthenticated_at > now() - interval '1 minute') FROM sessions`).Scan(&recent)
	if !recent {
		t.Fatal("re-authentication did not refresh the session")
	}
	if u := visit(browser(), "/auth/oidc/kimlik/start?reauth=1&next=/o/kentos/account"); u.Path != "/login" {
		t.Fatalf("re-authentication without a session ended at %s", u)
	}
	phone := browser()
	if u := visit(phone, "/auth/oidc/kimlik/start?next=/o/kentos"); u.Path != "/o/kentos" {
		t.Fatalf("sign-in with the linked identity ended at %s", u)
	}
	sess, err := apiv1connect.NewAuthServiceClient(phone, izci.URL).GetSession(ctx, connect.NewRequest(&apiv1.GetSessionRequest{}))
	if err != nil || sess.Msg.GetSession().GetUser().GetEmail() != "ayse@kentos.com.tr" {
		t.Fatalf("session: %v %v", sess, err)
	}
	if len(sess.Msg.GetSession().GetMemberships()) != 1 {
		t.Fatalf("auto-join by the verified domain: %v", sess.Msg.GetSession().GetMemberships())
	}

	// The identity belongs to Ayşe now; nobody else can link it.
	idp.set("mehmet-at-kurum", "mehmet@kentos.com.tr", nil)
	mehmet := browser()
	if u := visit(mehmet, "/auth/oidc/kurum/start"); u.Path != "/" {
		t.Fatalf("trusted provider without email_verified ended at %s", u)
	}
	idp.set("ayse-at-kimlik", "mehmet@kentos.com.tr", boolPtr(true))
	if u := visit(mehmet, "/auth/oidc/kimlik/start?link=1&next=/o/kentos/account"); u.Query().Get("oidc_error") != "oidc_identity_taken" {
		t.Fatalf("linking someone else's identity ended at %s", u)
	}
	if u := visit(mehmet, "/auth/oidc/kimlik/start?reauth=1&next=/o/kentos/account"); u.Query().Get("oidc_error") != "oidc_reauth_mismatch" {
		t.Fatalf("re-authenticating with someone else's identity ended at %s", u)
	}

	// At a trusted provider a verified address links the existing account.
	idp.set("ayse-at-kurum", "ayse@kentos.com.tr", boolPtr(true))
	if u := visit(browser(), "/auth/oidc/kurum/start?next=/o/kentos"); u.Path != "/o/kentos" {
		t.Fatalf("trusted provider ended at %s", u)
	}
	var identities int
	database.Pool.QueryRow(ctx, `SELECT count(*) FROM oidc_identities i JOIN users u ON u.id = i.user_id WHERE u.email = 'ayse@kentos.com.tr'`).Scan(&identities)
	if identities != 2 || users() != 2 {
		t.Fatalf("identities %d, users %d", identities, users())
	}
}
