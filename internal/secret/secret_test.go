package secret

import (
	"bytes"
	"slices"
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

func TestCode(t *testing.T) {
	a, _ := NewBox(make([]byte, 32))
	b, _ := NewBox(make([]byte, 32))
	other := make([]byte, 32)
	other[0] = 1
	c, _ := NewBox(other)
	code := a.Code("setup", 16)
	if len(code) != 19 || strings.Count(code, "-") != 3 || strings.ContainsAny(code, "01ILO") {
		t.Fatalf("format: %q", code)
	}
	if b.Code("setup", 16) != code {
		t.Error("the same master key must give the same code on every instance")
	}
	if c.Code("setup", 16) == code || a.Code("other", 16) == code {
		t.Error("codes must depend on the key and the label")
	}
}

func TestKeyring(t *testing.T) {
	key := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	old, _ := NewBox(key(1))
	ring, err := NewBox(key(2), key(1))
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := NewBox(key(2))
	ad := []byte("row")

	// Values of the previous key still open; new ones use the current key.
	sealed := old.Seal([]byte("token"), ad)
	if got, err := ring.Open(sealed, ad); err != nil || string(got) != "token" {
		t.Fatalf("open with the keyring: %q %v", got, err)
	}
	if _, err := fresh.Open(sealed, ad); err == nil {
		t.Fatal("the new key alone must not open the old value")
	}
	if _, err := fresh.Open(ring.Seal([]byte("x"), ad), ad); err != nil {
		t.Fatal("the keyring must seal with the current key")
	}
	resealed, changed, err := ring.Reseal(sealed, ad)
	if err != nil || !changed {
		t.Fatalf("reseal: %v %v", changed, err)
	}
	if got, err := fresh.Open(resealed, ad); err != nil || string(got) != "token" {
		t.Fatalf("resealed value: %q %v", got, err)
	}
	if same, changed, err := ring.Reseal(resealed, ad); err != nil || changed || !bytes.Equal(same, resealed) {
		t.Fatal("a value of the current key must be left as it is")
	}
	if _, _, err := fresh.Reseal(sealed, ad); err == nil {
		t.Fatal("a value of an unknown key cannot be resealed")
	}

	// Signatures of the previous key still verify.
	if !ring.Verify("msg", old.Sign("msg", 16), 16) || fresh.Verify("msg", old.Sign("msg", 16), 16) {
		t.Fatal("verify across keys")
	}
	if ring.Sign("msg", 16) != fresh.Sign("msg", 16) {
		t.Fatal("the keyring must sign with the current key")
	}

	// Keyed signatures name their key; lookups cover every key and the
	// unnamed form of earlier versions.
	if s := ring.KeyedSignature("code", 32); !strings.HasPrefix(s, ring.KeyID()+":") {
		t.Fatalf("keyed signature %q", s)
	}
	all := ring.KeyedSignatures("code", 32)
	for _, want := range []string{old.KeyedSignature("code", 32), old.Sign("code", 32), fresh.KeyedSignature("code", 32)} {
		if !slices.Contains(all, want) {
			t.Errorf("lookups lack %q", want)
		}
	}

	if _, err := NewBox(key(2), []byte("short")); err == nil || !strings.Contains(err.Error(), "previous key 1") {
		t.Fatalf("bad previous key: %v", err)
	}
}
