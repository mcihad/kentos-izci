package auth

import (
	"context"
	"net/netip"
	"time"
)

// attemptWindow is how long failures count.
const attemptWindow = 15 * time.Minute

// Limits per attemptWindow.
const (
	// One client guessing one account's password.
	loginPairLimit = 10
	// Everyone together guessing one account: high enough that a single
	// attacker cannot lock its owner out from another address.
	loginEmailLimit = 100
	// One client trying many accounts.
	loginIPLimit = 30
	// One client calling unauthenticated endpoints that guess secrets or
	// hash passwords.
	publicIPLimit = 30
	// Sign-up links sent to one e-mail address: nobody floods someone's
	// mailbox through izci.
	signupAddressLimit = 3
	// Invitation e-mails one organization, and one inviter, sends.
	invitationLimit = 30
)

// addressKey groups client addresses as one party: IPv6 clients usually
// own a whole /64, so they are limited per /64.
func addressKey(ip netip.Addr) string {
	ip = ip.Unmap()
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

// limited returns how long to wait when any of the keys has reached its
// limit in the current window, or 0.
func (s *Service) limited(ctx context.Context, limits map[string]int) (time.Duration, error) {
	keys := make([]string, 0, len(limits))
	for k := range limits {
		keys = append(keys, k)
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT key, count, window_start FROM auth_attempts
		WHERE key = ANY($1) AND window_start > now() - $2::interval`, keys, attemptWindow.String())
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var wait time.Duration
	for rows.Next() {
		var key string
		var count int
		var start time.Time
		if err := rows.Scan(&key, &count, &start); err != nil {
			return 0, err
		}
		if count >= limits[key] {
			wait = max(wait, time.Until(start.Add(attemptWindow)))
		}
	}
	return max(wait, 0), rows.Err()
}

// count records an attempt for each key, starting a new window when the
// last one is over.
func (s *Service) count(ctx context.Context, keys ...string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO auth_attempts (key, count, window_start) SELECT unnest($1::text[]), 1, now()
		ON CONFLICT (key) DO UPDATE SET
		  count = CASE WHEN auth_attempts.window_start <= now() - $2::interval THEN 1 ELSE auth_attempts.count + 1 END,
		  window_start = CASE WHEN auth_attempts.window_start <= now() - $2::interval THEN now() ELSE auth_attempts.window_start END`,
		keys, attemptWindow.String())
	return err
}

// forget drops the counters of keys (after a successful sign-in).
func (s *Service) forget(ctx context.Context, keys ...string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM auth_attempts WHERE key = ANY($1)`, keys)
	return err
}

// ThrottlePublic limits unauthenticated requests that are expensive or
// guess secrets (sign-up, accepting invitations, setup, second steps) per
// client address. Every call counts.
func (s *Service) ThrottlePublic(ctx context.Context, ip netip.Addr) error {
	if !ip.IsValid() {
		return nil
	}
	return s.throttle(ctx, "public:"+addressKey(ip), publicIPLimit)
}

// ThrottleSignup limits the sign-up links sent to one e-mail address.
// Every call counts.
func (s *Service) ThrottleSignup(ctx context.Context, email string) error {
	return s.ThrottleEmail(ctx, "signup", email)
}

// ThrottleEmail limits the e-mails of one purpose (sign-up, reset,
// invite) sent to one address. Every call counts.
func (s *Service) ThrottleEmail(ctx context.Context, purpose, email string) error {
	return s.throttle(ctx, purpose+":"+email, signupAddressLimit)
}

// ThrottleInvitations limits the invitation e-mails one organization and
// one inviter send, so that open sign-up (which lets anyone own a few
// organizations) cannot be used to mail large numbers of addresses.
func (s *Service) ThrottleInvitations(ctx context.Context, orgID, inviterID string) error {
	keys := []string{"invitations:" + orgID, "inviter:" + inviterID}
	wait, err := s.limited(ctx, map[string]int{keys[0]: invitationLimit, keys[1]: invitationLimit})
	if err != nil {
		return err
	}
	if wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	return s.count(ctx, keys...)
}

// throttle counts a call under key and fails with TooManyAttemptsError
// once limit calls were made in the window.
func (s *Service) throttle(ctx context.Context, key string, limit int) error {
	wait, err := s.limited(ctx, map[string]int{key: limit})
	if err != nil {
		return err
	}
	if wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	return s.count(ctx, key)
}
