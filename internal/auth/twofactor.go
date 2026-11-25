package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/store"
)

// ChallengeCookie carries the token of a sign-in waiting for its second
// step (HttpOnly, like the session cookie).
const ChallengeCookie = "izci_2fa"

const (
	// challengeTTL is how long the second step of a sign-in may take.
	challengeTTL = 10 * time.Minute
	// pendingTOTPTTL is how long a started setup waits for its first code.
	pendingTOTPTTL = time.Hour
	// totpIssuer names izci in authenticator apps.
	totpIssuer = "İzci"
	// lockoutEvery wrong codes in a row lock the second step for a while;
	// the lockout doubles each time, from 5 minutes up to a day.
	lockoutEvery = 5
)

// Two-factor authentication errors.
var (
	ErrTwoFactorRequired     = errors.New("the organization requires two-factor authentication")
	ErrInvalidSecondFactor   = errors.New("invalid two-factor code")
	ErrTwoFactorOn           = errors.New("two-factor authentication is already on")
	ErrTwoFactorOff          = errors.New("two-factor authentication is off")
	ErrTwoFactorSetupExpired = errors.New("two-factor setup expired, start again")
	ErrChallengeExpired      = errors.New("sign-in expired, sign in again")
)

// TOTPSetup is a started setup: the secret to add to an authenticator app.
type TOTPSetup struct {
	Secret string // base32, for typing in
	URI    string // otpauth:// URI, for the QR code
}

func totpAD(userID uuid.UUID) []byte { return []byte("izci/totp/" + userID.String()) }

// recoveryHash is the stored form of a normalized recovery code: an HMAC
// under a key derived from IZCI_SECRET_KEY.
func (s *Service) recoveryHash(userID uuid.UUID, code string) string {
	return s.box.Sign("izci/recovery-code/"+userID.String()+"/"+code, 32)
}

// BeginTOTP starts turning on two-factor authentication with a new secret.
// Starting again replaces the secret of an unfinished setup.
func (s *Service) BeginTOTP(ctx context.Context, u *store.User) (*TOTPSetup, error) {
	if u.TwoFactor() {
		return nil, ErrTwoFactorOn
	}
	secret := NewTOTPSecret()
	if err := store.SetPendingTOTP(ctx, s.db.Pool, u.ID, s.box.Seal(secret, totpAD(u.ID))); err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: EncodeTOTPSecret(secret), URI: TOTPURI(secret, totpIssuer, u.Email)}, nil
}

// EnableTOTP confirms a started setup with a code from the authenticator
// app, turns two-factor authentication on and returns the recovery codes,
// which are shown only this once. The user's other sessions end: they
// started without the second step.
func (s *Service) EnableTOTP(ctx context.Context, userID, keepSession uuid.UUID, code string) ([]string, error) {
	var codes []string
	err := pgx.BeginFunc(ctx, s.db.Pool, func(tx pgx.Tx) error {
		st, err := store.LockTwoFactor(ctx, tx, userID)
		if err != nil {
			return err
		}
		if st.EnabledAt != nil {
			return ErrTwoFactorOn
		}
		if st.Pending == nil || st.PendingAt == nil || time.Since(*st.PendingAt) > pendingTOTPTTL {
			return ErrTwoFactorSetupExpired
		}
		secret, err := s.box.Open(st.Pending, totpAD(userID))
		if err != nil {
			return ErrTwoFactorSetupExpired
		}
		step, ok := MatchTOTP(secret, code, time.Now(), 0)
		if !ok {
			return ErrInvalidSecondFactor
		}
		if err := store.EnableTOTP(ctx, tx, userID, step); err != nil {
			return err
		}
		if _, err := store.DeleteOtherSessions(ctx, tx, userID, keepSession); err != nil {
			return err
		}
		codes, err = s.replaceRecoveryCodes(ctx, tx, userID)
		return err
	})
	return codes, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]string, error) {
	codes := NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = s.recoveryHash(userID, NormalizeRecoveryCode(c))
	}
	return codes, store.ReplaceRecoveryCodes(ctx, tx, userID, hashes)
}

// DisableTOTP turns two-factor authentication off after checking a code
// from the authenticator app or a recovery code, and ends the user's other
// sessions.
func (s *Service) DisableTOTP(ctx context.Context, userID, keepSession uuid.UUID, code string) error {
	return s.withSecondFactor(ctx, userID, code, func(tx pgx.Tx) error {
		if err := store.DisableTwoFactor(ctx, tx, userID); err != nil {
			return err
		}
		_, err := store.DeleteOtherSessions(ctx, tx, userID, keepSession)
		return err
	})
}

// RegenerateRecoveryCodes replaces the recovery codes after checking a
// code and returns the new ones.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	var codes []string
	err := s.withSecondFactor(ctx, userID, code, func(tx pgx.Tx) error {
		var err error
		codes, err = s.replaceRecoveryCodes(ctx, tx, userID)
		return err
	})
	return codes, err
}

