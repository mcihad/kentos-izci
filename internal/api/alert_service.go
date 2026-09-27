package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Notifier is the notification subsystem as seen by the API: the Telegram
// bot when one is configured.
type Notifier interface {
	// Username is the bot's @username (empty while it is unreachable).
	Username() string
	// Mode is how the bot receives updates ("polling" or "webhook").
	Mode() string
	// ChatLinkCode creates a code that links a chat to an organization.
	ChatLinkCode(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, userID uuid.UUID) (*apiv1.CreateTelegramLinkCodeResponse, error)
	// AccountLinkCode creates a code that links a Telegram account to a user.
	AccountLinkCode(ctx context.Context, userID uuid.UUID) (*apiv1.CreateTelegramAccountLinkCodeResponse, error)
}

// AlertServer implements izci.api.v1.AlertService.
type AlertServer struct {
	*base
	apiv1connect.UnimplementedAlertServiceHandler
}

var _ apiv1connect.AlertServiceHandler = (*AlertServer)(nil)

const defaultThrottle = 30 * time.Minute

func errTelegramNotConfigured() error {
	return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_TELEGRAM_NOT_CONFIGURED,
		"no Telegram bot is configured on this server")
}

func (s *AlertServer) rulesChanged(orgID uuid.UUID) {
	if s.alerts != nil {
		s.alerts.Invalidate(orgID)
	}
}

func toChannel(c *store.Channel, projects map[uuid.UUID]*apiv1.ProjectRef, users map[uuid.UUID]*apiv1.UserRef) *apiv1.Channel {
	out := &apiv1.Channel{
		Id: c.ID.String(), Kind: apiv1.ChannelKind(c.Kind), Name: c.Name, Muted: c.Muted, CreatedAt: ts(c.CreatedAt),
		LastDeliveryAt: tsp(c.LastDeliveryAt), LastError: c.LastError,
	}
	channelDetails(out, c)
	if c.ProjectID != nil {
		out.Project = projects[*c.ProjectID]
	}
	if c.CreatedBy != nil {
		out.CreatedBy = users[*c.CreatedBy]
	}
	return out
}

func toRule(r *store.AlertRule, projects map[uuid.UUID]*apiv1.ProjectRef) *apiv1.AlertRule {
	out := &apiv1.AlertRule{
		Id: r.ID.String(), Name: r.Name, Enabled: r.Enabled,
		Trigger: &apiv1.AlertTrigger{Kind: apiv1.TriggerKind(r.Trigger.Kind), Threshold: uint32(max(r.Trigger.Threshold, 0))},
		Filters: &apiv1.AlertFilters{MinLevel: ingestv1.Level(r.Filters.MinLevel), Environments: r.Filters.Environments,
			Customers: r.Filters.Customers, Tags: r.Filters.Tags, UnhandledOnly: r.Filters.UnhandledOnly},
		Throttle:  durationpb.New(time.Duration(r.ThrottleSeconds) * time.Second),
		CreatedAt: ts(r.CreatedAt), UpdatedAt: ts(r.UpdatedAt), LastTriggeredAt: tsp(r.LastTriggeredAt),
	}
	if r.Trigger.WindowSeconds > 0 {
		out.Trigger.Window = durationpb.New(time.Duration(r.Trigger.WindowSeconds) * time.Second)
	}
	for _, id := range r.ChannelIDs {
		out.ChannelIds = append(out.ChannelIds, id.String())
	}
	if r.ProjectID != nil {
		out.Project = projects[*r.ProjectID]
	}
	return out
}

