package notify

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// WebhookConfig is what Slack, Teams and webhook channels store. The URL
// carries the credential of the destination, so it is sealed.
type WebhookConfig struct {
	URLSealed string `json:"url_sealed"`
	// URLHint shows where the channel posts ("hooks.slack.com/…/x7Qa").
	URLHint string `json:"url_hint"`
	// SecretSealed signs webhook deliveries (webhook channels only).
	SecretSealed string `json:"secret_sealed,omitempty"`
}

// NewWebhookSecret returns a random signing secret for a webhook channel.
func NewWebhookSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b)
}

// Webhooks sends to Slack, Microsoft Teams and generic webhooks.
type Webhooks struct {
	box *secret.Box
	hc  *http.Client
}

// NewWebhooks creates the webhook senders.
func NewWebhooks(box *secret.Box, hc *http.Client) *Webhooks { return &Webhooks{box: box, hc: hc} }

// Slack is the sender of Slack channels (incoming webhooks).
func (w *Webhooks) Slack() Sender { return senderFunc(w.slack) }

// Teams is the sender of Microsoft Teams channels (Workflows or incoming
// webhook URLs).
func (w *Webhooks) Teams() Sender { return senderFunc(w.teams) }

// Generic is the sender of webhook channels.
func (w *Webhooks) Generic() Sender { return senderFunc(w.generic) }

type senderFunc func(ctx context.Context, n *Notification) (*Receipt, error)

func (f senderFunc) Send(ctx context.Context, n *Notification) (*Receipt, error) { return f(ctx, n) }

func (w *Webhooks) config(n *Notification) (WebhookConfig, string, error) {
	var cfg WebhookConfig
	if err := json.Unmarshal(n.Channel.Config, &cfg); err != nil {
		return cfg, "", Permanent("invalid channel settings: %v", err)
	}
	u, err := Open(w.box, n.Channel.ID, cfg.URLSealed)
	if err != nil {
		return cfg, "", Permanent("the channel's URL cannot be decrypted (was IZCI_SECRET_KEY changed?)")
	}
	return cfg, u, nil
}

// ── Slack ──────────────────────────────────────────────────────────────────

// slackEscape escapes text for Slack mrkdwn.
func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// slackCode puts text in a code span.
func slackCode(s string) string { return "`" + slackEscape(strings.ReplaceAll(s, "`", "'")) + "`" }

func mrkdwn(text string) map[string]any { return map[string]any{"type": "mrkdwn", "text": text} }

// SlackMessage builds a Block Kit message.
func SlackMessage(n *Notification) map[string]any {
	if n.Test() {
		text := "✅ *Test bildirimi*\n" + slackEscape(testText(n.Channel.Name, n.OrgName))
		return map[string]any{"text": "Test bildirimi", "blocks": []any{map[string]any{"type": "section", "text": mrkdwn(text)}}}
	}
	v := n.Issue
	head := fmt.Sprintf("%s *%s*  <%s|%s>", LevelMark(v.Level), slackEscape(Headline(n.Item.Kind, n.Item.Payload)), n.Link,
		slackEscape(v.ShortID))
	if s := StatusText(v); s != "" {
		head += "\n_" + slackEscape(s) + "_"
	}
	body := "*" + slackEscape(Truncate(firstNonEmpty(v.Type, v.Title), 200)) + "*"
	if v.Type != "" && v.Value != "" {
		body += "\n" + slackEscape(Truncate(v.Value, 1500))
	}
	if loc := Location(v); loc != "" {
		body += "\n📍 " + slackCode(Truncate(loc, 300))
	}
	var facts []string
	for _, f := range Facts(v) {
		facts = append(facts, "*"+f.Label+":* "+slackEscape(Truncate(f.Value, 200)))
	}
	blocks := []any{
		map[string]any{"type": "section", "text": mrkdwn(head)},
		map[string]any{"type": "section", "text": mrkdwn(body)},
	}
	if len(facts) > 0 {
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(strings.Join(facts, "\n"))})
	}
	blocks = append(blocks, map[string]any{"type": "context", "elements": []any{mrkdwn(slackEscape(Stats(v)))}})
	return map[string]any{
		// The fallback text of notifications is mrkdwn too: escaped, an
		// error message cannot turn into a disguised link.
		"text":   slackEscape(fmt.Sprintf("%s: %s %s", Headline(n.Item.Kind, n.Item.Payload), v.ShortID, Truncate(Summary(v), 150))),
		"blocks": blocks,
	}
}

