package github

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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App is a GitHub App whose installations grant repository access.
type App struct {
	ID   int64
	Slug string

	api          string
	web          string
	key          *rsa.PrivateKey
	clientID     string
	clientSecret string
	http         *http.Client

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// AppConfig identifies a GitHub App and its OAuth client.
type AppConfig struct {
	ID   int64
	Slug string
	// PrivateKey is the PEM private key (PKCS#1 or PKCS#8).
	PrivateKey string
	// ClientID and ClientSecret verify who installed the app.
	ClientID     string
	ClientSecret string
	APIURL       string
	WebURL       string
}

// ErrInstallationNotAccessible means the user who came back from GitHub
// cannot access the installation named in the callback.
var ErrInstallationNotAccessible = errors.New("github app: the installation is not accessible to the signed-in GitHub user")

// NewApp parses the app's private key.
func NewApp(c AppConfig, hc *http.Client) (*App, error) {
	block, _ := pem.Decode([]byte(c.PrivateKey))
	if block == nil {
		return nil, errors.New("github app: private key is not PEM encoded")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = k
	} else {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("github app: parse private key: %w", err)
		}
		k, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("github app: private key is not RSA")
		}
		key = k
	}
	return &App{ID: c.ID, Slug: c.Slug, api: strings.TrimRight(c.APIURL, "/"), web: strings.TrimRight(c.WebURL, "/"), key: key,
		clientID: c.ClientID, clientSecret: c.ClientSecret, http: orDefault(hc), tokens: map[int64]cachedToken{}}, nil
}

// jwt signs a short-lived app token (RS256).
func (a *App) jwt() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(), // allow for clock drift
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(a.ID, 10),
	})
	signing := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// InstallationToken returns a cached installation access token.
func (a *App) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	a.mu.Lock()
	if t, ok := a.tokens[installationID]; ok && time.Until(t.expires) > 5*time.Minute {
		a.mu.Unlock()
		return t.token, nil
	}
	a.mu.Unlock()
	jwt, err := a.jwt()
	if err != nil {
		return "", err
	}
	var res struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := request(ctx, a.http, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", a.api, installationID),
		"Bearer "+jwt, nil, &res); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.tokens[installationID] = cachedToken{token: res.Token, expires: res.ExpiresAt}
	a.mu.Unlock()
	return res.Token, nil
}

// Installation returns the account an installation belongs to.
func (a *App) Installation(ctx context.Context, installationID int64) (*Account, error) {
	jwt, err := a.jwt()
	if err != nil {
		return nil, err
	}
	var res struct {
		Account Account `json:"account"`
	}
	err = request(ctx, a.http, http.MethodGet, fmt.Sprintf("%s/app/installations/%d", a.api, installationID), "Bearer "+jwt, nil, &res)
	return &res.Account, err
}

// Client returns a client acting as an installation.
func (a *App) Client(installationID int64) *Client {
	return &Client{api: a.api, http: a.http, installation: true,
		token: func(ctx context.Context) (string, error) { return a.InstallationToken(ctx, installationID) }}
}

// InstallURL is where an organization admin installs the app; GitHub sends
// state back to the app's setup URL.
func (a *App) InstallURL(state string) string {
	return a.web + "/apps/" + url.PathEscape(a.Slug) + "/installations/new?state=" + url.QueryEscape(state)
}

// VerifyInstaller exchanges the OAuth code GitHub sends back after an
// installation for a user token and returns the installation's account if
// that user can access it. Without this check a crafted callback could
// link another account's installation to an organization.
func (a *App) VerifyInstaller(ctx context.Context, code string, installationID int64) (*Account, error) {
	form := url.Values{"client_id": {a.clientID}, "client_secret": {a.clientSecret}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.web+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "izci")
	res, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github oauth: %w", err)
	}
	defer res.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tok); err != nil {
		return nil, fmt.Errorf("github oauth: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, &Error{Status: http.StatusUnauthorized, Message: strings.TrimSpace(tok.Error + " " + tok.Description)}
	}
	for page := 1; page <= 10; page++ {
		var list struct {
			Installations []struct {
				ID      int64   `json:"id"`
				Account Account `json:"account"`
			} `json:"installations"`
		}
		u := fmt.Sprintf("%s/user/installations?per_page=100&page=%d", a.api, page)
		if err := request(ctx, a.http, http.MethodGet, u, "token "+tok.AccessToken, nil, &list); err != nil {
			return nil, err
		}
		for _, in := range list.Installations {
			if in.ID == installationID {
				return &in.Account, nil
			}
		}
		if len(list.Installations) < 100 {
			break
		}
	}
	return nil, ErrInstallationNotAccessible
}
