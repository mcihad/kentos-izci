package auth

import (
	"strings"
	"testing"
	"time"
)

// TestTOTPCode checks the SHA-1 test vectors of RFC 6238 (appendix B),
// truncated to six digits.
func TestTOTPCode(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got := TOTPCode(secret, TOTPStep(time.Unix(unix, 0))); got != want {
			t.Errorf("T=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestMatchTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	step := TOTPStep(now)
	code := TOTPCode(secret, step)
	if got, ok := MatchTOTP(secret, code, now, 0); !ok || got != step {
		t.Fatalf("current code: %d %v", got, ok)
	}
	if _, ok := MatchTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok {
		t.Error("codes may be typed with a space")
	}
	// One step of clock drift either way is accepted, two are not.
	for d, want := range map[int64]bool{-2: false, -1: true, 1: true, 2: false} {
		if _, ok := MatchTOTP(secret, TOTPCode(secret, step+d), now, 0); ok != want {
			t.Errorf("drift %d: %v, want %v", d, ok, want)
		}
	}
	// A used step is never accepted again.
	if _, ok := MatchTOTP(secret, code, now, step); ok {
		t.Error("replayed code accepted")
	}
	if _, ok := MatchTOTP(secret, TOTPCode(secret, step+1), now, step); !ok {
		t.Error("next step after a used one must be accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := MatchTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTOTPURI(t *testing.T) {
	secret := []byte("12345678901234567890")
	got := TOTPURI(secret, "İzci", "ayşe@example.com")
	want := "otpauth://totp/%C4%B0zci:ay%C5%9Fe@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=%C4%B0zci&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Errorf("uri:\n%s\nwant\n%s", got, want)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes()
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("%d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("format: %q", c)
		}
		n := NormalizeRecoveryCode(c)
		if n != strings.ReplaceAll(c, "-", "") || seen[n] {
			t.Errorf("normalize %q: %q (duplicate %v)", c, n, seen[n])
		}
		seen[n] = true
		if NormalizeRecoveryCode(" "+strings.ToUpper(c)+" ") != n {
			t.Errorf("typed %q in upper case with spaces", c)
		}
	}
	for _, bad := range []string{"123456", "abcde-fghi", "abcde-fghij1", "abcde-fghio"} {
		if NormalizeRecoveryCode(bad) != "" {
			t.Errorf("%q is not a recovery code", bad)
		}
	}
}

func TestLockout(t *testing.T) {
	for failures, want := range map[int]time.Duration{
		5: 5 * time.Minute, 10: 10 * time.Minute, 15: 20 * time.Minute, 40: 640 * time.Minute,
		45: 1280 * time.Minute, 50: 24 * time.Hour, 5000: 24 * time.Hour,
	} {
		if got := lockout(failures); got != want {
			t.Errorf("lockout(%d) = %v, want %v", failures, got, want)
		}
	}
}
