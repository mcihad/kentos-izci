package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func TestAlertRulesAndChannels(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	_, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"}))
	if err != nil {
		t.Fatal(err)
	}
	st, err := owner.alert.GetNotificationStatus(ctx, req(&apiv1.GetNotificationStatusRequest{}))
	if err != nil || st.Msg.GetTelegramConfigured() {
		t.Fatalf("status without a bot: %v %v", st, err)
	}
	if _, err := owner.alert.CreateTelegramLinkCode(ctx, req(&apiv1.CreateTelegramLinkCodeRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TELEGRAM_NOT_CONFIGURED {
		t.Fatalf("link code without a bot: %v", err)
	}

	// Channels are created by the bot; simulate a linked group.
	var channelID string
	err = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.GetOrganizationBySlug(ctx, tx, "kentos")
		if err != nil {
			return err
		}
		ch, _, err := store.UpsertTelegramChannel(ctx, tx, org.ID, nil, nil, "KentOS Hatalar",
			store.TelegramTarget{ChatID: -100200300, Title: "KentOS Hatalar", Type: "supergroup"})
		channelID = ch.ID.String()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	chs, err := owner.alert.ListChannels(ctx, req(&apiv1.ListChannelsRequest{}))
	if err != nil || len(chs.Msg.GetChannels()) != 1 || chs.Msg.GetChannels()[0].GetTelegram().GetChatId() != -100200300 {
		t.Fatalf("channels: %v %v", chs, err)
	}

	input := func() *apiv1.AlertRuleInput {
		return &apiv1.AlertRuleInput{
			Name: "Kalem'de sık tekrar", Project: "kalem", Enabled: true,
			Trigger:    &apiv1.AlertTrigger{Kind: apiv1.TriggerKind_TRIGGER_KIND_FREQUENCY, Threshold: 50, Window: durationpb.New(time.Hour)},
			Filters:    &apiv1.AlertFilters{MinLevel: ingestv1.Level_LEVEL_ERROR, Environments: []string{" Production "}},
			ChannelIds: []string{channelID},
		}
	}
	bad := input()
	bad.ChannelIds = []string{"0190c0de-0000-7000-8000-000000000000"}
	if _, err := owner.alert.CreateAlertRule(ctx, req(&apiv1.CreateAlertRuleRequest{Rule: bad})); field(err) != "rule.channel_ids" {
		t.Fatalf("unknown channel: %v (%s)", err, field(err))
	}
	bad = input()
	bad.Trigger.Window = nil
	if _, err := owner.alert.CreateAlertRule(ctx, req(&apiv1.CreateAlertRuleRequest{Rule: bad})); field(err) != "rule.trigger" {
		t.Fatalf("frequency without window: %v", err)
	}
	created, err := owner.alert.CreateAlertRule(ctx, req(&apiv1.CreateAlertRuleRequest{Rule: input()}))
	if err != nil {
		t.Fatal(err)
	}
	r := created.Msg.GetRule()
	if r.GetProject().GetSlug() != "kalem" || r.GetFilters().GetEnvironments()[0] != "production" ||
		r.GetThrottle().AsDuration() != 30*time.Minute || r.GetTrigger().GetWindow().AsDuration() != time.Hour {
		t.Fatalf("created rule: %v", r)
	}

	upd := input()
	upd.Enabled = false
	upd.Throttle = durationpb.New(0)
	updated, err := owner.alert.UpdateAlertRule(ctx, req(&apiv1.UpdateAlertRuleRequest{Id: r.GetId(), Rule: upd}))
	if err != nil || updated.Msg.GetRule().GetEnabled() || updated.Msg.GetRule().GetThrottle().AsDuration() != 0 {
		t.Fatalf("update: %v %v", updated, err)
	}

	// Deleting a channel detaches it from rules.
	if _, err := owner.alert.DeleteChannel(ctx, req(&apiv1.DeleteChannelRequest{Id: channelID})); err != nil {
		t.Fatal(err)
	}
	rules, err := owner.alert.ListAlertRules(ctx, req(&apiv1.ListAlertRulesRequest{}))
	if err != nil || len(rules.Msg.GetRules()) != 1 || len(rules.Msg.GetRules()[0].GetChannelIds()) != 0 {
		t.Fatalf("rules after channel deletion: %v %v", rules, err)
	}
	if _, err := owner.alert.DeleteAlertRule(ctx, req(&apiv1.DeleteAlertRuleRequest{Id: r.GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.alert.DeleteAlertRule(ctx, req(&apiv1.DeleteAlertRuleRequest{Id: r.GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("second delete: %v", err)
	}
}
