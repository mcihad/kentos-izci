package api_test

import (
	"context"
	"strings"
	"testing"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

func instanceAudit(t *testing.T, c *client) map[string][]*apiv1.AuditLogEntry {
	t.Helper()
	res, err := c.admin.ListInstanceAuditLog(context.Background(), req(&apiv1.ListInstanceAuditLogRequest{PageSize: 200}))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]*apiv1.AuditLogEntry{}
	for _, e := range res.Msg.GetEntries() {
		out[e.GetAction()] = append(out[e.GetAction()], e)
	}
	return out
}

func orgAudit(t *testing.T, c *client) map[string][]*apiv1.AuditLogEntry {
	t.Helper()
	res, err := c.org.ListAuditLog(context.Background(), req(&apiv1.ListAuditLogRequest{PageSize: 100}))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]*apiv1.AuditLogEntry{}
	for _, e := range res.Msg.GetEntries() {
		out[e.GetAction()] = append(out[e.GetAction()], e)
	}
	return out
}

// TestSecurityAudit records sign-ins, account security changes and server
// administrators' visits to organizations.
func TestSecurityAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setupKentos(t, e)
	laptop := e.client(map[string]string{"Izci-Org": "kentos", "User-Agent": firefox})
	if _, err := laptop.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "yanlış parola"})); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := laptop.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "kimse@example.com", Password: "yanlış parola"})); err == nil {
		t.Fatal("unknown account accepted")
	}
	if _, err := laptop.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "uzun ve güçlü parola"})); err != nil {
		t.Fatal(err)
	}
	if _, err := laptop.auth.ChangePassword(ctx, req(&apiv1.ChangePasswordRequest{CurrentPassword: "uzun ve güçlü parola",
		NewPassword: "yepyeni uzun parola"})); err != nil {
		t.Fatal(err)
	}

	log := instanceAudit(t, laptop)
	failed := log["auth.login_failed"]
	if len(failed) != 2 || failed[1].GetMetadata()["reason"] != "invalid_credentials" || failed[1].GetActor().GetEmail() != "owner@example.com" ||
		failed[0].GetTargetName() != "kimse@example.com" || failed[0].GetActor() != nil {
		t.Fatalf("failed sign-ins: %v", failed)
	}
	logins := log["auth.login"]
	if len(logins) != 2 || logins[1].GetMetadata()["method"] != "setup" || logins[0].GetMetadata()["method"] != "password" ||
		!strings.Contains(logins[0].GetMetadata()["device"], "Firefox") {
		t.Fatalf("sign-ins: %v", logins)
	}
	if len(log["user.password.change"]) != 1 {
		t.Fatalf("instance log: %v", log)
	}
	// Account security changes reach the organization's log too.
	if len(orgAudit(t, laptop)["user.password.change"]) != 1 {
		t.Fatal("password change missing from the organization's audit log")
	}

	// A server administrator in an organization they are not a member of
	// is recorded there, once.
	if _, err := laptop.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: "Başka", Slug: "baska"})); err != nil {
		t.Fatal(err)
	}
	baska := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := baska.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "yepyeni uzun parola"})); err != nil {
		t.Fatal(err)
	}
	inv, err := baska.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "sahip@example.com", Role: apiv1.Role_ROLE_OWNER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	other := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := other.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Sahip", Password: "sahibin uzun parolası"})); err != nil {
		t.Fatal(err)
	}
	session, err := baska.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baska.org.RemoveMember(ctx, req(&apiv1.RemoveMemberRequest{UserId: session.Msg.GetSession().GetUser().GetId()})); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := baska.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
			t.Fatal(err)
		}
	}
	visits := orgAudit(t, other)["superadmin.access"]
	if len(visits) != 1 || visits[0].GetActor().GetEmail() != "owner@example.com" {
		t.Fatalf("superadmin visits: %v", visits)
	}
	if _, err := other.admin.ListInstanceAuditLog(ctx, req(&apiv1.ListInstanceAuditLogRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("instance log for a member: %v", err)
	}
}
