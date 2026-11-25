package api_test

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/store"
)

// mailbox records the account e-mails the API sends.
type mailbox struct {
	mu   sync.Mutex
	sent []sentMail
}

type sentMail struct{ to, subject, text, html string }

func (m *mailbox) SendMail(_ context.Context, to, subject, text, html string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{to, subject, text, html})
	return nil
}

var signupLink = regexp.MustCompile(`http://izci\.test/verify/(izv_[0-9A-Za-z]+)`)

// token returns the token of the last link sent to an address.
func (m *mailbox) token(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.sent) - 1; i >= 0; i-- {
		if m.sent[i].to == to {
			match := signupLink.FindStringSubmatch(m.sent[i].text)
			if match == nil || !strings.Contains(m.sent[i].html, match[0]) {
				t.Fatalf("no link in the mail to %s: %q", to, m.sent[i].text)
			}
			return match[1]
		}
	}
	t.Fatalf("no mail to %s", to)
	return ""
}

func (m *mailbox) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// TestOpenSignup checks that self sign-ups create accounts only for
// addresses whose owner opened the link sent to them.
func TestOpenSignup(t *testing.T) {
	box := &mailbox{}
	e := newEnv(t, func(_ *env, d *api.Deps) { d.Mailer = box })
	e.cfg.Auth.Signup = "open"
	e.cfg.Auth.SignupOrgLimit = 2
	ctx := context.Background()
	signUp := func(c *client, email, name, org string) (*apiv1.SignUpResponse, error) {
		res, err := c.auth.SignUp(ctx, req(&apiv1.SignUpRequest{Name: name, Email: email, Password: "uzun ve güçlü parola",
			OrganizationName: org, OrganizationSlug: strings.ToLower(org)}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}

	// Nobody signs up before the setup screen made the first account.
	if _, err := signUp(e.client(nil), "erken@example.com", "Erken", "Erken"); reason(err) != apiv1.ErrorReason_ERROR_REASON_SETUP_REQUIRED {
		t.Fatalf("sign-up before setup: %v", err)
	}
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}

	// Signing up sends a link; no account, no session yet.
	ayse := e.client(nil)
	res, err := signUp(ayse, "Ayse@Example.com", "Ayşe", "Acme")
	if err != nil || !res.GetVerificationSent() || res.GetSession() != nil {
		t.Fatalf("sign-up: %v %v", res, err)
	}
	if _, err := ayse.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("session before the link: %v", err)
	}
	var users int64
	_ = e.db.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'ayse@example.com'`).Scan(&users)
	if users != 0 {
		t.Fatal("an account exists before its address was confirmed")
	}
	// The mail carries nothing the signer typed.
	if m := box.sent[0]; m.to != "ayse@example.com" || strings.Contains(m.text+m.html, "Acme") || strings.Contains(m.text+m.html, "Ayşe") {
		t.Fatalf("mail: %+v", m)
	}
	first := box.token(t, "ayse@example.com")

	// Someone else's sign-up for the same address blocks nothing.
	mallory := e.client(nil)
	if _, err := signUp(mallory, "ayse@example.com", "Mallory", "Mallory"); err != nil {
		t.Fatalf("second sign-up for the address: %v", err)
	}
	second := box.token(t, "ayse@example.com")
	if _, err := ayse.auth.VerifySignup(ctx, req(&apiv1.VerifySignupRequest{Token: "izv_" + strings.Repeat("x", 32)})); reason(err) != apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID {
		t.Fatalf("unknown link: %v", err)
	}
	// The slug was taken meanwhile: the organization gets a suffix.
	if err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.CreateOrganization(ctx, tx, "acme", "Acme Başka")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	v, err := ayse.auth.VerifySignup(ctx, req(&apiv1.VerifySignupRequest{Token: first}))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	s := v.Msg.GetSession()
	if s.GetUser().GetEmail() != "ayse@example.com" || s.GetUser().GetName() != "Ayşe" || len(s.GetMemberships()) != 1 ||
		s.GetMemberships()[0].GetRole() != apiv1.Role_ROLE_OWNER || !strings.HasPrefix(s.GetMemberships()[0].GetOrganization().GetSlug(), "acme-") {
		t.Fatalf("session after verifying: %v", s)
	}
	if _, err := ayse.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); err != nil {
		t.Fatalf("signed in after verifying: %v", err)
	}
	// Used links and the other sign-ups for the address are gone.
	for _, tok := range []string{first, second} {
		if _, err := mallory.auth.VerifySignup(ctx, req(&apiv1.VerifySignupRequest{Token: tok})); reason(err) != apiv1.ErrorReason_ERROR_REASON_VERIFICATION_LINK_INVALID {
			t.Fatalf("link after verifying: %v", err)
		}
	}
	if _, err := signUp(mallory, "ayse@example.com", "Mallory", "Mallory"); reason(err) != apiv1.ErrorReason_ERROR_REASON_EMAIL_TAKEN {
		t.Fatalf("sign-up for a taken address: %v", err)
	}
	if _, err := signUp(mallory, "baska@example.com", "Mallory", "KentOS"); reason(err) != apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN {
		t.Fatalf("sign-up for a taken slug: %v", err)
	}

	// Links to one address are limited, so nobody floods a mailbox.
	sent := box.count()
	for i := range 4 {
		_, err := signUp(mallory, "kurban@example.com", "Mallory", "Kurban")
		if i < 3 && err != nil {
			t.Fatalf("sign-up %d: %v", i, err)
		}
		if i == 3 && reason(err) != apiv1.ErrorReason_ERROR_REASON_TOO_MANY_ATTEMPTS {
			t.Fatalf("fourth link to one address: %v", err)
		}
	}
	if box.count() != sent+3 {
		t.Fatalf("mails: %d", box.count()-sent)
	}

	// Each user owns at most AUTH_SIGNUP_ORG_LIMIT organizations.
	createOrg := func(slug string) error {
		_, err := ayse.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: slug, Slug: slug}))
		return err
	}
	if err := createOrg("ikinci"); err != nil {
		t.Fatalf("second organization: %v", err)
	}
	if err := createOrg("ucuncu"); reason(err) != apiv1.ErrorReason_ERROR_REASON_LIMIT_REACHED {
		t.Fatalf("organization over the limit: %v", err)
	}
}
