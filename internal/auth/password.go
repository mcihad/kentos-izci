// Package auth implements sign-in (passwords, sessions, OpenID Connect),
// personal access tokens and the principal carried through requests.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2024 recommendation: m=19 MiB, t=2, p=1).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLength is the minimum accepted password length.
const MinPasswordLength = 10

// ErrWeakPassword rejects passwords that are too short or too common.
var ErrWeakPassword = fmt.Errorf("password must be at least %d characters and not a common password", MinPasswordLength)

var commonPasswords = map[string]bool{
	"1234567890": true, "0123456789": true, "qwertyuiop": true, "password12": true, "password123": true,
	"parola1234": true, "sifre12345": true, "şifre12345": true, "1111111111": true, "abcdefghij": true,
	"izci123456": true, "kentos1234": true, "admin12345": true, "welcome123": true,
}

// ValidatePassword checks the password policy.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength || commonPasswords[strings.ToLower(pw)] {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword returns an argon2id hash in PHC string format.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("malformed password hash")

// VerifyPassword checks pw against a PHC argon2id hash in constant time.
func VerifyPassword(hash, pw string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(pw), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified when a user does not exist, so that response times
// do not reveal which emails have accounts.
var dummyHash, _ = HashPassword("izci-timing-equalizer")