func (w *Webhooks) slack(ctx context.Context, n *Notification) (*Receipt, error) {
	_, u, err := w.config(n)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(SlackMessage(n))
	_, err = post(ctx, w.hc, u, body, nil)
	return nil, err
}

// ── Microsoft Teams ────────────────────────────────────────────────────────

// teamsText is a block of text exactly as given. Teams renders Markdown in
// TextBlocks and fact values, but not in the TextRuns of a RichTextBlock:
// an error message like "[Hesabınız kilitlendi](https://…)" stays text
// instead of becoming a disguised link. run styles the text (weight, size,
// isSubtle, fontType), block places it (spacing).
func teamsText(s string, run, block map[string]any) map[string]any {
	r := map[string]any{"type": "TextRun", "text": s}
	maps.Copy(r, run)
	b := map[string]any{"type": "RichTextBlock", "inlines": []any{r}}
	maps.Copy(b, block)
	return b
}

// TeamsMessage builds a message with an Adaptive Card, the format both
// Teams Workflows webhooks and the older incoming webhooks accept. Nothing
// in it is Markdown.
func TeamsMessage(n *Notification) map[string]any {
	var body []any
	var actions []any
	if n.Test() {
		body = []any{
			teamsText("✅ Test bildirimi", map[string]any{"weight": "Bolder", "size": "Medium"}, nil),
			teamsText(testText(n.Channel.Name, n.OrgName), nil, nil),
		}
	} else {
		v := n.Issue
		body = append(body, teamsText(fmt.Sprintf("%s %s: %s", LevelMark(v.Level), Headline(n.Item.Kind, n.Item.Payload), v.ShortID),
			map[string]any{"weight": "Bolder", "size": "Medium"}, nil))
		if s := StatusText(v); s != "" {
			body = append(body, teamsText(s, map[string]any{"isSubtle": true}, map[string]any{"spacing": "None"}))
		}
		body = append(body, teamsText(Truncate(firstNonEmpty(v.Type, v.Title), 200), map[string]any{"weight": "Bolder"}, nil))
		if v.Type != "" && v.Value != "" {
			body = append(body, teamsText(Truncate(v.Value, 1500), nil, map[string]any{"spacing": "Small"}))
		}
		if loc := Location(v); loc != "" {
			body = append(body, teamsText(Truncate(loc, 300), map[string]any{"fontType": "Monospace", "isSubtle": true}, nil))
		}
		// Facts as text runs rather than a FactSet, whose values are
		// Markdown.
		for i, f := range Facts(v) {
			spacing := "None"
			if i == 0 {
				spacing = "Medium"
			}
			body = append(body, map[string]any{"type": "RichTextBlock", "spacing": spacing, "inlines": []any{
				map[string]any{"type": "TextRun", "text": f.Label + ": ", "weight": "Bolder"},
				map[string]any{"type": "TextRun", "text": Truncate(f.Value, 200)},
			}})
		}
		body = append(body, teamsText(Stats(v), map[string]any{"isSubtle": true, "size": "Small"}, nil))
		actions = append(actions, map[string]any{"type": "Action.OpenUrl", "title": "Hatayı aç", "url": n.Link})
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"msteams": map[string]any{"width": "Full"},
		"body":    body,
	}
	if len(actions) > 0 {
		card["actions"] = actions
	}
	return map[string]any{
		"type": "message",
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"contentUrl":  nil,
			"content":     card,
		}},
	}
}

func (w *Webhooks) teams(ctx context.Context, n *Notification) (*Receipt, error) {
	_, u, err := w.config(n)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(TeamsMessage(n))
	_, err = post(ctx, w.hc, u, body, nil)
	return nil, err
}

