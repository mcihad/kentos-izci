package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mcihad/kentos-izci/internal/config"
)

func TestLocalURL(t *testing.T) {
	for addr, want := range map[string]string{
		":5300":          "http://127.0.0.1:5300",
		"0.0.0.0:5300":   "http://127.0.0.1:5300",
		"[::]:5300":      "http://[::1]:5300",
		"10.0.0.5:8080":  "http://10.0.0.5:8080",
		"localhost:5300": "http://localhost:5300",
	} {
		got, err := localURL(&config.Config{HTTP: config.HTTPConfig{Addr: addr}})
		if err != nil || got != want {
			t.Errorf("localURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	tls := &config.Config{HTTP: config.HTTPConfig{Addr: ":443", TLSCertFile: "c.pem", TLSKeyFile: "k.pem"}}
	if got, _ := localURL(tls); got != "https://127.0.0.1:443" {
		t.Errorf("TLS listener: %q", got)
	}
	if _, err := localURL(&config.Config{HTTP: config.HTTPConfig{Addr: "5300"}}); err == nil {
		t.Error("an address without a port must be rejected")
	}
}

func TestHealthcheck(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/readyz" && ready.Load():
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	run := func(args ...string) (string, error) {
		cmd := newHealthcheckCmd(&cliApp{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(append([]string{"--url", srv.URL}, args...))
		err := cmd.ExecuteContext(context.Background())
		return out.String(), err
	}
	if out, err := run(); err != nil || strings.TrimSpace(out) != "ok" {
		t.Fatalf("healthz: %q %v", out, err)
	}
	if _, err := run("--ready"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("readyz while not ready: %v", err)
	}
	ready.Store(true)
	if _, err := run("--ready"); err != nil {
		t.Fatalf("readyz: %v", err)
	}
	srv.Close()
	if _, err := run(); err == nil {
		t.Fatal("a stopped server must fail the check")
	}
}