// projectRefs maps project ids of the organization to references.
func projectRefs(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (map[uuid.UUID]*apiv1.ProjectRef, error) {
	list, err := store.ListProjects(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*apiv1.ProjectRef, len(list))
	for _, p := range list {
		out[p.ID] = &apiv1.ProjectRef{Id: p.ID.String(), Slug: p.Slug, Name: p.Name, Platform: p.Platform}
	}
	return out, nil
}

// ListAlertRules implements AlertService.ListAlertRules.
func (s *AlertServer) ListAlertRules(ctx context.Context, req *connect.Request[apiv1.ListAlertRulesRequest]) (*connect.Response[apiv1.ListAlertRulesResponse], error) {
	var out apiv1.ListAlertRulesResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		rules, err := store.ListAlertRules(ctx, tx, []uuid.UUID{t.OrgID()}, false)
		if err != nil {
			return err
		}
		filter := strings.ToLower(req.Msg.GetProject())
		for _, r := range rules {
			rule := toRule(r, projects)
			if filter != "" && rule.GetProject().GetSlug() != filter {
				continue
			}
			out.Rules = append(out.Rules, rule)
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list alert rules", err)
	}
	return connect.NewResponse(&out), nil
}

// ruleFromInput validates an AlertRuleInput against the organization.
func (s *AlertServer) ruleFromInput(ctx context.Context, tx pgx.Tx, t *auth.Tenant, in *apiv1.AlertRuleInput, r *store.AlertRule) error {
	r.OrgID, r.Name, r.Enabled = t.OrgID(), strings.TrimSpace(in.GetName()), in.GetEnabled()
	r.ProjectID = nil
	if slug := strings.ToLower(in.GetProject()); slug != "" {
		p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), slug)
		if errors.Is(err, store.ErrNotFound) {
			return fieldError("rule.project", "unknown project")
		}
		if err != nil {
			return err
		}
		r.ProjectID = &p.ID
	}
	tr := in.GetTrigger()
	r.Trigger = store.RuleTrigger{Kind: int16(tr.GetKind())}
	switch tr.GetKind() {
	case apiv1.TriggerKind_TRIGGER_KIND_NEW_ISSUE, apiv1.TriggerKind_TRIGGER_KIND_REGRESSION:
	case apiv1.TriggerKind_TRIGGER_KIND_FREQUENCY:
		w := tr.GetWindow().AsDuration()
		if tr.GetThreshold() < 1 || w < time.Minute || w > 24*time.Hour {
			return fieldError("rule.trigger", "frequency rules need a threshold and a window between 1 minute and 24 hours")
		}
		r.Trigger.Threshold, r.Trigger.WindowSeconds = int(tr.GetThreshold()), int(w.Seconds())
	default:
		return fieldError("rule.trigger.kind", "unknown trigger")
	}
	f := in.GetFilters()
	r.Filters = store.RuleFilters{
		MinLevel: int16(f.GetMinLevel()), Environments: lowerTrimmed(f.GetEnvironments()), Customers: lowerTrimmed(f.GetCustomers()),
		Tags: f.GetTags(), UnhandledOnly: f.GetUnhandledOnly(),
	}
	channels, err := store.ListChannels(ctx, tx, t.OrgID())
	if err != nil {
		return err
	}
	r.ChannelIDs = r.ChannelIDs[:0]
	for _, raw := range in.GetChannelIds() {
		id, _ := uuid.Parse(raw)
		if !slices.ContainsFunc(channels, func(c *store.Channel) bool { return c.ID == id }) {
			return fieldError("rule.channel_ids", "unknown channel")
		}
		if !slices.Contains(r.ChannelIDs, id) {
			r.ChannelIDs = append(r.ChannelIDs, id)
		}
	}
	throttle := defaultThrottle
	if in.Throttle != nil {
		throttle = in.GetThrottle().AsDuration()
	}
	if throttle < 0 || throttle > 7*24*time.Hour {
		return fieldError("rule.throttle", "throttle must be between 0 and 7 days")
	}
	r.ThrottleSeconds = int32(throttle.Seconds())
	return nil
}