// withSecondFactor checks a code of a user with two-factor authentication
// on, then runs action in the same transaction. A wrong code is committed
// as a failure (every lockoutEvery failures lock the second step) and
// reported as ErrInvalidSecondFactor; a locked step reports
// TooManyAttemptsError.
func (s *Service) withSecondFactor(ctx context.Context, userID uuid.UUID, code string, action func(pgx.Tx) error) error {
	var verdict error
	err := pgx.BeginFunc(ctx, s.db.Pool, func(tx pgx.Tx) error {
		st, err := store.LockTwoFactor(ctx, tx, userID)
		if err != nil {
			return err
		}
		if st.EnabledAt == nil || st.Secret == nil {
			return ErrTwoFactorOff
		}
		now := time.Now()
		if st.LockedUntil != nil && st.LockedUntil.After(now) {
			return &TooManyAttemptsError{RetryAfter: st.LockedUntil.Sub(now)}
		}
		step, ok, err := s.checkSecondFactor(ctx, tx, userID, st, code, now)
		if err != nil {
			return err
		}
		if !ok {
			failures := st.Failures + 1
			var until *time.Time
			if failures%lockoutEvery == 0 {
				t := now.Add(lockout(failures))
				until = &t
			}
			verdict = ErrInvalidSecondFactor
			return store.RecordSecondFactorFailure(ctx, tx, userID, failures, until)
		}
		if err := store.AcceptSecondFactor(ctx, tx, userID, step); err != nil {
			return err
		}
		return action(tx)
	})
	if err != nil {
		return err
	}
	return verdict
}

// checkSecondFactor checks a recovery code (used up when it matches) or a
// TOTP code; step is the TOTP time step, 0 for recovery codes.
func (s *Service) checkSecondFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, st *store.TwoFactorState, code string, now time.Time) (int64, bool, error) {
	if rc := NormalizeRecoveryCode(code); rc != "" {
		ok, err := store.UseRecoveryCode(ctx, tx, userID, s.recoveryHash(userID, rc))
		return 0, ok, err
	}
	secret, err := s.box.Open(st.Secret, totpAD(userID))
	if err != nil {
		return 0, false, fmt.Errorf("two-factor secret of user %s cannot be decrypted; if IZCI_SECRET_KEY changed, "+
			"reset it with \"izci user reset-2fa\": %w", userID, err)
	}
	step, ok := MatchTOTP(secret, code, now, st.LastStep)
	return step, ok, nil
}

// lockout is how long the second step stays locked after failures wrong
// codes in a row: 5 minutes after the first lockoutEvery, doubling up to a
// day.
func lockout(failures int) time.Duration {
	n := failures/lockoutEvery - 1
	if n < 0 {
		n = 0
	}
	if n >= 9 {
		return 24 * time.Hour
	}
	return min(5*time.Minute<<n, 24*time.Hour)
}

// BeginChallenge starts the second step of a sign-in and returns the
// cookie that carries it to CompleteChallenge.
func (s *Service) BeginChallenge(ctx context.Context, userID uuid.UUID) (*http.Cookie, error) {
	token, err := store.CreateLoginChallenge(ctx, s.db.Pool, userID, challengeTTL)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name: ChallengeCookie, Value: token, Path: "/", MaxAge: int(challengeTTL.Seconds()),
		HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	}, nil
}

// ClearChallengeCookie expires the challenge cookie.
func (s *Service) ClearChallengeCookie() *http.Cookie {
	return &http.Cookie{Name: ChallengeCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode}
}

// CompleteChallenge checks the second step of the sign-in whose challenge
// cookie h carries and returns the user to start a session for. The
// challenge is used up on success.
func (s *Service) CompleteChallenge(ctx context.Context, h http.Header, code string) (*store.User, error) {
	token := ""
	if cookies, err := http.ParseCookie(h.Get("Cookie")); err == nil {
		for _, c := range cookies {
			if c.Name == ChallengeCookie {
				token = c.Value
			}
		}
	}
	if token == "" {
		return nil, ErrChallengeExpired
	}
	userID, err := store.LoginChallengeUser(ctx, s.db.Pool, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrChallengeExpired
	}
	if err != nil {
		return nil, err
	}
	err = s.withSecondFactor(ctx, userID, code, func(tx pgx.Tx) error {
		return store.DeleteLoginChallenge(ctx, tx, token)
	})
	if errors.Is(err, ErrTwoFactorOff) {
		return nil, ErrChallengeExpired
	}
	if err != nil {
		return nil, err
	}
	u, err := store.GetUser(ctx, s.db.Pool, userID)
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	_ = store.MarkUserLogin(ctx, s.db.Pool, u.ID)
	return u, nil
}
