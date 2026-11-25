package auth_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

// TestHostCookies checks that on https the cookies carry the __Host-
// prefix, so that other applications on sibling subdomains cannot plant
// them, and that a cookie without the prefix is ignored there.
func TestHostCookies(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	var user *store.User
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@example.com", Name: "Ayşe"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	box, _ := secret.NewBox(make([]byte, 32))
	service := func(publicURL string) *auth.Service {
		cfg := &config.Config{Env: config.EnvTest, PublicURL: publicURL, Auth: config.AuthConfig{SessionTTL: time.Hour, PasswordLogin: true}}
		return auth.NewService(database, cfg, box, slog.New(slog.DiscardHandler))
	}

	svc := service("https://izci.example.com")
	cookie, _, err := svc.StartSession(ctx, user.ID, netip.Addr{}, "test", "password")
	if err != nil {
		t.Fatal(err)
	}
	if cookie.Name != "__Host-izci_session" || !cookie.Secure || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("session cookie: %+v", cookie)
	}
	if p, err := svc.Authenticate(ctx, http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}); err != nil || p == nil {
		t.Fatalf("prefixed cookie: %v %v", p, err)
	}
	if p, err := svc.Authenticate(ctx, http.Header{"Cookie": {"izci_session=" + cookie.Value}}); err != nil || p != nil {
		t.Fatalf("a cookie without the prefix must be ignored on https: %v %v", p, err)
	}
	if c := svc.ClearCookie(); c.Name != "__Host-izci_session" || c.Path != "/" {
		t.Fatalf("cleared cookie: %+v", c)
	}
	challenge, err := svc.BeginChallenge(ctx, user.ID)
	if err != nil || challenge.Name != "__Host-izci_2fa" || !challenge.Secure || challenge.Path != "/" {
		t.Fatalf("challenge cookie: %+v %v", challenge, err)
	}

	// Plain http (development) has no secure cookies, hence no prefix.
	dev := service("http://localhost:5300")
	cookie, _, err = dev.StartSession(ctx, user.ID, netip.Addr{}, "test", "password")
	if err != nil || cookie.Name != "izci_session" || cookie.Secure {
		t.Fatalf("development cookie: %+v %v", cookie, err)
	}
	if p, err := dev.Authenticate(ctx, http.Header{"Cookie": {"izci_session=" + cookie.Value}}); err != nil || p == nil {
		t.Fatalf("development cookie: %v %v", p, err)
	}
}
