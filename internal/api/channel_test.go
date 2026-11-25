package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/demo"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/store"
)

// fakeJira answers the calls izci makes: credentials, project, tickets.
func fakeJira(t *testing.T) (*httptest.Server, *[]map[string]any) {
	var mu sync.Mutex
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat-token" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"errorMessages":["Unauthorized"]}`))
			return
		}
		switch {
		case r.URL.Path == "/rest/api/2/myself", r.URL.Path == "/rest/api/2/project/KALEM":
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/rest/api/2/issue" && r.Method == http.MethodPost:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			created = append(created, in)
			mu.Unlock()
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"10001","key":"KALEM-7"}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &created
}

func TestChannels(t *testing.T) {
	email, err := notify.NewEmail(config.SMTPConfig{Host: "127.0.0.1", Port: 2525, From: "izci@kentos.example", TLS: "none"})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, func(_ *env, d *api.Deps) {
		d.Email = email
		d.Config.Notify.AllowPrivateNetworks = true // the fake Jira is on loopback
	})
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	status, err := owner.alert.GetNotificationStatus(ctx, req(&apiv1.GetNotificationStatusRequest{}))
	if err != nil || !status.Msg.GetEmailConfigured() || status.Msg.GetEmailFrom() != "izci@kentos.example" {
		t.Fatalf("status: %v %v", status, err)
	}

	mk := func(name string, s *apiv1.ChannelSettings) (*apiv1.CreateChannelResponse, error) {
		res, err := owner.alert.CreateChannel(ctx, req(&apiv1.CreateChannelRequest{Name: name, Settings: s}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	mail, err := mk("Ekip postası", &apiv1.ChannelSettings{Kind: &apiv1.ChannelSettings_Email{Email: &apiv1.EmailSettings{
		Recipients: []string{"Ali@Kentos.example", "ali@kentos.example", "ayse@kentos.example"}}}})
	if err != nil || mail.GetChannel().GetKind() != apiv1.ChannelKind_CHANNEL_KIND_EMAIL ||
		strings.Join(mail.GetChannel().GetEmailRecipients(), ",") != "ali@kentos.example,ayse@kentos.example" {
		t.Fatalf("email: %v %v", mail, err)
	}
	slack, err := mk("Slack", &apiv1.ChannelSettings{Kind: &apiv1.ChannelSettings_Slack{Slack: &apiv1.WebhookSettings{
		Url: "https://hooks.slack.com/services/T0/B0/secretpart1234"}}})
	if err != nil || slack.GetChannel().GetUrlHint() != "hooks.slack.com/…/1234" || slack.GetWebhookSecret() != "" {
		t.Fatalf("slack: %v %v", slack, err)
	}
	if _, err := mk("Düz", &apiv1.ChannelSettings{Kind: &apiv1.ChannelSettings_Teams{Teams: &apiv1.WebhookSettings{
		Url: "http://teams.example.com/hook"}}}); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("teams over http must be refused: %v", err)
	}
	hook, err := mk("Webhook", &apiv1.ChannelSettings{Kind: &apiv1.ChannelSettings_Webhook{Webhook: &apiv1.WebhookSettings{
		Url: "http://ic-sistem.example.com/izci"}}})
	if err != nil || !strings.HasPrefix(hook.GetWebhookSecret(), "whsec_") || !hook.GetChannel().GetSigned() {
		t.Fatalf("webhook: %v %v", hook, err)
	}
	jiraSrv, created := fakeJira(t)
	jiraSettings := func(token string) *apiv1.ChannelSettings {
		return &apiv1.ChannelSettings{Kind: &apiv1.ChannelSettings_Jira{Jira: &apiv1.JiraInput{BaseUrl: jiraSrv.URL + "/",
			ProjectKey: "kalem", Token: token, Labels: []string{"arka uç"}}}}
	}
	if _, err := mk("Jira", jiraSettings("wrong")); reason(err) != apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED ||
		!strings.Contains(err.Error(), "credentials") {
		t.Fatalf("bad token: %v", err)
	}
	jira, err := mk("Jira", jiraSettings("pat-token"))
	if err != nil || jira.GetChannel().GetJira().GetProjectKey() != "KALEM" || jira.GetChannel().GetJira().GetIssueType() != "Bug" ||
		jira.GetChannel().GetJira().GetBaseUrl() != jiraSrv.URL || strings.Join(jira.GetChannel().GetJira().GetLabels(), ",") != "arka-uç" {
		t.Fatalf("jira: %v %v", jira, err)
	}

	// Secrets are sealed: neither the URL nor the token is stored in clear.
	var raw string
	err = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT string_agg(config::text, ' ') FROM notification_channels`).Scan(&raw)
	})
	if err != nil || strings.Contains(raw, "secretpart1234") || strings.Contains(raw, "pat-token") || strings.Contains(raw, "whsec_") {
		t.Fatalf("stored settings leak secrets: %s %v", raw, err)
	}

	// Updating keeps secrets that are not sent again.
	id := jira.GetChannel().GetId()
	upd, err := owner.alert.UpdateChannel(ctx, req(&apiv1.UpdateChannelRequest{Id: id, Name: "Jira KALEM", Settings: jiraSettings("")}))
	if err != nil || upd.Msg.GetChannel().GetName() != "Jira KALEM" {
		t.Fatalf("update: %v %v", upd, err)
	}
	if _, err := owner.alert.UpdateChannel(ctx, req(&apiv1.UpdateChannelRequest{Id: id, Settings: &apiv1.ChannelSettings{
		Kind: &apiv1.ChannelSettings_Slack{Slack: &apiv1.WebhookSettings{}}}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("kind change: %v", err)
	}

	// A ticket by hand, once per issue and channel.
	if _, err := demo.Seed(ctx, e.db, e.log, demo.Options{OrgSlug: "kentos", Events: 60, Days: 1, Seed: 3}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	issue := listIssues(t, owner, "")[0].GetId()
	ticket, err := owner.issue.CreateTicket(ctx, req(&apiv1.CreateTicketRequest{Issue: issue, ChannelId: id}))
	if err != nil || !ticket.Msg.GetCreated() || ticket.Msg.GetTicket().GetKey() != "KALEM-7" ||
		ticket.Msg.GetTicket().GetUrl() != jiraSrv.URL+"/browse/KALEM-7" {
		t.Fatalf("ticket: %v %v", ticket, err)
	}
	again, err := owner.issue.CreateTicket(ctx, req(&apiv1.CreateTicketRequest{Issue: issue, ChannelId: id}))
	if err != nil || again.Msg.GetCreated() || len(*created) != 1 {
		t.Fatalf("second ticket: %v %v (%d created)", again, err, len(*created))
	}
	got, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: issue}))
	if err != nil || len(got.Msg.GetIssue().GetTickets()) != 1 || got.Msg.GetIssue().GetTickets()[0].GetChannelName() != "Jira KALEM" {
		t.Fatalf("issue tickets: %v %v", got, err)
	}
	activity, _ := owner.issue.ListIssueActivity(ctx, req(&apiv1.ListIssueActivityRequest{IssueId: issue}))
	found := false
	for _, a := range activity.Msg.GetActivities() {
		if a.GetKind() == apiv1.ActivityKind_ACTIVITY_KIND_TICKET_CREATED && a.GetData()["key"] == "KALEM-7" && a.GetActor() != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("ticket activity missing: %v", activity.Msg.GetActivities())
	}
	if _, err := owner.issue.CreateTicket(ctx, req(&apiv1.CreateTicketRequest{Issue: issue, ChannelId: slack.GetChannel().GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("ticket through a Slack channel: %v", err)
	}

	// Members may open tickets but not manage channels.
	member := joinAs(t, e, owner, "uye@example.com", apiv1.Role_ROLE_MEMBER)
	if _, err := member.alert.CreateChannel(ctx, req(&apiv1.CreateChannelRequest{Name: "x", Settings: jiraSettings("pat-token")})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("member create: %v", err)
	}
	// Tests go to the outbox for every kind.
	if _, err := owner.alert.SendTestNotification(ctx, req(&apiv1.SendTestNotificationRequest{ChannelId: hook.GetChannel().GetId()})); err != nil {
		t.Fatal(err)
	}
	var queued int
	_ = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE kind = $1 AND channel_id = $2`, store.NotifyTest,
			uuid.MustParse(hook.GetChannel().GetId())).Scan(&queued)
	})
	if queued != 1 {
		t.Fatalf("test notification not queued: %d", queued)
	}
}
