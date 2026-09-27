package github

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Access turns stored integrations into API clients.
type Access struct {
	APIURL string
	WebURL string
	// App is the configured GitHub App, nil without one.
	App  *App
	box  *secret.Box
	http *http.Client
}

// NewAccess creates the integration resolver.
func NewAccess(apiURL, webURL string, app *App, box *secret.Box, hc *http.Client) *Access {
	return &Access{APIURL: apiURL, WebURL: webURL, App: app, box: box, http: orDefault(hc)}
}

// TokenAAD binds a sealed token to its integration row.
func TokenAAD(id uuid.UUID) []byte { return []byte("integration:" + id.String()) }

// SealToken encrypts a personal access token for storage.
func (a *Access) SealToken(id uuid.UUID, token string) []byte {
	return a.box.Seal([]byte(token), TokenAAD(id))
}

// ErrAppNotConfigured means an App integration exists but the server has no
// GitHub App configured any more.
var ErrAppNotConfigured = errors.New("github app is not configured on this server")

// Client returns an API client for an integration.
func (a *Access) Client(i *store.Integration) (*Client, error) {
	switch i.Auth {
	case store.IntegrationToken:
		token, err := a.box.Open(i.Secret, TokenAAD(i.ID))
		if err != nil {
			return nil, err
		}
		return NewTokenClient(a.APIURL, string(token), a.http), nil
	case store.IntegrationApp:
		if a.App == nil || i.InstallationID == nil {
			return nil, ErrAppNotConfigured
		}
		return a.App.Client(*i.InstallationID), nil
	default:
		return nil, errors.New("unknown integration kind")
	}
}

// Verify checks a token and returns its owner.
func (a *Access) Verify(ctx context.Context, token string) (*Account, error) {
	return NewTokenClient(a.APIURL, token, a.http).CurrentUser(ctx)
}
