package config

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
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
	if cfg.Auth.SessionTTL != 720*time.Hour || cfg.Auth.SessionMaxAge != 720*time.Hour {
		t.Errorf("session lifetimes: %s idle, %s at most", cfg.Auth.SessionTTL, cfg.Auth.SessionMaxAge)
	}
	t.Setenv("AUTH_SESSION_MAX_AGE", "0s")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "AUTH_SESSION_MAX_AGE") {
		t.Fatalf("sessions without an end must fail, got %v", err)
	}
	t.Setenv("AUTH_SESSION_MAX_AGE", "720h")

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

func TestMetricsAndSelfMonitoring(t *testing.T) {
	t.Setenv("IZCI_ENV", "development")
	t.Setenv("IZCI_SECRET_KEY", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Token != "" || cfg.Self.Enabled() || cfg.Self.Environment != "development" {
		t.Fatalf("defaults: %+v %+v", cfg.Metrics, cfg.Self)
	}

	t.Setenv("IZCI_SELF_URL", "https://izci.example.com/")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "set together") {
		t.Fatalf("a URL without a key must fail, got %v", err)
	}
	t.Setenv("IZCI_SELF_KEY", "izp_personal_token")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "ingest key") {
		t.Fatalf("a personal access token must be refused, got %v", err)
	}
	t.Setenv("IZCI_SELF_KEY", "izk_project_key")
	t.Setenv("IZCI_SELF_ENVIRONMENT", "izci-production")
	if cfg, err = Load(""); err != nil {
		t.Fatal(err)
	}
	if !cfg.Self.Enabled() || cfg.Self.URL != "https://izci.example.com" || cfg.Self.Environment != "izci-production" {
		t.Fatalf("self: %+v", cfg.Self)
	}
	if s := fmt.Sprint(cfg.Self.Key); s != "[redacted]" {
		t.Errorf("the key must not print, got %q", s)
	}
	t.Setenv("IZCI_SELF_URL", "izci.example.com")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "IZCI_SELF_URL") {
		t.Fatalf("a URL without scheme must fail, got %v", err)
	}
}

func TestSuperadminTwoFactor(t *testing.T) {
	for _, c := range []struct {
		env   Environment
		value string
		want  bool
	}{
		{EnvProduction, "auto", true},
		{EnvDevelopment, "auto", false},
		{EnvDevelopment, "true", true},
		{EnvProduction, "false", false},
	} {
		cfg := &Config{Env: c.env, Auth: AuthConfig{RequireTwoFactorSuperadmin: c.value}}
		if got := cfg.SuperadminTwoFactor(); got != c.want {
			t.Errorf("%s with %s: %v, want %v", c.env, c.value, got, c.want)
		}
	}
	t.Setenv("IZCI_ENV", "development")
	t.Setenv("AUTH_REQUIRE_2FA_SUPERADMIN", "sometimes")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "AUTH_REQUIRE_2FA_SUPERADMIN") {
		t.Fatalf("unknown value must fail, got %v", err)
	}
}

func TestOpenSignupNeedsSMTP(t *testing.T) {
	t.Setenv("IZCI_ENV", "production")
	t.Setenv("IZCI_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("AUTH_SIGNUP", "open")
	t.Setenv("SMTP_HOST", "")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "AUTH_SIGNUP=open needs SMTP_HOST") {
		t.Fatalf("open sign-up without SMTP in production: %v", err)
	}
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_FROM", "izci@example.com")
	if _, err := Load(""); err != nil && strings.Contains(err.Error(), "AUTH_SIGNUP") {
		t.Fatalf("open sign-up with SMTP: %v", err)
	}
	t.Setenv("IZCI_ENV", "development")
	t.Setenv("SMTP_HOST", "")
	if _, err := Load(""); err != nil && strings.Contains(err.Error(), "AUTH_SIGNUP") {
		t.Fatalf("development logs the links instead: %v", err)
	}
}
