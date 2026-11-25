package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/secret"
)

// TwoFactorState is a user's two-factor authentication record. Secrets are
// sealed with the secret box.
type TwoFactorState struct {
	Secret      []byte
	EnabledAt   *time.Time
	LastStep    int64
	Pending     []byte
	PendingAt   *time.Time
	Failures    int
	LockedUntil *time.Time
}

// LockTwoFactor returns a user's two-factor record, locking the row until
// the transaction ends so that codes are checked one at a time.
func LockTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*TwoFactorState, error) {
	var st TwoFactorState
	err := tx.QueryRow(ctx, `
		SELECT totp_secret, totp_enabled_at, totp_last_step, totp_pending_secret, totp_pending_at, totp_failures, totp_locked_until
		FROM users WHERE id = $1 FOR UPDATE`, userID).
		Scan(&st.Secret, &st.EnabledAt, &st.LastStep, &st.Pending, &st.PendingAt, &st.Failures, &st.LockedUntil)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// SetPendingTOTP stores a secret waiting for its first code.
func SetPendingTOTP(ctx context.Context, q Querier, userID uuid.UUID, sealed []byte) error {
	_, err := q.Exec(ctx, `UPDATE users SET totp_pending_secret = $2, totp_pending_at = now() WHERE id = $1`, userID, sealed)
	return err
}

// EnableTOTP turns two-factor authentication on with the pending secret;
// step is the time step of the code that confirmed it.
func EnableTOTP(ctx context.Context, q Querier, userID uuid.UUID, step int64) error {
	_, err := q.Exec(ctx, `
		UPDATE users SET totp_secret = totp_pending_secret, totp_enabled_at = now(), totp_last_step = $2,
		       totp_pending_secret = NULL, totp_pending_at = NULL, totp_failures = 0, totp_locked_until = NULL,
		       updated_at = now()
		WHERE id = $1`, userID, step)
	return err
}

// DisableTwoFactor turns two-factor authentication off and deletes the
// user's recovery codes and pending sign-ins.
func DisableTwoFactor(ctx context.Context, q Querier, userID uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL, totp_last_step = 0,
		       totp_pending_secret = NULL, totp_pending_at = NULL, totp_failures = 0, totp_locked_until = NULL,
		       updated_at = now()
		WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := q.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `DELETE FROM login_challenges WHERE user_id = $1`, userID)
	return err
}

// AcceptSecondFactor records a correct code: it clears the failure count
// and, for TOTP codes (step > 0), marks the step used.
func AcceptSecondFactor(ctx context.Context, q Querier, userID uuid.UUID, step int64) error {
	_, err := q.Exec(ctx, `
		UPDATE users SET totp_last_step = greatest(totp_last_step, $2), totp_failures = 0, totp_locked_until = NULL
		WHERE id = $1`, userID, step)
	return err
}

// RecordSecondFactorFailure stores the consecutive failure count and the
// lockout it caused (nil for none).
func RecordSecondFactorFailure(ctx context.Context, q Querier, userID uuid.UUID, failures int, lockedUntil *time.Time) error {
	_, err := q.Exec(ctx, `UPDATE users SET totp_failures = $2, totp_locked_until = $3 WHERE id = $1`, userID, failures, lockedUntil)
	return err
}

// ReplaceRecoveryCodes stores new recovery code hashes, dropping the old
// codes.
func ReplaceRecoveryCodes(ctx context.Context, q Querier, userID uuid.UUID, hashes []string) error {
	if _, err := q.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) SELECT $1, unnest($2::text[])`, userID, hashes)
	return err
}

// UseRecoveryCode marks an unused recovery code used and reports whether
// there was one. hashes are the forms the code may be stored in (one per
// key of the keyring).
func UseRecoveryCode(ctx context.Context, q Querier, userID uuid.UUID, hashes []string) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE recovery_codes SET used_at = now() WHERE user_id = $1 AND code_hash = ANY($2) AND used_at IS NULL`,
		userID, hashes)
	return tag.RowsAffected() > 0, err
}

// UsersWithStaleRecoveryCodes counts the users whose unused recovery codes
// were hashed with another master key than the one named keyID.
func UsersWithStaleRecoveryCodes(ctx context.Context, q Querier, keyID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(DISTINCT user_id) FROM recovery_codes
		WHERE used_at IS NULL AND code_hash NOT LIKE $1 || ':%'`, keyID).Scan(&n)
	return n, err
}

// RecoveryCodesLeft counts a user's unused recovery codes.
func RecoveryCodesLeft(ctx context.Context, q Querier, userID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// CreateLoginChallenge stores a sign-in waiting for its second step and
// returns its token.
func CreateLoginChallenge(ctx context.Context, q Querier, userID uuid.UUID, ttl time.Duration) (string, error) {
	token := secret.Random(43)
	_, err := q.Exec(ctx, `INSERT INTO login_challenges (token_hash, user_id, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		secret.Hash(token), userID, ttl.String())
	return token, err
}

// LoginChallengeUser returns the user of an unexpired sign-in challenge.
func LoginChallengeUser(ctx context.Context, q Querier, token string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT user_id FROM login_challenges WHERE token_hash = $1 AND expires_at > now()`,
		secret.Hash(token)).Scan(&id)
	return id, notFound(err)
}

// DeleteLoginChallenge removes a sign-in challenge.
func DeleteLoginChallenge(ctx context.Context, q Querier, token string) error {
	_, err := q.Exec(ctx, `DELETE FROM login_challenges WHERE token_hash = $1`, secret.Hash(token))
	return err
}
