package auth_test

import (
	"context"
	"errors"
	"log/slog"
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

func TestLoginLimits(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("ayşenin uzun parolası")
	if err := database.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@kentos.com.tr", PasswordHash: &hash})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Env: config.EnvTest, Auth: config.AuthConfig{PasswordLogin: true, SessionTTL: time.Hour}}
	box, _ := secret.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	// Two instances share the counters.
	a, b := auth.NewService(database, cfg, box, log), auth.NewService(database, cfg, box, log)
	login := func(s *auth.Service, pw, ip string) error {
		_, err := s.Login(ctx, "Ayse@Kentos.com.tr", pw, netip.MustParseAddr(ip))
		return err
	}
	limited := func(err error) bool {
		var tm *auth.TooManyAttemptsError
		return errors.As(err, &tm) && tm.RetryAfter > 0
	}

	// An attacker rotating addresses within one IPv6 /64 is one client.
	for i := range 10 {
		s := a
		if i%2 == 1 {
			s = b
		}
		if err := login(s, "yanlış parola", netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 15: byte(i + 1)}).String()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := login(b, "ayşenin uzun parolası", "2001:db8::ff"); !limited(err) {
		t.Fatalf("eleventh attempt from the same /64: %v", err)
	}
	// The owner, elsewhere, is not locked out.
	if err := login(a, "ayşenin uzun parolası", "198.51.100.7"); err != nil {
		t.Fatalf("owner from another address: %v", err)
	}
	// A success clears only its own client's failures.
	for range 9 {
		_ = login(a, "yanlış parola", "198.51.100.7")
	}
	if err := login(a, "ayşenin uzun parolası", "198.51.100.7"); err != nil {
		t.Fatalf("ninth failure must not lock: %v", err)
	}
	if err := login(a, "yanlış parola", "198.51.100.7"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("counter not cleared by the success: %v", err)
	}
	if err := login(a, "ayşenin uzun parolası", "2001:db8::1"); !limited(err) {
		t.Fatalf("the /64 is still limited: %v", err)
	}
}
