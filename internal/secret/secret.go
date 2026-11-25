// Package secret generates and protects secrets: random tokens (API keys,
// personal access tokens, session and invitation tokens), their storage
// hashes, AES-256-GCM encryption of third-party credentials at rest, and HMAC
// signatures for short-lived values such as OAuth state and Telegram button
// payloads.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
)

// Token prefixes identify what a secret is (and let secret scanners find
// leaked ones).
const (
	PrefixAPIKey        = "izk_" // project ingest key
	PrefixAccessToken   = "izp_" // personal access token
	PrefixSession       = "izs_" // browser session cookie value
	PrefixInvitation    = "izi_" // invitation link token
	PrefixSignup        = "izv_" // sign-up e-mail verification link token
	apiKeyRandomLen     = 32
	accessTokenRandLen  = 40
	sessionRandomLen    = 43
	invitationRandomLen = 32
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Random returns n characters drawn uniformly from [0-9A-Za-z] using
// crypto/rand. 32 characters carry ~190 bits of entropy.
func Random(n int) string {
	var b strings.Builder
	b.Grow(n)
	max := big.NewInt(int64(len(base62)))
	for range n {
		i, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Errorf("secret: crypto/rand failed: %w", err))
		}
		b.WriteByte(base62[i.Int64()])
	}
	return b.String()
}

// NewAPIKey returns a fresh project ingest key ("izk_" + 32 chars).
func NewAPIKey() string { return PrefixAPIKey + Random(apiKeyRandomLen) }

// NewAccessToken returns a fresh personal access token ("izp_" + 40 chars).
func NewAccessToken() string { return PrefixAccessToken + Random(accessTokenRandLen) }

// NewSessionToken returns a fresh session cookie value.
func NewSessionToken() string { return PrefixSession + Random(sessionRandomLen) }

// NewInvitationToken returns a fresh invitation token.
func NewInvitationToken() string { return PrefixInvitation + Random(invitationRandomLen) }

// NewSignupToken returns a fresh token for a sign-up verification link.
func NewSignupToken() string { return PrefixSignup + Random(invitationRandomLen) }

// Hash returns the SHA-256 digest used to store and look up a token. Tokens
// are high-entropy random strings, so a fast unsalted hash is appropriate
// (unlike passwords).
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// DisplayPrefix returns the leading part of a token that is safe to show,
// e.g. "izk_4f9c2a" (prefix plus 6 characters).
func DisplayPrefix(token string) string {
	const visible = 6
	i := strings.IndexByte(token, '_')
	if i < 0 || len(token) < i+1+visible {
		if len(token) > visible {
			return token[:visible]
		}
		return token
	}
	return token[:i+1+visible]
}

// HasPrefix reports whether token looks like the given kind of secret.
func HasPrefix(token, prefix string) bool {
	return strings.HasPrefix(token, prefix) && len(token) > len(prefix)+16
}

// Box encrypts small secrets (third-party tokens) with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
	key  []byte
}

// NewBox derives purpose-specific keys from the 32-byte master key.
func NewBox(masterKey []byte) (*Box, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("secret: master key must be 32 bytes")
	}
	encKey := derive(masterKey, "izci/encryption/v1")
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, key: derive(masterKey, "izci/signing/v1")}, nil
}

// derive computes HMAC-SHA256(master, label): independent keys per purpose.
func derive(master []byte, label string) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// Seal encrypts plaintext. additionalData (e.g. the owning row id) is
// authenticated but not encrypted, binding the ciphertext to its context.
// The result is nonce || ciphertext.
func (b *Box) Seal(plaintext, additionalData []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(plaintext)+b.aead.Overhead())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(fmt.Errorf("secret: crypto/rand failed: %w", err))
	}
	return b.aead.Seal(nonce, nonce, plaintext, additionalData)
}

// ErrDecrypt means the ciphertext was tampered with, belongs to another
// context or was sealed with another master key.
var ErrDecrypt = errors.New("secret: cannot decrypt value")

// Open decrypts a value produced by Seal with the same additionalData.
func (b *Box) Open(sealed, additionalData []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	out, err := b.aead.Open(nil, sealed[:n], sealed[n:], additionalData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

// Sign returns a URL-safe HMAC signature of msg, truncated to n bytes
// (n ≥ 8). Short signatures fit into Telegram's 64-byte callback data.
func (b *Box) Sign(msg string, n int) string {
	m := hmac.New(sha256.New, b.key)
	m.Write([]byte(msg))
	sum := m.Sum(nil)
	if n < 8 {
		n = 8
	}
	if n > len(sum) {
		n = len(sum)
	}
	return base64.RawURLEncoding.EncodeToString(sum[:n])
}

// codeAlphabet has no look-alike characters (0/O, 1/I/L).
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// Code derives a code for people to type from label: n characters in
// groups of four ("7KQ2-…"), the same for every process with the same
// master key.
func (b *Box) Code(label string, n int) string {
	m := hmac.New(sha256.New, b.key)
	m.Write([]byte("izci/code/" + label))
	sum := m.Sum(nil)
	var out strings.Builder
	for i := 0; i < n && i < len(sum); i++ {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteByte(codeAlphabet[int(sum[i])%len(codeAlphabet)])
	}
	return out.String()
}

// Verify checks a signature produced by Sign in constant time.
func (b *Box) Verify(msg, sig string, n int) bool {
	return hmac.Equal([]byte(b.Sign(msg, n)), []byte(sig))
}
