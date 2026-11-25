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
	"encoding/hex"
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
	PrefixPasswordReset = "izr_" // password reset link token
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

// NewPasswordResetToken returns a fresh token for a password reset link.
func NewPasswordResetToken() string { return PrefixPasswordReset + Random(invitationRandomLen) }

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

// Box encrypts small secrets (third-party tokens) with AES-256-GCM and
// signs with HMAC-SHA256. It holds a keyring: the current master key
// (IZCI_SECRET_KEY) seals and signs; previous master keys
// (IZCI_SECRET_KEY_PREVIOUS) still open what they sealed and verify what
// they signed, so that the master key can be rotated without losing stored
// secrets ("izci secrets rotate" re-seals them).
type Box struct {
	// keys[0] is the current key.
	keys []boxKey
}

type boxKey struct {
	aead cipher.AEAD
	sign []byte
	id   string
}

// NewBox derives purpose-specific keys from the 32-byte master key and the
// previous master keys, newest first.
func NewBox(masterKey []byte, previous ...[]byte) (*Box, error) {
	b := &Box{}
	for i, master := range append([][]byte{masterKey}, previous...) {
		k, err := newBoxKey(master)
		if err != nil {
			if i > 0 {
				return nil, fmt.Errorf("secret: previous key %d: %w", i, err)
			}
			return nil, err
		}
		b.keys = append(b.keys, k)
	}
	return b, nil
}

func newBoxKey(master []byte) (boxKey, error) {
	if len(master) != 32 {
		return boxKey{}, errors.New("secret: master key must be 32 bytes")
	}
	block, err := aes.NewCipher(derive(master, "izci/encryption/v1"))
	if err != nil {
		return boxKey{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return boxKey{}, err
	}
	return boxKey{aead: aead, sign: derive(master, "izci/signing/v1"), id: hex.EncodeToString(derive(master, "izci/key-id/v1")[:4])}, nil
}

// derive computes HMAC-SHA256(master, label): independent keys per purpose.
func derive(master []byte, label string) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// KeyID names the current master key (8 hex characters) without revealing
// it.
func (b *Box) KeyID() string { return b.keys[0].id }

// Seal encrypts plaintext with the current key. additionalData (e.g. the
// owning row id) is authenticated but not encrypted, binding the
// ciphertext to its context. The result is nonce || ciphertext.
func (b *Box) Seal(plaintext, additionalData []byte) []byte {
	aead := b.keys[0].aead
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(fmt.Errorf("secret: crypto/rand failed: %w", err))
	}
	return aead.Seal(nonce, nonce, plaintext, additionalData)
}

// ErrDecrypt means the ciphertext was tampered with, belongs to another
// context or was sealed with a master key that is not in the keyring.
var ErrDecrypt = errors.New("secret: cannot decrypt value")

// Open decrypts a value produced by Seal with the same additionalData,
// sealed with any key of the keyring.
func (b *Box) Open(sealed, additionalData []byte) ([]byte, error) {
	out, _, err := b.open(sealed, additionalData)
	return out, err
}

// open also reports which key of the keyring opened the value.
func (b *Box) open(sealed, additionalData []byte) ([]byte, int, error) {
	for i, k := range b.keys {
		n := k.aead.NonceSize()
		if len(sealed) < n+k.aead.Overhead() {
			return nil, 0, ErrDecrypt
		}
		if out, err := k.aead.Open(nil, sealed[:n], sealed[n:], additionalData); err == nil {
			return out, i, nil
		}
	}
	return nil, 0, ErrDecrypt
}

// Reseal makes a sealed value one of the current key: values of previous
// keys are sealed again (changed), those of the current key are returned
// as they are.
func (b *Box) Reseal(sealed, additionalData []byte) (out []byte, changed bool, err error) {
	plain, key, err := b.open(sealed, additionalData)
	if err != nil {
		return nil, false, err
	}
	if key == 0 {
		return sealed, false, nil
	}
	return b.Seal(plain, additionalData), true, nil
}

// Sign returns a URL-safe HMAC signature of msg, truncated to n bytes
// (n ≥ 8). Short signatures fit into Telegram's 64-byte callback data.
func (b *Box) Sign(msg string, n int) string { return b.keys[0].signature(msg, n) }

func (k boxKey) signature(msg string, n int) string {
	m := hmac.New(sha256.New, k.sign)
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

// KeyedSignature is a signature that names its key ("<key id>:<signature>"),
// for values kept long, such as the hashes of recovery codes.
func (b *Box) KeyedSignature(msg string, n int) string {
	return b.keys[0].id + ":" + b.keys[0].signature(msg, n)
}

// KeyedSignatures are the forms msg may be stored in under any key of the
// keyring: keyed signatures, and plain ones from before keys were named.
func (b *Box) KeyedSignatures(msg string, n int) []string {
	out := make([]string, 0, 2*len(b.keys))
	for _, k := range b.keys {
		sig := k.signature(msg, n)
		out = append(out, k.id+":"+sig, sig)
	}
	return out
}

// codeAlphabet has no look-alike characters (0/O, 1/I/L).
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// Code derives a code for people to type from label: n characters in
// groups of four ("7KQ2-…"), the same for every process with the same
// master key.
func (b *Box) Code(label string, n int) string {
	m := hmac.New(sha256.New, b.keys[0].sign)
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

// Verify checks a signature produced by Sign with any key of the keyring,
// in constant time.
func (b *Box) Verify(msg, sig string, n int) bool {
	ok := false
	for _, k := range b.keys {
		ok = hmac.Equal([]byte(k.signature(msg, n)), []byte(sig)) || ok
	}
	return ok
}
