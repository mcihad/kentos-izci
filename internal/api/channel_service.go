package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/store"
)

// channelDetails fills the kind-specific fields of a channel from its
// stored settings; secrets never leave the server.
func channelDetails(out *apiv1.Channel, c *store.Channel) {
	switch c.Kind {
	case store.ChannelTelegram:
		out.Telegram = &apiv1.TelegramChat{ChatId: c.Telegram.ChatID, ThreadId: c.Telegram.ThreadID, Title: c.Telegram.Title,
			Type: c.Telegram.Type, Username: c.Telegram.Username}
	case store.ChannelEmail:
		var cfg notify.EmailConfig
		_ = json.Unmarshal(c.Config, &cfg)
		out.EmailRecipients = cfg.Recipients
	case store.ChannelSlack, store.ChannelTeams, store.ChannelWebhook:
		var cfg notify.WebhookConfig
		_ = json.Unmarshal(c.Config, &cfg)
		out.UrlHint, out.Signed = cfg.URLHint, cfg.SecretSealed != ""
	case store.ChannelJira:
		var cfg notify.JiraConfig
		_ = json.Unmarshal(c.Config, &cfg)
		out.Jira = &apiv1.JiraSettings{BaseUrl: cfg.BaseURL, ProjectKey: cfg.ProjectKey, IssueType: cfg.IssueType,
			Email: cfg.Email, Labels: cfg.Labels}
	}
}

// jiraFailure reports a failed Jira call with the service, its status and
// its own message as metadata, so that the UI can explain it.
func jiraFailure(what string, err error) error {
	meta := []string{"service", "jira"}
	var se *notify.StatusError
	if errors.As(err, &se) {
		meta = append(meta, "status", strconv.Itoa(se.Status), "detail", se.Detail)
	} else if errors.Is(err, notify.ErrPrivateAddress) || strings.Contains(err.Error(), "NOTIFY_ALLOW_PRIVATE_NETWORKS") {
		meta = append(meta, "blocked", "private_network")
	} else {
		meta = append(meta, "status", "0")
	}
	return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED, what+": "+err.Error(), meta...)
}

func errChannelUnavailable(msg string) error {
	return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_CHANNEL_UNAVAILABLE, msg)
}

// channelAvailable refuses kinds this server cannot deliver.
func (b *base) channelAvailable(kind int16) error {
	switch kind {
	case store.ChannelTelegram:
		if b.notifier == nil {
			return errTelegramNotConfigured()
		}
	case store.ChannelEmail:
		if b.email == nil {
			return errChannelUnavailable("no SMTP server is configured on this server (SMTP_HOST)")
		}
	}
	return nil
}

