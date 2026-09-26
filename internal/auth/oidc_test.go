package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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
}

type codeInfo struct{ nonce, challenge, redirect string }

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := &fakeIdP{key: key, codes: map[string]codeInfo{}, sub: "kimlik-user-42", email: "Zeynep@Kentos.com.tr"}
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
		claims, _ := json.Marshal(map[string]any{
			"iss": p.srv.URL, "aud": "izci", "sub": p.sub, "email": p.email, "email_verified": true,
			"name": "Zeynep Kaya", "nonce": info.nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		})
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
