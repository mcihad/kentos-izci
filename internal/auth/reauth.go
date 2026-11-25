package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/store"
)

// ReauthWindow is how recently a session must have proved its user's
// identity for sensitive changes (passwords, access tokens, two-factor
// settings, sign-in methods).
const ReauthWindow = 10 * time.Minute

// reauthLimit bounds wrong passwords when re-authenticating.
const reauthLimit = 10

// ErrSessionRequired means a sensitive change was attempted with an access
// token instead of a browser session.
var ErrSessionRequired = errors.New("this change needs a browser session")

// RecentlyAuthenticated reports whether the principal is a browser session
// that proved its user's identity within ReauthWindow.
func RecentlyAuthenticated(p *Principal) bool {
	return p != nil && p.Session != nil && time.Since(p.Session.ReauthenticatedAt) <= ReauthWindow
}

// Reauthenticate lets a signed-in user prove their identity again with
// their password or a two-factor code (a recovery code included), which
// unlocks sensitive changes for ReauthWindow.
func (s *Service) Reauthenticate(ctx context.Context, p *Principal, password, code string) (time.Time, error) {
	if p == nil || p.Session == nil {
		return time.Time{}, ErrSessionRequired
	}
	switch {
	case password != "":
		key := "reauth:" + p.User.ID.String()
		wait, err := s.limited(ctx, map[string]int{key: reauthLimit})
		if err != nil {
			return time.Time{}, err
		}
		if wait > 0 {
			return time.Time{}, &TooManyAttemptsError{RetryAfter: wait}
		}
		if p.User.PasswordHash == nil {
			return time.Time{}, ErrInvalidCredentials
		}
		ok, err := VerifyPassword(*p.User.PasswordHash, password)
		if err != nil || !ok {
			if err := s.count(ctx, key); err != nil {
				return time.Time{}, err
			}
			return time.Time{}, ErrInvalidCredentials
		}
		if err := s.forget(ctx, key); err != nil {
			return time.Time{}, err
		}
		return store.MarkSessionReauthenticated(ctx, s.db.Pool, p.Session.ID)
	case code != "":
		var at time.Time
		err := s.withSecondFactor(ctx, p.User.ID, code, func(tx pgx.Tx) error {
			var err error
			at, err = store.MarkSessionReauthenticated(ctx, tx, p.Session.ID)
			return err
		})
		if errors.Is(err, ErrTwoFactorOff) {
			return time.Time{}, ErrInvalidCredentials
		}
		return at, err
	}
	return time.Time{}, ErrInvalidCredentials
}