// channelConfig validates settings and builds the stored JSON, sealing
// secrets for the channel id. With current (an update) empty secrets keep
// the current ones. It returns the kind and, for new webhooks, the signing
// secret to show once.
func (b *base) channelConfig(ctx context.Context, id uuid.UUID, in *apiv1.ChannelSettings, current *store.Channel) (int16, []byte, string, error) {
	box := b.auth.Box()
	var kind int16
	var cfg any
	var shownSecret string
	switch k := in.GetKind().(type) {
	case *apiv1.ChannelSettings_Email:
		kind = store.ChannelEmail
		if err := b.channelAvailable(kind); err != nil {
			return 0, nil, "", err
		}
		var recipients []string
		for _, r := range k.Email.GetRecipients() {
			a, err := mail.ParseAddress(strings.TrimSpace(r))
			if err != nil {
				return 0, nil, "", fieldError("settings.email.recipients", "invalid e-mail address: "+r)
			}
			if addr := strings.ToLower(a.Address); !slices.Contains(recipients, addr) {
				recipients = append(recipients, addr)
			}
		}
		if len(recipients) == 0 || len(recipients) > notify.MaxEmailRecipients {
			return 0, nil, "", fieldError("settings.email.recipients", "1 to 20 recipients are required")
		}
		cfg = notify.EmailConfig{Recipients: recipients}
	case *apiv1.ChannelSettings_Slack, *apiv1.ChannelSettings_Teams, *apiv1.ChannelSettings_Webhook:
		var raw, field string
		httpsOnly := true
		switch k := k.(type) {
		case *apiv1.ChannelSettings_Slack:
			kind, raw, field = store.ChannelSlack, k.Slack.GetUrl(), "settings.slack.url"
		case *apiv1.ChannelSettings_Teams:
			kind, raw, field = store.ChannelTeams, k.Teams.GetUrl(), "settings.teams.url"
		case *apiv1.ChannelSettings_Webhook:
			kind, raw, field, httpsOnly = store.ChannelWebhook, k.Webhook.GetUrl(), "settings.webhook.url", false
		}
		var old notify.WebhookConfig
		if current != nil {
			_ = json.Unmarshal(current.Config, &old)
		}
		wc := notify.WebhookConfig{URLSealed: old.URLSealed, URLHint: old.URLHint, SecretSealed: old.SecretSealed}
		if strings.TrimSpace(raw) != "" || current == nil {
			u, err := notify.ValidateURL(raw, httpsOnly)
			if err != nil {
				return 0, nil, "", fieldError(field, err.Error())
			}
			wc.URLSealed, wc.URLHint = notify.Seal(box, id, u.String()), notify.URLHint(u.String())
		}
		if kind == store.ChannelWebhook && wc.SecretSealed == "" {
			shownSecret = notify.NewWebhookSecret()
			wc.SecretSealed = notify.Seal(box, id, shownSecret)
		}
		cfg = wc
	case *apiv1.ChannelSettings_Jira:
		kind = store.ChannelJira
		j := k.Jira
		u, err := notify.ValidateURL(j.GetBaseUrl(), false)
		if err != nil {
			return 0, nil, "", fieldError("settings.jira.base_url", err.Error())
		}
		jc := notify.JiraConfig{BaseURL: strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"),
			ProjectKey: strings.ToUpper(strings.TrimSpace(j.GetProjectKey())), IssueType: strings.TrimSpace(j.GetIssueType()),
			Email: strings.TrimSpace(j.GetEmail())}
		if jc.IssueType == "" {
			jc.IssueType = "Bug"
		}
		for _, l := range j.GetLabels() {
			if l = strings.Join(strings.Fields(l), "-"); l != "" && !slices.Contains(jc.Labels, l) {
				jc.Labels = append(jc.Labels, l)
			}
		}
		token := strings.TrimSpace(j.GetToken())
		if token == "" && current != nil {
			var old notify.JiraConfig
			_ = json.Unmarshal(current.Config, &old)
			if token, err = notify.Open(box, id, old.TokenSealed); err != nil {
				return 0, nil, "", fieldError("settings.jira.token", "enter the token again")
			}
		}
		if token == "" {
			return 0, nil, "", fieldError("settings.jira.token", "a Jira API token or personal access token is required")
		}
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := b.jira.CheckJira(checkCtx, jc, token); err != nil {
			return 0, nil, "", jiraFailure("Jira check failed", err)
		}
		jc.TokenSealed = notify.Seal(box, id, token)
		cfg = jc
	default:
		return 0, nil, "", fieldError("settings", "choose a destination")
	}
	raw, err := json.Marshal(cfg)
	return kind, raw, shownSecret, err
}

// CreateChannel implements AlertService.CreateChannel.
func (s *AlertServer) CreateChannel(ctx context.Context, req *connect.Request[apiv1.CreateChannelRequest]) (*connect.Response[apiv1.CreateChannelResponse], error) {
	id := uuid.Must(uuid.NewV7())
	var out apiv1.CreateChannelResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		c := &store.Channel{ID: id, OrgID: t.OrgID(), Name: strings.TrimSpace(req.Msg.GetName())}
		if slug := strings.ToLower(req.Msg.GetProject()); slug != "" {
			p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), slug)
			if errors.Is(err, store.ErrNotFound) {
				return errNotFound("project")
			}
			if err != nil {
				return err
			}
			c.ProjectID = &p.ID
		}
		kind, cfg, secretValue, err := s.channelConfig(ctx, id, req.Msg.GetSettings(), nil)
		if err != nil {
			return err
		}
		c.Kind, c.Config, out.WebhookSecret = kind, cfg, secretValue
		if p := auth.PrincipalFrom(ctx); p != nil {
			c.CreatedBy = &p.User.ID
		}
		saved, err := store.InsertChannel(ctx, tx, c)
		if err != nil {
			return err
		}
		// Jira tickets are opened on purpose; other first channels get the
		// default rules, as the first Telegram chat does.
		if saved.Kind != store.ChannelJira {
			if err := store.EnsureDefaultRules(ctx, tx, t.OrgID(), saved.ID); err != nil {
				return err
			}
		}
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		out.Channel = toChannel(saved, projects, nil)
		return s.audit(ctx, tx, "channel.create", "channel", saved.ID.String(), saved.Name,
			map[string]string{"kind": notify.KindName(saved.Kind)})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create channel", err)
	}
	s.rulesChanged(auth.TenantFrom(ctx).OrgID())
	return connect.NewResponse(&out), nil
}
