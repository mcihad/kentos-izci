package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TOTP parameters: the RFC 6238 defaults (HMAC-SHA1) every authenticator
// app supports.
const (
	totpDigits      = 6
	totpPeriod      = 30 // seconds per step
	totpSkew        = 1  // steps accepted before and after the current one (clock drift)
	totpSecretBytes = 20 // 160 bits, as RFC 4226 recommends
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random TOTP secret.
func NewTOTPSecret() []byte {
	b := make([]byte, totpSecretBytes)
	_, _ = rand.Read(b) // never fails (Go 1.24+ crashes instead)
	return b
}

// EncodeTOTPSecret returns the base32 form of a secret, as typed into an
// authenticator app when the QR code cannot be scanned.
func EncodeTOTPSecret(secret []byte) string { return totpEncoding.EncodeToString(secret) }

// TOTPStep returns the time step of t.
func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// TOTPCode returns the code of a time step: HOTP (RFC 4226) with the step
// as the counter.
func TOTPCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// MatchTOTP checks a code against the steps around t and returns the step
// it belongs to. Steps up to lastStep were used already and never match
// again, so an observed code cannot be replayed.
func MatchTOTP(secret []byte, code string, t time.Time, lastStep int64) (int64, bool) {
	code = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, code)
	if len(code) != totpDigits {
		return 0, false
	}
	now := TOTPStep(t)
	for step := now - totpSkew; step <= now+totpSkew; step++ {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// TOTPURI returns the otpauth:// URI (Google Authenticator's key URI format)
// authenticator apps read from a QR code.
func TOTPURI(secret []byte, issuer, account string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) +
		"?secret=" + EncodeTOTPSecret(secret) +
		"&issuer=" + esc(issuer) +
		"&algorithm=SHA1&digits=" + strconv.Itoa(totpDigits) + "&period=" + strconv.Itoa(totpPeriod)
}

// Recovery codes sign in when the authenticator app is lost: ten
// single-use codes of ten characters from an alphabet without look-alike
// characters (about 49 bits each), shown as "xxxxx-xxxxx".
const (
	RecoveryCodeCount = 10
	recoveryCodeLen   = 10
	recoveryAlphabet  = "23456789abcdefghjkmnpqrstuvwxyz"
)

// NewRecoveryCodes returns a fresh set of recovery codes.
func NewRecoveryCodes() []string {
	out := make([]string, RecoveryCodeCount)
	max := big.NewInt(int64(len(recoveryAlphabet)))
	for i := range out {
		var b strings.Builder
		for j := range recoveryCodeLen {
			if j == recoveryCodeLen/2 {
				b.WriteByte('-')
			}
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				panic(fmt.Errorf("auth: crypto/rand failed: %w", err))
			}
			b.WriteByte(recoveryAlphabet[n.Int64()])
		}
		out[i] = b.String()
	}
	return out
}

// NormalizeRecoveryCode lowercases a typed recovery code and drops
// separators; it returns "" when the input cannot be a recovery code (a
// TOTP code, for instance).
func NormalizeRecoveryCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ' || r == '-':
		case strings.ContainsRune(recoveryAlphabet, r):
			b.WriteRune(r)
		default:
			return ""
		}
	}
	if b.Len() != recoveryCodeLen {
		return ""
	}
	return b.String()
}
