package clientip

import (
	"net/http"
	"testing"

	"github.com/mcihad/kentos-izci/internal/config"
)

func TestResolve(t *testing.T) {
	var trusted config.PrefixList
	if err := trusted.UnmarshalText([]byte("10.0.0.0/8,127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		remote string
		xff    string
		real   string
		want   string
	}{
		{"direct client", "203.0.113.7:5123", "", "", "203.0.113.7"},
		{"untrusted peer cannot spoof", "203.0.113.7:5123", "1.2.3.4", "", "203.0.113.7"},
		{"trusted proxy with xff", "10.0.0.2:443", "198.51.100.9, 10.0.0.5", "", "198.51.100.9"},
		{"spoofed leftmost is ignored", "10.0.0.2:443", "6.6.6.6, 198.51.100.9", "", "198.51.100.9"},
		{"all hops trusted", "10.0.0.2:443", "10.1.1.1, 10.0.0.5", "", "10.1.1.1"},
		{"x-real-ip fallback", "127.0.0.1:9000", "", "198.51.100.10", "198.51.100.10"},
		{"ipv6 peer", "[2001:db8::1]:443", "", "", "2001:db8::1"},
		{"garbage xff falls back to peer", "10.0.0.2:443", "not-an-ip", "", "10.0.0.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.xff != "" {
				h.Set("X-Forwarded-For", c.xff)
			}
			if c.real != "" {
				h.Set("X-Real-IP", c.real)
			}
			if got := Resolve(c.remote, h, trusted).String(); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestAnonymize(t *testing.T) {
	for in, want := range map[string]string{"203.0.113.77": "203.0.113.0", "2001:db8:abcd:12::1": "2001:db8:abcd::"} {
		if got := Anonymize(parseHostPort(in)).String(); got != want {
			t.Errorf("Anonymize(%s) = %s, want %s", in, got, want)
		}
	}
}
