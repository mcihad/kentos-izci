package secret

import (
	"bytes"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		k := NewAPIKey()
		if !strings.HasPrefix(k, PrefixAPIKey) || len(k) != len(PrefixAPIKey)+32 {
			t.Fatalf("bad api key %q", k)
		}
		if seen[k] {
			t.Fatal("duplicate key generated")
		}
		seen[k] = true
		for _, c := range k[len(PrefixAPIKey):] {
			if !strings.ContainsRune(base62, c) {
				t.Fatalf("non-base62 character %q in %q", c, k)
			}
		}
	}
	if got := DisplayPrefix("izk_4f9c2aXXXXXXXXXXXXXXXXXXXXXXXXXX"); got != "izk_4f9c2a" {
		t.Errorf("DisplayPrefix = %q", got)
	}
	if !HasPrefix(NewAccessToken(), PrefixAccessToken) || HasPrefix("izp_short", PrefixAccessToken) {
		t.Error("HasPrefix misclassified a token")
	}
	if !bytes.Equal(Hash("a"), Hash("a")) || bytes.Equal(Hash("a"), Hash("b")) {
		t.Error("Hash must be deterministic and distinguish inputs")
	}
}

func TestBox(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal([]byte("github_pat_secret"), []byte("integration-1"))
	if bytes.Contains(sealed, []byte("github_pat_secret")) {
		t.Fatal("ciphertext leaks the plaintext")
	}
	plain, err := box.Open(sealed, []byte("integration-1"))
	if err != nil || string(plain) != "github_pat_secret" {
		t.Fatalf("round trip failed: %q %v", plain, err)
	}
	if _, err := box.Open(sealed, []byte("integration-2")); err == nil {
		t.Fatal("different additional data must not decrypt")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed, []byte("integration-1")); err == nil {
		t.Fatal("tampered ciphertext must not decrypt")
	}
	other, _ := NewBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(box.Seal([]byte("x"), nil), nil); err == nil {
		t.Fatal("another master key must not decrypt")
	}
	if _, err := NewBox([]byte("short")); err == nil {
		t.Fatal("short master key must be rejected")
	}
}

func TestSign(t *testing.T) {
	box, _ := NewBox(bytes.Repeat([]byte{1}, 32))
	sig := box.Sign("r:issue-123", 9)
	if len(sig) != 12 {
		t.Fatalf("9 bytes should encode to 12 base64url chars, got %d", len(sig))
	}
	if !box.Verify("r:issue-123", sig, 9) {
		t.Fatal("valid signature rejected")
	}
	if box.Verify("r:issue-124", sig, 9) {
		t.Fatal("signature accepted for another message")
	}
}
