package github_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcihad/kentos-izci/internal/github"
)

func TestAppInstallationTokens(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	var issued atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/7/access_tokens" && r.Method == http.MethodPost:
			jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			parts := strings.Split(jwt, ".")
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
				http.Error(w, `{"message":"bad signature"}`, http.StatusUnauthorized)
				return
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var c struct {
				Iss string `json:"iss"`
			}
			_ = json.Unmarshal(claims, &c)
			if c.Iss != "42" {
				http.Error(w, `{"message":"wrong issuer"}`, http.StatusUnauthorized)
				return
			}
			issued.Add(1)
			fmt.Fprintf(w, `{"token":"ghs_install","expires_at":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case r.URL.Path == "/installation/repositories":
			if r.Header.Get("Authorization") != "token ghs_install" {
				http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"repositories":[{"name":"kalem","full_name":"kentos/kalem","default_branch":"main","owner":{"login":"kentos"}},
				{"name":"bildirim","full_name":"kentos/bildirim","default_branch":"develop","owner":{"login":"kentos"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app, err := github.NewApp(github.AppConfig{ID: 42, Slug: "izci-test", PrivateKey: pemKey, APIURL: srv.URL,
		WebURL: "https://github.test"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c := app.Client(7)
	repos, err := c.ListRepositories(context.Background(), "BILD")
	if err != nil || len(repos) != 1 || repos[0].FullName != "kentos/bildirim" || repos[0].DefaultBranch != "develop" {
		t.Fatalf("repositories: %+v %v", repos, err)
	}
	if _, err := c.ListRepositories(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if issued.Load() != 1 {
		t.Fatalf("installation token requested %d times, want 1 (cached)", issued.Load())
	}
	if !strings.HasPrefix(app.InstallURL("s t"), "https://github.test/apps/izci-test/installations/new?state=s+t") {
		t.Fatalf("install url: %s", app.InstallURL("s t"))
	}
}

func TestTokenClient(t *testing.T) {
	var created map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token github_pat_ok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"cihad","avatar_url":"https://avatars.test/c"}`))
		case "/repos/kentos/kalem/pulls":
			_ = json.NewDecoder(r.Body).Decode(&created)
			if created["base"] == "missing" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"base branch does not exist"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"number":12,"html_url":"https://github.test/kentos/kalem/pull/12","state":"open","draft":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	if _, err := github.NewTokenClient(srv.URL, "wrong", srv.Client()).CurrentUser(ctx); !github.Unauthorized(err) {
		t.Fatalf("bad token: %v", err)
	}
	c := github.NewTokenClient(srv.URL, "github_pat_ok", srv.Client())
	me, err := c.CurrentUser(ctx)
	if err != nil || me.Login != "cihad" {
		t.Fatalf("user: %v %v", me, err)
	}
	pr, err := c.CreatePullRequest(ctx, "kentos", "kalem", github.NewPullRequest{Title: "fix", Head: "izci/kalem-1a", Base: "main", Draft: true})
	if err != nil || pr.Number != 12 || !pr.Draft || created["head"] != "izci/kalem-1a" || created["draft"] != true {
		t.Fatalf("pull request: %+v %v %v", pr, err, created)
	}
	_, err = c.CreatePullRequest(ctx, "kentos", "kalem", github.NewPullRequest{Title: "fix", Head: "x", Base: "missing"})
	if err == nil || !strings.Contains(err.Error(), "base branch does not exist") {
		t.Fatalf("validation error: %v", err)
	}
	if _, err := c.GetRepository(ctx, "kentos", "yok"); !github.Forbidden(err) {
		t.Fatalf("missing repository: %v", err)
	}
}