func lowerTrimmed(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CreateAlertRule implements AlertService.CreateAlertRule.
func (s *AlertServer) CreateAlertRule(ctx context.Context, req *connect.Request[apiv1.CreateAlertRuleRequest]) (*connect.Response[apiv1.CreateAlertRuleResponse], error) {
	p := auth.PrincipalFrom(ctx)
	var out apiv1.CreateAlertRuleResponse
	var orgID uuid.UUID
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		orgID = t.OrgID()
		r := &store.AlertRule{CreatedBy: &p.User.ID}
		if err := s.ruleFromInput(ctx, tx, t, req.Msg.GetRule(), r); err != nil {
			return err
		}
		saved, err := store.SaveAlertRule(ctx, tx, r)
		if err != nil {
			return err
		}
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		out.Rule = toRule(saved, projects)
		return s.audit(ctx, tx, "alert_rule.create", "alert_rule", saved.ID.String(), saved.Name, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create alert rule", err)
	}
	s.rulesChanged(orgID)
	return connect.NewResponse(&out), nil
}

// UpdateAlertRule implements AlertService.UpdateAlertRule.
func (s *AlertServer) UpdateAlertRule(ctx context.Context, req *connect.Request[apiv1.UpdateAlertRuleRequest]) (*connect.Response[apiv1.UpdateAlertRuleResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	var out apiv1.UpdateAlertRuleResponse
	var orgID uuid.UUID
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		orgID = t.OrgID()
		r := &store.AlertRule{ID: id}
		if err := s.ruleFromInput(ctx, tx, t, req.Msg.GetRule(), r); err != nil {
			return err
		}
		saved, err := store.SaveAlertRule(ctx, tx, r)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("alert rule")
		}
		if err != nil {
			return err
		}
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		out.Rule = toRule(saved, projects)
		return s.audit(ctx, tx, "alert_rule.update", "alert_rule", saved.ID.String(), saved.Name, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update alert rule", err)
	}
	s.rulesChanged(orgID)
	return connect.NewResponse(&out), nil
}

// DeleteAlertRule implements AlertService.DeleteAlertRule.
func (s *AlertServer) DeleteAlertRule(ctx context.Context, req *connect.Request[apiv1.DeleteAlertRuleRequest]) (*connect.Response[apiv1.DeleteAlertRuleResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	var orgID uuid.UUID
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		orgID = t.OrgID()
		if err := store.DeleteAlertRule(ctx, tx, id); errors.Is(err, store.ErrNotFound) {
			return errNotFound("alert rule")
		} else if err != nil {
			return err
		}
		return s.audit(ctx, tx, "alert_rule.delete", "alert_rule", id.String(), "", nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete alert rule", err)
	}
	s.rulesChanged(orgID)
	return connect.NewResponse(&apiv1.DeleteAlertRuleResponse{}), nil
}

// ListChannels implements AlertService.ListChannels.
func (s *AlertServer) ListChannels(ctx context.Context, _ *connect.Request[apiv1.ListChannelsRequest]) (*connect.Response[apiv1.ListChannelsResponse], error) {
	var out apiv1.ListChannelsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		channels, err := store.ListChannels(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		var creators []uuid.UUID
		for _, c := range channels {
			if c.CreatedBy != nil {
				creators = append(creators, *c.CreatedBy)
			}
		}
		users, err := userRefs(ctx, tx, creators)
		if err != nil {
			return err
		}
		for _, c := range channels {
			out.Channels = append(out.Channels, toChannel(c, projects, users))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list channels", err)
	}
	return connect.NewResponse(&out), nil
}

// UpdateChannel implements AlertService.UpdateChannel.
func (s *AlertServer) UpdateChannel(ctx context.Context, req *connect.Request[apiv1.UpdateChannelRequest]) (*connect.Response[apiv1.UpdateChannelResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	var out apiv1.UpdateChannelResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if settings := req.Msg.GetSettings(); settings != nil {
			current, err := store.GetChannel(ctx, tx, id)
			if errors.Is(err, store.ErrNotFound) {
				return errNotFound("channel")
			}
			if err != nil {
				return err
			}
			kind, cfg, _, err := s.channelConfig(ctx, id, settings, current)
			if err != nil {
				return err
			}
			if kind != current.Kind {
				return fieldError("settings", "the channel kind cannot change")
			}
			if err := store.UpdateChannelConfig(ctx, tx, id, cfg); err != nil {
				return err
			}
		}
		c, err := store.UpdateChannel(ctx, tx, id, strings.TrimSpace(req.Msg.GetName()), req.Msg.GetMuted())
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("channel")
		}
		if err != nil {
			return err
		}
		projects, err := projectRefs(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		out.Channel = toChannel(c, projects, nil)
		return s.audit(ctx, tx, "channel.update", "channel", c.ID.String(), c.Name, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update channel", err)
	}
	return connect.NewResponse(&out), nil
}

// DeleteChannel implements AlertService.DeleteChannel.
func (s *AlertServer) DeleteChannel(ctx context.Context, req *connect.Request[apiv1.DeleteChannelRequest]) (*connect.Response[apiv1.DeleteChannelResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetId())
	var orgID uuid.UUID
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		orgID = t.OrgID()
		if err := store.DeleteChannel(ctx, tx, id); errors.Is(err, store.ErrNotFound) {
			return errNotFound("channel")
		} else if err != nil {
			return err
		}
		return s.audit(ctx, tx, "channel.delete", "channel", id.String(), "", nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete channel", err)
	}
	s.rulesChanged(orgID)
	return connect.NewResponse(&apiv1.DeleteChannelResponse{}), nil
}

// CreateTelegramLinkCode implements AlertService.CreateTelegramLinkCode.
func (s *AlertServer) CreateTelegramLinkCode(ctx context.Context, req *connect.Request[apiv1.CreateTelegramLinkCodeRequest]) (*connect.Response[apiv1.CreateTelegramLinkCodeResponse], error) {
	if s.notifier == nil {
		return nil, errTelegramNotConfigured()
	}
	t := auth.TenantFrom(ctx)
	var projectID *uuid.UUID
	if slug := strings.ToLower(req.Msg.GetProject()); slug != "" {
		err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
			p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), slug)
			if errors.Is(err, store.ErrNotFound) {
				return errNotFound("project")
			}
			if err == nil {
				projectID = &p.ID
			}
			return err
		})
		if err != nil {
			return nil, internal(ctx, s.log, "telegram link code", err)
		}
	}
	out, err := s.notifier.ChatLinkCode(ctx, t.OrgID(), projectID, auth.PrincipalFrom(ctx).User.ID)
	if err != nil {
		return nil, apiError(connect.CodeUnavailable, apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED, "the Telegram bot is not reachable")
	}
	return connect.NewResponse(out), nil
}

