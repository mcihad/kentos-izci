// Package clientip determines the address of the client that made a
// request, honoring forwarding headers only from trusted proxies.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/mcihad/kentos-izci/internal/config"
)

// Resolve returns the client address of a request.
//
// remoteAddr is the direct peer ("ip:port" or "ip"). When the peer is a
// trusted proxy, X-Forwarded-For is walked from right to left, skipping
// trusted proxies, and the first untrusted address wins; X-Real-IP is used
// when X-Forwarded-For is absent. Headers from untrusted peers are ignored,
// so clients cannot spoof their address. The zero Addr means unknown.
func Resolve(remoteAddr string, h http.Header, trusted config.PrefixList) netip.Addr {
	peer := parseHostPort(remoteAddr)
	if !peer.IsValid() || !trusted.Contains(peer) {
		return peer
	}
	if xff := h.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addr := parseHostPort(strings.TrimSpace(hops[i]))
			if !addr.IsValid() {
				break
			}
			if !trusted.Contains(addr) || i == 0 {
				return addr
			}
		}
	}
	if real := parseHostPort(strings.TrimSpace(h.Get("X-Real-IP"))); real.IsValid() {
		return real
	}
	return peer
}

func parseHostPort(s string) netip.Addr {
	if s == "" {
		return netip.Addr{}
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap()
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	if i := strings.IndexByte(s, '%'); i >= 0 { // IPv6 zone
		s = s[:i]
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// Anonymize removes the host part of an address: IPv4 keeps /24, IPv6 keeps
// /48.
func Anonymize(a netip.Addr) netip.Addr {
	if !a.IsValid() {
		return a
	}
	bits := 48
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil {
		return netip.Addr{}
	}
	return p.Addr()
}
