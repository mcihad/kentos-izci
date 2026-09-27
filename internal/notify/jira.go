package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// JiraConfig is what Jira channels store.
type JiraConfig struct {
	// BaseURL of the site, e.g. "https://kentos.atlassian.net".
	BaseURL    string `json:"base_url"`
	ProjectKey string `json:"project_key"`
	// IssueType is the name of the issue type to create, e.g. "Bug".
	IssueType string   `json:"issue_type"`
	Labels    []string `json:"labels,omitempty"`
	// Email signs in with an API token (Jira Cloud); without it the token
	// is a personal access token (Jira Server and Data Center).
	Email       string `json:"email,omitempty"`
	TokenSealed string `json:"token_sealed"`
}

// Jira creates Jira issues from izci issues: automatically through alert
// rules (a later notification of the same issue comments on its ticket)
// or by hand from the issue page.
type Jira struct {
	box *secret.Box
	hc  *http.Client
}

// NewJira creates the Jira sender.
func NewJira(box *secret.Box, hc *http.Client) *Jira { return &Jira{box: box, hc: hc} }

type jiraClient struct {
	hc   *http.Client
	base string
	auth string
	cfg  JiraConfig
}

func (j *Jira) client(ch *store.Channel) (*jiraClient, error) {
	var cfg JiraConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return nil, Permanent("invalid channel settings: %v", err)
	}
	token, err := Open(j.box, ch.ID, cfg.TokenSealed)
	if err != nil {
		return nil, Permanent("the channel's Jira token cannot be decrypted (was IZCI_SECRET_KEY changed?)")
	}
	c := &jiraClient{hc: j.hc, base: strings.TrimRight(cfg.BaseURL, "/"), cfg: cfg}
	if cfg.Email != "" {
		c.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Email+":"+token))
	} else {
		c.auth = "Bearer " + token
	}
	return c, nil
}

// jiraError reads Jira's {"errorMessages": [...], "errors": {...}}.
type jiraError struct {
	Messages []string          `json:"errorMessages"`
	Errors   map[string]string `json:"errors"`
}

func (c *jiraClient) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return Permanent("invalid Jira address: %v", err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivateAddress) {
			return &PermanentError{Reason: ErrPrivateAddress.Error()}
		}
		return fmt.Errorf("Jira %s: %w", req.URL.Host, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	var je jiraError
	_ = json.Unmarshal(raw, &je)
	parts := append([]string{}, je.Messages...)
	for field, msg := range je.Errors {
		parts = append(parts, field+": "+msg)
	}
	se := &StatusError{Service: "jira", Status: res.StatusCode, Detail: strings.Join(parts, "; ")}
	switch {
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return &RetryError{After: retryAfter(res.Header.Get("Retry-After")), Reason: se.Error(), Err: se}
	case res.StatusCode == http.StatusUnauthorized:
		return &PermanentError{Reason: "Jira rejected the credentials (" + se.Error() + ")", Err: se}
	}
	return &PermanentError{Reason: se.Error(), Err: se}
}

// Check verifies the credentials and that the project exists.
func (c *jiraClient) check(ctx context.Context) error {
	if err := c.call(ctx, http.MethodGet, "/rest/api/2/myself", nil, nil); err != nil {
		return err
	}
	return c.call(ctx, http.MethodGet, "/rest/api/2/project/"+url.PathEscape(c.cfg.ProjectKey), nil, nil)
}

// wikiEscape keeps Jira wiki markup from interpreting user text.
func wikiEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\*_{}[]|^~+-#!?`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// JiraSummary is the ticket title: "KALEM-1A: NullReferenceException: …".
func JiraSummary(v *IssueView) string {
	s := strings.Join(strings.Fields(v.ShortID+": "+Summary(v)), " ")
	return Truncate(s, 250)
}

// JiraDescription is the ticket body in Jira wiki markup.
func JiraDescription(v *IssueView, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "İzci'de %s hatası: [%s|%s]\n\n", v.ShortID, wikiEscape(link), link)
	if v.Type != "" {
		fmt.Fprintf(&b, "*%s*\n", wikiEscape(v.Type))
	}
	if msg := firstNonEmpty(valueOf(v), v.Title); msg != "" {
		fmt.Fprintf(&b, "{noformat}\n%s\n{noformat}\n", strings.ReplaceAll(Truncate(msg, 4000), "{noformat}", "{ noformat}"))
	}
	if loc := Location(v); loc != "" {
		fmt.Fprintf(&b, "*Konum:* {{%s}}\n", strings.ReplaceAll(loc, "}}", "} }"))
	}
	for _, f := range Facts(v) {
		fmt.Fprintf(&b, "*%s:* %s\n", f.Label, wikiEscape(f.Value))
	}
	fmt.Fprintf(&b, "*Görülme:* %s, ilk %s, son %s\n", Stats(v), v.FirstSeen.UTC().Format("2006-01-02 15:04 UTC"),
		v.LastSeen.UTC().Format("2006-01-02 15:04 UTC"))
	return b.String()
}

