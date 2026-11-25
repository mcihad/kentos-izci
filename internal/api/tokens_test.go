package api_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// TestAccessTokenPolicy checks an organization's access token policy:
// tokens bound to it cannot outlive token_max_days, older tokens do not
// work in it, and tokens unused for token_idle_days are revoked.
func TestAccessTokenPolicy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	o, err := owner.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	settings := o.Msg.GetOrganization().GetSettings()
	settings.TokenMaxDays, settings.TokenIdleDays = 30, 10
	if _, err := owner.org.UpdateOrganization(ctx, req(&apiv1.UpdateOrganizationRequest{Settings: settings})); err != nil {
		t.Fatal(err)
	}
	sess, err := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if err != nil || sess.Msg.GetSession().GetMemberships()[0].GetTokenMaxDays() != 30 {
		t.Fatalf("membership: %v %v", sess, err)
	}

	create := func(org string, days int) (*apiv1.CreateAccessTokenResponse, error) {
		m := &apiv1.CreateAccessTokenRequest{Name: "cli", Organization: org,
			Scopes: []apiv1.TokenScope{apiv1.TokenScope_TOKEN_SCOPE_READ}}
		if days > 0 {
			m.ExpiresIn = durationpb.New(time.Duration(days) * 24 * time.Hour)
		}
		res, err := owner.user.CreateAccessToken(ctx, req(m))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	// A token bound to the organization cannot outlive its limit.
	for _, days := range []int{0, 60} {
		if _, err := create("kentos", days); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
			t.Fatalf("bound token for %d days: %v", days, err)
		}
	}
	if _, err := create("kentos", 30); err != nil {
		t.Fatalf("bound token within the limit: %v", err)
	}
	// Tokens for every organization may live longer; the policy applies
	// when they reach this one.
	tok, err := create("", 0)
	if err != nil {
		t.Fatal(err)
	}
	bot := e.client(map[string]string{"Authorization": "Bearer " + tok.GetSecret(), "Izci-Org": "kentos"})
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
		t.Fatalf("new token: %v", err)
	}
	list, err := owner.user.ListAccessTokens(ctx, req(&apiv1.ListAccessTokensRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range list.Msg.GetTokens() {
		if at.GetId() == tok.GetToken().GetId() && at.GetLastUsedIp() != "127.0.0.1" {
			t.Fatalf("last used address: %q", at.GetLastUsedIp())
		}
	}
	age := func(created, used string) {
		t.Helper()
		if _, err := e.db.Pool.Exec(ctx, `UPDATE personal_tokens SET created_at = now() - $2::interval, last_used_at = now() - $3::interval
			WHERE id = $1`, tok.GetToken().GetId(), created, used); err != nil {
			t.Fatal(err)
		}
	}

	// Older than the limit: turned away, but not revoked.
	age("40 days", "1 minute")
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_ACCESS_TOKEN_POLICY {
		t.Fatalf("token older than the limit: %v", err)
	}
	// Unused for longer than the idle limit: revoked.
	age("20 days", "15 days")
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_ACCESS_TOKEN_POLICY {
		t.Fatalf("idle token: %v", err)
	}
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("revoked token: %v", err)
	}
}
