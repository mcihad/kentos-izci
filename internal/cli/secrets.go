package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

func newSecretsCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage the secrets izci seals with IZCI_SECRET_KEY",
	}
	var dryRun bool
	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Re-seal stored secrets with the current IZCI_SECRET_KEY",
		Long: `Re-seal the secrets izci stores (GitHub tokens, notification channel
URLs and keys, two-factor secrets) with the current master key.

To rotate the master key:
  1. Generate a new key: openssl rand -base64 32
  2. On every instance set IZCI_SECRET_KEY to the new key and
     IZCI_SECRET_KEY_PREVIOUS to the old one, then restart them.
  3. Run "izci secrets rotate".
  4. Once the report shows nothing depends on the old key (recovery codes
     cannot be re-sealed: their users create new ones), remove
     IZCI_SECRET_KEY_PREVIOUS and restart.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			box, err := secret.NewBox(a.cfg.SecretKey, a.cfg.SecretKeyPrevious.Bytes()...)
			if err != nil {
				return err
			}
			r, err := rotateSecrets(cmd.Context(), database, box, dryRun)
			if err != nil {
				return err
			}
			if !dryRun {
				cliAudit(cmd.Context(), database, cmd.ErrOrStderr(), store.AuditEntry{Action: "secrets.rotate", TargetType: "instance",
					Metadata: map[string]string{"key_id": box.KeyID()}}, nil)
			}
			r.print(cmd.OutOrStdout(), box.KeyID(), dryRun)
			return nil
		},
	}
	rotate.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be re-sealed without changing anything")
	cmd.AddCommand(rotate)
	return cmd
}

// Kinds of sealed secrets.
const (
	sealedGitHub    = "GitHub tokens"
	sealedChannel   = "channel secrets"
	sealedTwoFactor = "two-factor secrets"
)

// rotation reports what rotateSecrets found, per kind of secret.
type rotation struct {
	resealed, current, unreadable map[string]int
	// staleRecoveryUsers have recovery codes hashed with a previous key.
	staleRecoveryUsers int
}

func (r *rotation) count(kind string, changed bool, err error) {
	switch {
	case err != nil:
		r.unreadable[kind]++
	case changed:
		r.resealed[kind]++
	default:
		r.current[kind]++
	}
}

func (r *rotation) print(w io.Writer, keyID string, dryRun bool) {
	verb := "re-sealed"
	if dryRun {
		verb = "to re-seal"
	}
	fmt.Fprintf(w, "current key %s\n", keyID)
	unreadable := 0
	for _, kind := range []string{sealedGitHub, sealedChannel, sealedTwoFactor} {
		fmt.Fprintf(w, "%-20s %d %s, %d already current, %d unreadable\n", kind+":", r.resealed[kind], verb, r.current[kind], r.unreadable[kind])
		unreadable += r.unreadable[kind]
	}
	if unreadable > 0 {
		fmt.Fprintf(w, "\n%d values open with no key of the keyring and were left as they are; add the key they were sealed with\n"+
			"to IZCI_SECRET_KEY_PREVIOUS, or set them again (GitHub connections, channels, two-factor authentication).\n", unreadable)
	}
	if r.staleRecoveryUsers > 0 {
		fmt.Fprintf(w, "\n%d users have recovery codes hashed with a previous key. They work while IZCI_SECRET_KEY_PREVIOUS\n"+
			"lists that key; ask the users to create new codes before removing it.\n", r.staleRecoveryUsers)
	}
	if dryRun {
		fmt.Fprintln(w, "\ndry run: nothing was changed")
	}
}

// rotateSecrets re-seals every stored secret of a previous key with the
// current one, in one transaction.
func rotateSecrets(ctx context.Context, database *db.DB, box *secret.Box, dryRun bool) (*rotation, error) {
	r := &rotation{resealed: map[string]int{}, current: map[string]int{}, unreadable: map[string]int{}}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		// GitHub tokens (integrations with token authentication).
		type sealedRow struct {
			ID    uuid.UUID
			Value []byte
		}
		rows, err := tx.Query(ctx, `SELECT id, secret FROM integrations WHERE secret IS NOT NULL`)
		if err != nil {
			return err
		}
		tokens, err := pgx.CollectRows(rows, pgx.RowToStructByPos[sealedRow])
		if err != nil {
			return err
		}
		for _, t := range tokens {
			out, changed, err := box.Reseal(t.Value, github.TokenAAD(t.ID))
			r.count(sealedGitHub, changed, err)
			if changed && !dryRun {
				if _, err := tx.Exec(ctx, `UPDATE integrations SET secret = $2 WHERE id = $1`, t.ID, out); err != nil {
					return err
				}
			}
		}

		// Channel secrets: every "…_sealed" value of a channel's config
		// (webhook URLs and signing keys, Jira tokens).
		rows, err = tx.Query(ctx, `SELECT id, config FROM notification_channels`)
		if err != nil {
			return err
		}
		channels, err := pgx.CollectRows(rows, pgx.RowToStructByPos[sealedRow])
		if err != nil {
			return err
		}
		for _, c := range channels {
			var cfg map[string]json.RawMessage
			if err := json.Unmarshal(c.Value, &cfg); err != nil {
				continue
			}
			for key, raw := range cfg {
				var sealed string
				if !strings.HasSuffix(key, "_sealed") || json.Unmarshal(raw, &sealed) != nil || sealed == "" {
					continue
				}
				out, changed, err := notify.Reseal(box, c.ID, sealed)
				r.count(sealedChannel, changed, err)
				if changed && !dryRun {
					if _, err := tx.Exec(ctx, `UPDATE notification_channels SET config = jsonb_set(config, ARRAY[$2], to_jsonb($3::text))
						WHERE id = $1`, c.ID, key, out); err != nil {
						return err
					}
				}
			}
		}

		// Two-factor secrets, confirmed and pending.
		for _, column := range []string{"totp_secret", "totp_pending_secret"} {
			rows, err := tx.Query(ctx, `SELECT id, `+column+` FROM users WHERE `+column+` IS NOT NULL`)
			if err != nil {
				return err
			}
			secrets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[sealedRow])
			if err != nil {
				return err
			}
			for _, s := range secrets {
				out, changed, err := box.Reseal(s.Value, auth.TOTPAAD(s.ID))
				r.count(sealedTwoFactor, changed, err)
				if changed && !dryRun {
					if _, err := tx.Exec(ctx, `UPDATE users SET `+column+` = $2 WHERE id = $1`, s.ID, out); err != nil {
						return err
					}
				}
			}
		}

		// Recovery codes are hashes: they cannot be moved to the new key.
		r.staleRecoveryUsers, err = store.UsersWithStaleRecoveryCodes(ctx, tx, box.KeyID())
		return err
	})
	return r, err
}
