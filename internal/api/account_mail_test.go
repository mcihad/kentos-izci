package api_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/api"
)

const firefox = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:131.0) Gecko/20100101 Firefox/131.0"

// waitMail waits for a mail to an address whose subject contains subject
// (security notices go out in the background).
func (m *mailbox) waitMail(t *testing.T, to, subject string) sentMail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, s := range m.sent {
			if s.to == to && strings.Contains(s.subject, subject) {
				m.mu.Unlock()
				return s
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no mail to %s about %q", to, subject)
	return sentMail{}
}

func (m *mailbox) about(subject string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sent {
		if strings.Contains(s.subject, subject) {
			n++
		}
	}
	return n
}

// TestAccountMail checks the e-mail account flows: invitations, password
// resets and security notices.
func TestAccountMail(t *testing.T) {
	box := &mailbox{}
	e := newEnv(t, func(_ *env, d *api.Deps) { d.Mailer = box })
	ctx := context.Background()
	const owner = "owner@example.com"
	c := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := c.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: owner,
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	providers, err := c.auth.ListAuthProviders(ctx, req(&apiv1.ListAuthProvidersRequest{}))
	if err != nil || !providers.Msg.GetPasswordResetAvailable() {
		t.Fatalf("providers: %v %v", providers, err)
	}

	// Invitations go by e-mail when asked.
	inv, err := c.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "mehmet@example.com", Role: apiv1.Role_ROLE_MEMBER, SendEmail: true}))
	if err != nil || !inv.Msg.GetEmailSent() {
		t.Fatalf("invite: %v %v", inv, err)
	}
	if m := box.waitMail(t, "mehmet@example.com", "KentOS organizasyonuna davet"); !strings.Contains(m.text, inv.Msg.GetInvitation().GetUrl()) {
		t.Fatalf("invitation mail: %+v", m)
	}

	// A password reset tells nobody which addresses have accounts.
	if _, err := c.auth.RequestPasswordReset(ctx, req(&apiv1.RequestPasswordResetRequest{Email: "kimse@example.com"})); err != nil {
		t.Fatalf("reset for an unknown address: %v", err)
	}
	if _, err := c.auth.RequestPasswordReset(ctx, req(&apiv1.RequestPasswordResetRequest{Email: owner})); err != nil {
		t.Fatal(err)
	}
	m := box.waitMail(t, owner, "parolanızı sıfırlayın")
	token := regexp.MustCompile(`/reset-password/(izr_[0-9A-Za-z]+)`).FindStringSubmatch(m.text)
	if token == nil || box.about("parolanızı sıfırlayın") != 1 {
		t.Fatalf("reset mail: %+v", m)
	}
	if _, err := c.auth.ResetPassword(ctx, req(&apiv1.ResetPasswordRequest{Token: token[1], NewPassword: "kısa"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT &&
		reason(err) != apiv1.ErrorReason_ERROR_REASON_WEAK_PASSWORD {
		t.Fatalf("weak password: %v", err)
	}
	if _, err := c.auth.ResetPassword(ctx, req(&apiv1.ResetPasswordRequest{Token: token[1], NewPassword: "yepyeni ve uzun parola"})); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := c.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("session after a reset: %v", err)
	}
	if _, err := c.auth.ResetPassword(ctx, req(&apiv1.ResetPasswordRequest{Token: token[1], NewPassword: "bir başka uzun parola"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID {
		t.Fatalf("used link: %v", err)
	}
	if _, err := c.auth.ResetPassword(ctx, req(&apiv1.ResetPasswordRequest{Token: "izr_bilinmeyenbaglanti0000", NewPassword: "bir başka uzun parola"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID {
		t.Fatalf("unknown link: %v", err)
	}
	box.waitMail(t, owner, "parolanız değiştirildi")

	// A sign-in from a new device is reported; the first sign-in was not.
	if n := box.about("yeni bir cihazdan"); n != 0 {
		t.Fatalf("first sign-in reported: %d", n)
	}
	laptop := e.client(map[string]string{"Izci-Org": "kentos", "User-Agent": firefox})
	if _, err := laptop.auth.Login(ctx, req(&apiv1.LoginRequest{Email: owner, Password: "yepyeni ve uzun parola"})); err != nil {
		t.Fatal(err)
	}
	if m := box.waitMail(t, owner, "yeni bir cihazdan"); !strings.Contains(m.text, "Firefox") {
		t.Fatalf("new device mail: %+v", m)
	}
	again := e.client(map[string]string{"Izci-Org": "kentos", "User-Agent": firefox})
	if _, err := again.auth.Login(ctx, req(&apiv1.LoginRequest{Email: owner, Password: "yepyeni ve uzun parola"})); err != nil {
		t.Fatal(err)
	}

	// New access tokens and password changes are reported too.
	if _, err := laptop.user.CreateAccessToken(ctx, req(&apiv1.CreateAccessTokenRequest{Name: "cli",
		Scopes: []apiv1.TokenScope{apiv1.TokenScope_TOKEN_SCOPE_READ, apiv1.TokenScope_TOKEN_SCOPE_WRITE}})); err != nil {
		t.Fatal(err)
	}
	if m := box.waitMail(t, owner, "erişim anahtarı"); !strings.Contains(m.text, "okuma + yazma") || !strings.Contains(m.text, "“cli”") {
		t.Fatalf("token mail: %+v", m)
	}
	if _, err := laptop.auth.ChangePassword(ctx, req(&apiv1.ChangePasswordRequest{CurrentPassword: "yepyeni ve uzun parola",
		NewPassword: "üçüncü uzun parola"})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for box.about("parolanız değiştirildi") < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if box.about("parolanız değiştirildi") != 2 || box.about("yeni bir cihazdan") != 1 {
		t.Fatalf("notices: password %d, devices %d", box.about("parolanız değiştirildi"), box.about("yeni bir cihazdan"))
	}

	// Reset links to one address are limited.
	for i := range 3 {
		_, err := c.auth.RequestPasswordReset(ctx, req(&apiv1.RequestPasswordResetRequest{Email: owner}))
		if i < 2 && err != nil {
			t.Fatalf("reset %d: %v", i, err)
		}
		if i == 2 && reason(err) != apiv1.ErrorReason_ERROR_REASON_TOO_MANY_ATTEMPTS {
			t.Fatalf("fourth reset link: %v", err)
		}
	}
}

// TestNoMailNoReset checks that servers without e-mail do not offer
// password resets.
func TestNoMailNoReset(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.client(nil)
	providers, err := c.auth.ListAuthProviders(ctx, req(&apiv1.ListAuthProvidersRequest{}))
	if err != nil || providers.Msg.GetPasswordResetAvailable() {
		t.Fatalf("providers: %v %v", providers, err)
	}
	if _, err := c.auth.RequestPasswordReset(ctx, req(&apiv1.RequestPasswordResetRequest{Email: "owner@example.com"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_EMAIL_DELIVERY_FAILED {
		t.Fatalf("reset without e-mail: %v", err)
	}
}
