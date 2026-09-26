package config

import (
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
)

func TestSecretKeyUnmarshal(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	cases := map[string]string{
		"std base64": base64.StdEncoding.EncodeToString(raw),
		"url base64": base64.RawURLEncoding.EncodeToString(raw),
		"hex":        "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var k SecretKey
			if err := k.UnmarshalText([]byte(in)); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if string(k) != string(raw) {
				t.Fatalf("decoded key mismatch")
			}
			if k.String() != "[redacted]" {
				t.Fatalf("String() must redact, got %q", k.String())
			}
		})
	}

	var short SecretKey
	if err := short.UnmarshalText([]byte(base64.StdEncoding.EncodeToString([]byte("too short")))); err == nil {
		t.Fatal("expected error for a key that is not 32 bytes")
	}
}

func TestPrefixList(t *testing.T) {
	var p PrefixList
	if err := p.UnmarshalText([]byte(" 10.0.0.0/8, 127.0.0.1 ,::1,")); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(p) != 3 {
		t.Fatalf("want 3 prefixes, got %d", len(p))
	}
	for _, ip := range []string{"10.1.2.3", "127.0.0.1", "::1", "::ffff:10.9.9.9"} {
		if !p.Contains(netip.MustParseAddr(ip)) {
			t.Errorf("%s should be trusted", ip)
		}
	}
	if p.Contains(netip.MustParseAddr("192.168.1.1")) {
		t.Error("192.168.1.1 must not be trusted")
	}
	if err := p.UnmarshalText([]byte("not-an-ip")); err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestLoadDefaultsAndValidation(t *testing.T) {
	t.Setenv("IZCI_ENV", "development")
	t.Setenv("IZCI_PUBLIC_URL", "http://localhost:5300/")
	t.Setenv("IZCI_SECRET_KEY", "")
	if _, err := Load(t.TempDir() + "/missing.env"); err == nil {
		t.Fatal("an explicit env file that does not exist must be an error")
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PublicURL != "http://localhost:5300" {
		t.Errorf("trailing slash must be trimmed, got %q", cfg.PublicURL)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("development must default to text logs, got %q", cfg.Log.Format)
	}
	if !cfg.SecretGenerated() || len(cfg.SecretKey) != 32 {
		t.Error("development without IZCI_SECRET_KEY must generate an ephemeral key")
	}

	t.Setenv("IZCI_ENV", "production")
	_, err = Load("")
	if err == nil || !strings.Contains(err.Error(), "IZCI_SECRET_KEY") {
		t.Fatalf("production without a secret key must fail, got %v", err)
	}

	t.Setenv("IZCI_ENV", "staging")
	if _, err = Load(""); err == nil {
		t.Fatal("unknown IZCI_ENV must fail")
	}
}