// ── Generic webhooks ───────────────────────────────────────────────────────

// WebhookEvent names a notification in webhook payloads and the
// X-Izci-Event header.
func WebhookEvent(kind int16) string {
	switch kind {
	case store.NotifyRegression:
		return "issue.regressed"
	case store.NotifyFrequency:
		return "issue.frequency"
	case store.NotifyTest:
		return "test"
	}
	return "issue.created"
}

// WebhookPayload is the JSON a webhook receives.
func WebhookPayload(n *Notification, sentAt time.Time) map[string]any {
	out := map[string]any{
		"event":        WebhookEvent(n.Item.Kind),
		"delivery_id":  n.Item.ID.String(),
		"sent_at":      sentAt.UTC().Format(time.RFC3339),
		"channel":      map[string]any{"id": n.Channel.ID.String(), "name": n.Channel.Name},
		"organization": map[string]any{"name": n.OrgName},
	}
	if n.Test() {
		out["message"] = testText(n.Channel.Name, n.OrgName)
		return out
	}
	v := n.Issue
	out["organization"] = map[string]any{"slug": v.OrgSlug, "name": v.OrgName}
	out["project"] = map[string]any{"slug": v.ProjectSlug, "name": v.ProjectName}
	issue := map[string]any{
		"id": v.ID, "short_id": v.ShortID, "url": n.Link, "title": v.Title, "type": v.Type, "value": v.Value,
		"culprit": v.Culprit, "filename": v.Filename, "lineno": v.Lineno, "level": LevelName(v.Level),
		"status": StatusName(v.Status), "unhandled": v.Unhandled, "times_seen": v.TimesSeen, "users": v.Users,
		"customers": v.Customers, "first_seen": v.FirstSeen.UTC().Format(time.RFC3339),
		"last_seen": v.LastSeen.UTC().Format(time.RFC3339),
	}
	out["issue"] = issue
	sample := map[string]any{}
	for k, val := range map[string]string{"event_id": v.EventID, "environment": v.Environment, "release": v.Release,
		"customer": v.Customer, "tenant": v.Tenant, "user": v.User} {
		if val != "" {
			sample[k] = val
		}
	}
	if len(sample) > 0 {
		out["latest_event"] = sample
	}
	if n.Item.Kind == store.NotifyFrequency {
		threshold, _ := n.Item.Payload["threshold"].(float64)
		window, _ := n.Item.Payload["window_seconds"].(float64)
		out["trigger"] = map[string]any{"threshold": int(threshold), "window_seconds": int(window)}
	}
	if n.Item.RuleID != nil {
		out["rule_id"] = n.Item.RuleID.String()
	}
	return out
}

// Signature signs a webhook delivery: HMAC-SHA256 of "timestamp.body" with
// the channel's secret, hex encoded, sent as "X-Izci-Signature: v1=<hex>".
func Signature(secretKey string, timestamp int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secretKey))
	m.Write([]byte(strconv.FormatInt(timestamp, 10)))
	m.Write([]byte("."))
	m.Write(body)
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}

func (w *Webhooks) generic(ctx context.Context, n *Notification) (*Receipt, error) {
	cfg, u, err := w.config(n)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	body, _ := json.Marshal(WebhookPayload(n, now))
	header := http.Header{
		"X-Izci-Event":    {WebhookEvent(n.Item.Kind)},
		"X-Izci-Delivery": {n.Item.ID.String()},
	}
	if cfg.SecretSealed != "" {
		key, err := Open(w.box, n.Channel.ID, cfg.SecretSealed)
		if err != nil {
			return nil, Permanent("the channel's signing secret cannot be decrypted (was IZCI_SECRET_KEY changed?)")
		}
		ts := now.Unix()
		header.Set("X-Izci-Timestamp", strconv.FormatInt(ts, 10))
		header.Set("X-Izci-Signature", Signature(key, ts, body))
	}
	_, err = post(ctx, w.hc, u, body, header)
	return nil, err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
