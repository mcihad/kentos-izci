package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/secret"
)

// CreatePasswordReset stores a password reset for ttl and returns the
// token of its link.
func CreatePasswordReset(ctx context.Context, q Querier, userID uuid.UUID, ttl time.Duration) (string, error) {
	token := secret.NewPasswordResetToken()
	_, err := q.Exec(ctx, `INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		secret.Hash(token), userID, ttl.String())
	return token, err
}

// PasswordResetValid reports whether a reset token is known and unexpired,
// without using it.
func PasswordResetValid(ctx context.Context, q Querier, token string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM password_resets WHERE token_hash = $1 AND expires_at > now())`,
		secret.Hash(token)).Scan(&ok)
	return ok, err
}

// TakePasswordReset returns the account of an unexpired reset token and
// removes every reset of that account (ErrNotFound for unknown, used or
// expired tokens).
func TakePasswordReset(ctx context.Context, q Querier, token string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := q.QueryRow(ctx, `DELETE FROM password_resets WHERE token_hash = $1 AND expires_at > now() RETURNING user_id`,
		secret.Hash(token)).Scan(&userID)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	_, err = q.Exec(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID)
	return userID, err
}

// SeenDevice records a sign-in from a device (browser and operating
// system family). It reports whether the device is new for the member and
// whether they had signed in from any device before.
func SeenDevice(ctx context.Context, q Querier, userID uuid.UUID, device string) (isNew, hadDevices bool, err error) {
	var before int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM user_devices WHERE user_id = $1`, userID).Scan(&before); err != nil {
		return false, false, err
	}
	err = q.QueryRow(ctx, `INSERT INTO user_devices (user_id, device) VALUES ($1, $2)
		ON CONFLICT (user_id, device) DO UPDATE SET last_seen = now()
		RETURNING xmax = 0`, userID, device).Scan(&isNew)
	return isNew, before > 0, err
}
