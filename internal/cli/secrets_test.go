package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

// TestRotateSecrets checks that "izci secrets rotate" moves every stored
// secret from a previous master key to the current one.
func TestRotateSecrets(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	key := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	old, _ := secret.NewBox(key(1))
	ring, _ := secret.NewBox(key(2), key(1))
	fresh, _ := secret.NewBox(key(2))
	if ring.KeyID() != fresh.KeyID() || ring.KeyID() == old.KeyID() {
		t.Fatalf("key ids: %s %s %s", ring.KeyID(), fresh.KeyID(), old.KeyID())
	}

	integration, channel := uuid.New(), uuid.New()
	var user uuid.UUID
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		if err != nil {
			return err
		}
		u, err := store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@example.com", Name: "Ayşe"})
		if err != nil {
			return err
		}
		user = u.ID
		if _, err := tx.Exec(ctx, `INSERT INTO integrations (id, org_id, auth, name, secret) VALUES ($1, $2, 1, 'kentos', $3)`,
			integration, org.ID, old.Seal([]byte("ghp_token"), github.TokenAAD(integration))); err != nil {
			return err
		}
		cfg, _ := json.Marshal(map[string]any{"url_sealed": notify.Seal(old, channel, "https://hooks.example.com/x"),
			"secret_sealed": notify.Seal(old, channel, "whsec"), "url_hint": "hooks.example.com/…/x", "chat_id": -1001234567890123})
		if _, err := tx.Exec(ctx, `INSERT INTO notification_channels (id, org_id, kind, name, config) VALUES ($1, $2, 4, 'Webhook', $3)`,
			channel, org.ID, cfg); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET totp_secret = $2 WHERE id = $1`, user, old.Seal([]byte("totp"), auth.TOTPAAD(user))); err != nil {
			return err
		}
		// A recovery code from before the rotation.
		_, err = tx.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2)`, user, old.KeyedSignature("code", 32))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	opens := func(b *secret.Box) bool {
		t.Helper()
		var token, totp, cfg []byte
		err := database.InSystem(ctx, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT secret FROM integrations WHERE id = $1`, integration).Scan(&token); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT totp_secret FROM users WHERE id = $1`, user).Scan(&totp); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT config FROM notification_channels WHERE id = $1`, channel).Scan(&cfg)
		})
		if err != nil {
			t.Fatal(err)
		}
		var c notify.WebhookConfig
		_ = json.Unmarshal(cfg, &c)
		_, e1 := b.Open(token, github.TokenAAD(integration))
		_, e2 := b.Open(totp, auth.TOTPAAD(user))
		u, e3 := notify.Open(b, channel, c.URLSealed)
		s, e4 := notify.Open(b, channel, c.SecretSealed)
		if !bytes.Contains(cfg, []byte("-1001234567890123")) {
			t.Fatalf("other settings changed: %s", cfg)
		}
		return e1 == nil && e2 == nil && e3 == nil && e4 == nil && u == "https://hooks.example.com/x" && s == "whsec"
	}

	// A dry run changes nothing.
	r, err := rotateSecrets(ctx, database, ring, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.resealed[sealedGitHub] != 1 || r.resealed[sealedChannel] != 2 || r.resealed[sealedTwoFactor] != 1 || opens(fresh) || !opens(old) {
		t.Fatalf("dry run: %+v", r)
	}

	r, err = rotateSecrets(ctx, database, ring, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.resealed[sealedGitHub] != 1 || r.resealed[sealedChannel] != 2 || r.resealed[sealedTwoFactor] != 1 || r.staleRecoveryUsers != 1 {
		t.Fatalf("rotation: %+v", r)
	}
	if !opens(fresh) {
		t.Fatal("the new key alone does not open the re-sealed secrets")
	}
	var out strings.Builder
	r.print(&out, ring.KeyID(), false)
	if !strings.Contains(out.String(), "1 users have recovery codes hashed with a previous key") {
		t.Fatalf("report:\n%s", out.String())
	}

	// A second run finds everything current; values of an unknown key are
	// reported, not changed.
	other, _ := secret.NewBox(key(3))
	err = database.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE users SET totp_pending_secret = $2 WHERE id = $1`, user, other.Seal([]byte("x"), auth.TOTPAAD(user)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err = rotateSecrets(ctx, database, ring, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.current[sealedGitHub] != 1 || r.current[sealedChannel] != 2 || r.current[sealedTwoFactor] != 1 ||
		r.unreadable[sealedTwoFactor] != 1 || r.resealed[sealedTwoFactor] != 0 {
		t.Fatalf("second rotation: %+v", r)
	}
}