// create opens a ticket for an issue.
func (c *jiraClient) create(ctx context.Context, v *IssueView, link string) (*Ticket, error) {
	issueType := c.cfg.IssueType
	if issueType == "" {
		issueType = "Bug"
	}
	labels := append([]string{"izci"}, c.cfg.Labels...)
	in := map[string]any{"fields": map[string]any{
		"project":     map[string]any{"key": c.cfg.ProjectKey},
		"issuetype":   map[string]any{"name": issueType},
		"summary":     JiraSummary(v),
		"description": JiraDescription(v, link),
		"labels":      labels,
	}}
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := c.call(ctx, http.MethodPost, "/rest/api/2/issue", in, &out); err != nil {
		return nil, err
	}
	if out.Key == "" {
		return nil, errors.New("Jira created an issue without a key")
	}
	return &Ticket{Key: out.Key, URL: c.base + "/browse/" + out.Key}, nil
}

// comment adds a note about a later notification to an existing ticket.
func (c *jiraClient) comment(ctx context.Context, key, text string) error {
	return c.call(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", map[string]any{"body": text}, nil)
}

// Send creates a ticket for an issue's first notification in the channel
// and comments on it for regressions and frequency alerts. A test checks
// the credentials and the project without creating anything.
func (j *Jira) Send(ctx context.Context, n *Notification) (*Receipt, error) {
	c, err := j.client(n.Channel)
	if err != nil {
		return nil, err
	}
	if n.Test() {
		return nil, c.check(ctx)
	}
	v := n.Issue
	if n.Thread != nil {
		key, _ := n.Thread.Payload["key"].(string)
		if key != "" {
			if n.Item.Kind != store.NotifyNewIssue {
				text := fmt.Sprintf("İzci: %s. %s\n[Hatayı aç|%s]", Headline(n.Item.Kind, n.Item.Payload), Stats(v), n.Link)
				if err := c.comment(ctx, key, text); err != nil {
					return nil, err
				}
			}
			t := *n.Thread
			t.LastSentAt, t.LastCount, t.Kind = time.Now(), v.TimesSeen, n.Item.Kind
			return &Receipt{Thread: &t}, nil
		}
	}
	ticket, err := c.create(ctx, v, n.Link)
	if err != nil {
		return nil, err
	}
	return &Receipt{Thread: ticketThread(n.Channel.ID, v, ticket, n.Item.Kind), Ticket: ticket}, nil
}

func ticketThread(channelID uuid.UUID, v *IssueView, t *Ticket, kind int16) *store.Thread {
	id, _ := strconv.ParseInt(strings.TrimLeft(t.Key[strings.LastIndexByte(t.Key, '-')+1:], "0"), 10, 64)
	now := time.Now()
	return &store.Thread{ChannelID: channelID, IssueID: uuid.MustParse(v.ID), MessageID: id, LastSentAt: now,
		LastCount: v.TimesSeen, Kind: kind, Payload: map[string]any{"key": t.Key, "url": t.URL,
			"created_at": now.UTC().Format(time.RFC3339)}}
}

// CreateTicket opens a ticket for an issue by hand (from the issue page);
// the thread records it so rules comment on it later.
func (j *Jira) CreateTicket(ctx context.Context, ch *store.Channel, v *IssueView, link string) (*Ticket, *store.Thread, error) {
	c, err := j.client(ch)
	if err != nil {
		return nil, nil, err
	}
	ticket, err := c.create(ctx, v, link)
	if err != nil {
		return nil, nil, err
	}
	return ticket, ticketThread(ch.ID, v, ticket, store.NotifyNewIssue), nil
}

// CheckJira verifies Jira settings before a channel is saved.
func (j *Jira) CheckJira(ctx context.Context, cfg JiraConfig, token string) error {
	c := &jiraClient{hc: j.hc, base: strings.TrimRight(cfg.BaseURL, "/"), cfg: cfg}
	if cfg.Email != "" {
		c.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Email+":"+token))
	} else {
		c.auth = "Bearer " + token
	}
	return c.check(ctx)
}
