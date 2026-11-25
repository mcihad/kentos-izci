package api_test

import (
	"context"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
)

// authenticator is the test's authenticator app.
type authenticator []byte

func newAuthenticator(t *testing.T, secret string) authenticator {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("secret %q: %v", secret, err)
	}
	return b
}

// code returns the code d steps from now; every code is accepted once, so
// successive sign-ins use later steps (the server accepts one step of
// drift).
func (a authenticator) code(d int64) string {
	return auth.TOTPCode(a, auth.TOTPStep(time.Now())+d)
}

// wrong returns a code that matches no step of the accepted window.
func (a authenticator) wrong() string {
	valid := map[string]bool{a.code(-1): true, a.code(0): true, a.code(1): true, a.code(2): true}
	for _, c := range []string{"000000", "111111", "222222", "333333", "444444"} {
		if !valid[c] {
			return c
		}
	}
	return "555555"
}

func TestTwoFactorAuthentication(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const password = "uzun ve güçlü parola"
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Ayşe", Email: "ayse@example.com",
		Password: password, OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	require := func(c *client, on bool) error {
		o, err := c.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
		if err != nil {
			return err
		}
		settings := o.Msg.GetOrganization().GetSettings()
		settings.RequireTwoFactor = on
		_, err = c.org.UpdateOrganization(ctx, req(&apiv1.UpdateOrganizationRequest{Settings: settings}))
		return err
	}
	// Requiring two-factor authentication without using it would lock the
	// owner out.
	if err := require(owner, true); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("require without own 2FA: %v", err)
	}

	// A session on another device, signed in with the password alone.
	laptop := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := laptop.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "ayse@example.com", Password: password})); err != nil {
		t.Fatal(err)
	}

	// Setup: a secret, a confirming code, recovery codes.
	setup, err := owner.user.BeginTwoFactorSetup(ctx, req(&apiv1.BeginTwoFactorSetupRequest{}))
	if err != nil || !strings.HasPrefix(setup.Msg.GetUri(), "otpauth://totp/%C4%B0zci:ayse@example.com?secret="+setup.Msg.GetSecret()+"&") {
		t.Fatalf("begin setup: %v %v", setup, err)
	}
	app := newAuthenticator(t, setup.Msg.GetSecret())
	if _, err := owner.user.EnableTwoFactor(ctx, req(&apiv1.EnableTwoFactorRequest{Code: app.wrong()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("enable with a wrong code: %v", err)
	}
	enabled, err := owner.user.EnableTwoFactor(ctx, req(&apiv1.EnableTwoFactorRequest{Code: app.code(-1)}))
	if err != nil || len(enabled.Msg.GetRecoveryCodes()) != 10 {
		t.Fatalf("enable: %v %v", enabled, err)
	}
	recovery := enabled.Msg.GetRecoveryCodes()
	sess, err := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if u := sess.Msg.GetSession().GetUser(); err != nil || !u.GetTwoFactorEnabled() || u.GetRecoveryCodesLeft() != 10 {
		t.Fatalf("session after enabling: %v %v", sess, err)
	}
	if _, err := owner.user.BeginTwoFactorSetup(ctx, req(&apiv1.BeginTwoFactorSetupRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_CONFLICT {
		t.Fatalf("second setup: %v", err)
	}
	// Sessions that never passed the second step end.
	if _, err := laptop.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("other session after enabling: %v", err)
	}
	// Access tokens cannot change two-factor settings.
	tok, err := owner.user.CreateAccessToken(ctx, req(&apiv1.CreateAccessTokenRequest{Name: "cli",
		Scopes: []apiv1.TokenScope{apiv1.TokenScope_TOKEN_SCOPE_READ, apiv1.TokenScope_TOKEN_SCOPE_WRITE}}))
	if err != nil {
		t.Fatal(err)
	}
	bot := e.client(map[string]string{"Authorization": "Bearer " + tok.Msg.GetSecret(), "Izci-Org": "kentos"})
	if _, err := bot.user.DisableTwoFactor(ctx, req(&apiv1.DisableTwoFactorRequest{Code: recovery[9]})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("token disabling 2FA: %v", err)
	}

	// Password sign-in now asks for the second step.
	login := func() *client {
		t.Helper()
		c := e.client(map[string]string{"Izci-Org": "kentos"})
		res, err := c.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "ayse@example.com", Password: password}))
		if err != nil || !res.Msg.GetTwoFactorRequired() || res.Msg.GetSession() != nil {
			t.Fatalf("login: %v %v", res, err)
		}
		return c
	}
	verify := func(c *client, code string) (*apiv1.Session, error) {
		res, err := c.auth.VerifyTwoFactor(ctx, req(&apiv1.VerifyTwoFactorRequest{Code: code}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetSession(), nil
	}
	browser := login()
	if _, err := browser.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("session before the second step: %v", err)
	}
	if _, err := verify(e.client(nil), app.code(0)); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_EXPIRED {
		t.Fatalf("second step without a sign-in: %v", err)
	}
	if _, err := verify(browser, app.wrong()); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("wrong code: %v", err)
	}
	if s, err := verify(browser, app.code(0)); err != nil || s.GetUser().GetEmail() != "ayse@example.com" {
		t.Fatalf("second step: %v %v", s, err)
	}
	if _, err := browser.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
		t.Fatalf("signed in: %v", err)
	}
	if _, err := verify(browser, app.code(1)); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_EXPIRED {
		t.Fatalf("challenge reused: %v", err)
	}

	// A code works once; a recovery code too.
	browser = login()
	if _, err := verify(browser, app.code(0)); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("replayed code: %v", err)
	}
	if _, err := verify(browser, strings.ToUpper(recovery[0])); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	sess, _ = browser.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if sess.Msg.GetSession().GetUser().GetRecoveryCodesLeft() != 9 {
		t.Fatalf("recovery codes left: %v", sess.Msg.GetSession().GetUser())
	}
	browser = login()
	if _, err := verify(browser, recovery[0]); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("used recovery code: %v", err)
	}

	// New recovery codes replace the old ones.
	fresh, err := owner.user.RegenerateRecoveryCodes(ctx, req(&apiv1.RegenerateRecoveryCodesRequest{Code: recovery[1]}))
	if err != nil || len(fresh.Msg.GetRecoveryCodes()) != 10 {
		t.Fatalf("regenerate: %v %v", fresh, err)
	}
	if _, err := verify(browser, recovery[2]); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("old recovery code after regenerating: %v", err)
	}

	// The organization requires two-factor authentication: members without
	// it are turned away until they turn it on.
	if err := require(owner, true); err != nil {
		t.Fatalf("require: %v", err)
	}
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "mehmet@example.com", Role: apiv1.Role_ROLE_MEMBER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	member := e.client(map[string]string{"Izci-Org": "kentos"})
	acc, err := member.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Mehmet", Password: "mehmetin parolası"}))
	if err != nil || !acc.Msg.GetSession().GetMemberships()[0].GetRequiresTwoFactor() {
		t.Fatalf("membership of a member without 2FA: %v %v", acc, err)
	}
	if _, err := member.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("member without 2FA: %v", err)
	}
	members, err := owner.org.ListMembers(ctx, req(&apiv1.ListMembersRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members.Msg.GetMembers() {
		if m.GetTwoFactorEnabled() != (m.GetUser().GetEmail() == "ayse@example.com") {
			t.Errorf("member %s: two_factor_enabled=%v", m.GetUser().GetEmail(), m.GetTwoFactorEnabled())
		}
	}
	msetup, err := member.user.BeginTwoFactorSetup(ctx, req(&apiv1.BeginTwoFactorSetupRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	mapp := newAuthenticator(t, msetup.Msg.GetSecret())
	if _, err := member.user.EnableTwoFactor(ctx, req(&apiv1.EnableTwoFactorRequest{Code: mapp.code(-1)})); err != nil {
		t.Fatal(err)
	}
	if _, err := member.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
		t.Fatalf("member with 2FA: %v", err)
	}
	if _, err := member.user.DisableTwoFactor(ctx, req(&apiv1.DisableTwoFactorRequest{Code: mapp.wrong()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("disable with a wrong code: %v", err)
	}
	if _, err := member.user.DisableTwoFactor(ctx, req(&apiv1.DisableTwoFactorRequest{Code: mapp.code(0)})); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := member.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("member after turning 2FA off: %v", err)
	}

	// Lifting the requirement weakens every member's account: an old
	// session confirms its user's identity first, here with a recovery code.
	if _, err := e.db.Pool.Exec(ctx, `UPDATE sessions SET reauthenticated_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if err := require(owner, false); reason(err) != apiv1.ErrorReason_ERROR_REASON_REAUTHENTICATION_REQUIRED {
		t.Fatalf("lifting the requirement from an old session: %v", err)
	}
	if _, err := owner.auth.Reauthenticate(ctx, req(&apiv1.ReauthenticateRequest{Code: app.wrong()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
		t.Fatalf("reauthenticate with a wrong code: %v", err)
	}
	if _, err := owner.auth.Reauthenticate(ctx, req(&apiv1.ReauthenticateRequest{Code: fresh.Msg.GetRecoveryCodes()[0]})); err != nil {
		t.Fatalf("reauthenticate with a recovery code: %v", err)
	}
	if err := require(owner, false); err != nil {
		t.Fatalf("lifting the requirement: %v", err)
	}
	if err := require(owner, true); err != nil {
		t.Fatalf("requiring again: %v", err)
	}

	// Wrong codes lock the second step: five in a row after the last
	// correct code.
	browser = login()
	for range 5 {
		if _, err := verify(browser, app.wrong()); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_TWO_FACTOR_CODE {
			t.Fatalf("wrong code: %v", err)
		}
	}
	if _, err := verify(browser, app.code(1)); reason(err) != apiv1.ErrorReason_ERROR_REASON_TOO_MANY_ATTEMPTS {
		t.Fatalf("locked second step: %v", err)
	}

	// An instance administrator resets it: the password alone signs in,
	// and the organization turns the account away until 2FA is on again.
	sess, _ = owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if _, err := owner.admin.UpdateUser(ctx, req(&apiv1.UpdateUserRequest{UserId: sess.Msg.GetSession().GetUser().GetId(), ResetTwoFactor: true})); err != nil {
		t.Fatalf("admin reset: %v", err)
	}
	// The sessions that passed the second step end with it, except the
	// administrator's own current one.
	var sessions int
	if err := e.db.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE u.email = 'ayse@example.com'`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions after the reset: %d %v", sessions, err)
	}
	if _, err := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); err != nil {
		t.Fatalf("own session after the reset: %v", err)
	}
	plain := e.client(map[string]string{"Izci-Org": "kentos"})
	res, err := plain.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "ayse@example.com", Password: password}))
	if err != nil || res.Msg.GetTwoFactorRequired() || res.Msg.GetSession() == nil {
		t.Fatalf("login after reset: %v %v", res, err)
	}
	if _, err := plain.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("owner without 2FA: %v", err)
	}
	if _, err := bot.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("token of a user without 2FA: %v", err)
	}

	// Each change is in the organization's audit log and the instance's.
	var inOrg, inInstance int
	err = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE org_id IS NOT NULL), count(*) FILTER (WHERE org_id IS NULL)
			FROM audit_log WHERE action IN ('user.two_factor.enable', 'user.two_factor.disable', 'user.two_factor.recovery_codes')`).
			Scan(&inOrg, &inInstance)
	})
	if err != nil || inOrg != 4 || inInstance != 4 {
		t.Fatalf("audit entries: %d in the organization, %d in the instance, %v", inOrg, inInstance, err)
	}
}

// TestSuperadminTwoFactor checks AUTH_REQUIRE_2FA_SUPERADMIN: an instance
// administrator without two-factor authentication keeps their account and
// memberships, but not their administrator rights.
func TestSuperadminTwoFactor(t *testing.T) {
	e := newEnv(t)
	e.cfg.Auth.RequireTwoFactorSuperadmin = "true"
	ctx := context.Background()
	admin := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := admin.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Ayşe", Email: "ayse@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.CreateOrganization(ctx, tx, "diger", "Diğer")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	needs := func() bool {
		t.Helper()
		s, err := admin.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		return s.Msg.GetSession().GetSuperadminNeedsTwoFactor()
	}
	if !needs() {
		t.Fatal("session must report the missing second factor")
	}
	if _, err := admin.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
		t.Fatalf("own organization: %v", err)
	}
	if _, err := admin.admin.ListUsers(ctx, req(&apiv1.ListUsersRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("server administration without 2FA: %v", err)
	}
	admin.hdr["Izci-Org"] = "diger"
	if _, err := admin.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED {
		t.Fatalf("someone else's organization without 2FA: %v", err)
	}

	// Two-factor authentication gives the rights back.
	setup, err := admin.user.BeginTwoFactorSetup(ctx, req(&apiv1.BeginTwoFactorSetupRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	app := newAuthenticator(t, setup.Msg.GetSecret())
	if _, err := admin.user.EnableTwoFactor(ctx, req(&apiv1.EnableTwoFactorRequest{Code: app.code(-1)})); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err != nil {
		t.Fatalf("someone else's organization with 2FA: %v", err)
	}
	if _, err := admin.admin.ListUsers(ctx, req(&apiv1.ListUsersRequest{})); err != nil {
		t.Fatalf("server administration with 2FA: %v", err)
	}
	if needs() {
		t.Fatal("session still reports a missing second factor")
	}
}