// SendTestNotification implements AlertService.SendTestNotification.
func (s *AlertServer) SendTestNotification(ctx context.Context, req *connect.Request[apiv1.SendTestNotificationRequest]) (*connect.Response[apiv1.SendTestNotificationResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetChannelId())
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		ch, err := store.GetChannel(ctx, tx, id)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("channel")
		} else if err != nil {
			return err
		}
		if err := s.channelAvailable(ch.Kind); err != nil {
			return err
		}
		return store.EnqueueNotifications(ctx, tx, []store.OutboxItem{{OrgID: t.OrgID(), ChannelID: id, Kind: store.NotifyTest}})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "test notification", err)
	}
	if s.delivery != nil {
		s.delivery.Wake()
	}
	return connect.NewResponse(&apiv1.SendTestNotificationResponse{}), nil
}

// GetNotificationStatus implements AlertService.GetNotificationStatus.
func (s *AlertServer) GetNotificationStatus(context.Context, *connect.Request[apiv1.GetNotificationStatusRequest]) (*connect.Response[apiv1.GetNotificationStatusResponse], error) {
	out := &apiv1.GetNotificationStatusResponse{}
	if s.notifier != nil {
		out.TelegramConfigured, out.TelegramBotUsername, out.TelegramMode = true, s.notifier.Username(), s.notifier.Mode()
	}
	if s.email != nil {
		out.EmailConfigured, out.EmailFrom = true, s.email.From()
	}
	return connect.NewResponse(out), nil
}
