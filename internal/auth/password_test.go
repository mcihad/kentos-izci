package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format %q", hash)
	}
	if ok, err := VerifyPassword(hash, "correct horse battery"); !ok || err != nil {
		t.Fatalf("valid password rejected: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword(hash, "correct horse batterY"); ok {
		t.Fatal("wrong password accepted")
	}
	other, _ := HashPassword("correct horse battery")
	if other == hash {
		t.Fatal("hashes must be salted")
	}
	if _, err := VerifyPassword("$bcrypt$x", "x"); err == nil {
		t.Fatal("foreign hash formats must be rejected")
	}
}

func TestPasswordPolicy(t *testing.T) {
	for pw, ok := range map[string]bool{
		"short":             false,
		"1234567890":        false,
		"Parola1234":        false,
		"ğüşiöçĞÜŞİ":        true, // 10 runes, multi-byte
		"uzun ve güçlü bir": true,
	} {
		if err := ValidatePassword(pw); (err == nil) != ok {
			t.Errorf("ValidatePassword(%q) = %v, want ok=%v", pw, err, ok)
		}
	}
}
