// Package github talks to GitHub (or GitHub Enterprise Server) for the
// features izci needs: verifying access, listing repositories and opening
// pull requests. Organizations authenticate with a personal access token or
// through a GitHub App installation; both yield a Client.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenSource returns a token for API and git calls.
type TokenSource func(ctx context.Context) (string, error)

// Client calls the GitHub REST API with one identity.
type Client struct {
	api   string
	http  *http.Client
	token TokenSource
	// installation marks App installation tokens (repository listing
	// uses a different endpoint).
	installation bool
}

// NewTokenClient authenticates with a personal access token.
func NewTokenClient(apiURL, token string, hc *http.Client) *Client {
	return &Client{api: strings.TrimRight(apiURL, "/"), http: orDefault(hc), token: func(context.Context) (string, error) { return token, nil }}
}

func orDefault(hc *http.Client) *http.Client {
	if hc == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return hc
}

// Token returns the token of the client (for git over HTTPS).
func (c *Client) Token(ctx context.Context) (string, error) { return c.token(ctx) }

// Error is an unsuccessful API response.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// Unauthorized reports a missing, expired or revoked token.
func Unauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// Forbidden reports a token without the needed permission (or a resource it
// cannot see, which GitHub reports as not found).
func Forbidden(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Status == http.StatusForbidden || e.Status == http.StatusNotFound)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	return request(ctx, c.http, method, c.api+path, "token "+token, body, out)
}

func request(ctx context.Context, hc *http.Client, method, u, authorization string, body, out any) error {
	var rd io.Reader
	if body != nil {
		js, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(js)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "izci")
	req.Header.Set("Authorization", authorization)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, req.URL.Path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		for _, x := range e.Errors {
			if x.Message != "" {
				msg += ": " + x.Message
			}
		}
		return &Error{Status: res.StatusCode, Message: msg}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Account is a GitHub user or organization.
type Account struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// CurrentUser returns the owner of a personal access token.
func (c *Client) CurrentUser(ctx context.Context) (*Account, error) {
	var a Account
	return &a, c.do(ctx, http.MethodGet, "/user", nil, &a)
}

// Repository is a GitHub repository.
type Repository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// ListRepositories returns repositories the identity can access whose name
// contains query (case-insensitive), at most 100.
func (c *Client) ListRepositories(ctx context.Context, query string) ([]Repository, error) {
	var repos []Repository
	for page := 1; page <= 5 && len(repos) < 500; page++ {
		var batch []Repository
		if c.installation {
			var res struct {
				Repositories []Repository `json:"repositories"`
			}
			if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), nil, &res); err != nil {
				return nil, err
			}
			batch = res.Repositories
		} else if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/user/repos?per_page=100&sort=pushed&page=%d", page), nil, &batch); err != nil {
			return nil, err
		}
		repos = append(repos, batch...)
		if len(batch) < 100 {
			break
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := repos[:0]
	for _, r := range repos {
		if q == "" || strings.Contains(strings.ToLower(r.FullName), q) {
			out = append(out, r)
		}
		if len(out) == 100 {
			break
		}
	}
	return out, nil
}

// GetRepository returns one repository.
func (c *Client) GetRepository(ctx context.Context, owner, name string) (*Repository, error) {
	var r Repository
	return &r, c.do(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), nil, &r)
}

// NewPullRequest describes a pull request to open.
type NewPullRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// PullRequest is an opened pull request.
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	Title   string `json:"title"`
}

// CreatePullRequest opens a pull request.
func (c *Client) CreatePullRequest(ctx context.Context, owner, name string, pr NewPullRequest) (*PullRequest, error) {
	var out PullRequest
	return &out, c.do(ctx, http.MethodPost, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/pulls", pr, &out)
}
