package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// client is who sent a request: key groups requests for rate limiting, https says whether the browser used TLS.
type client struct {
	key   string
	https bool
}

// clientOf believes X-Forwarded-For and X-Forwarded-Proto only when they come from a proxy in trusted, and then takes
// the nearest address that isn't one of those proxies; anything further left was written by the client itself.
func clientOf(r *http.Request, trusted []netip.Prefix) client {
	peer := peerAddr(r.RemoteAddr)
	c := client{key: keyOf(peer, r.RemoteAddr), https: r.TLS != nil}
	if !peer.IsValid() || !within(peer, trusted) {
		return c
	}
	c.https = c.https || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		addr = addr.Unmap()
		if !within(addr, trusted) {
			c.key = keyOf(addr, "")
			break
		}
	}
	return c
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, _ := netip.ParseAddr(host)
	return addr.Unmap()
}

// keyOf counts an IPv6 client by its /64, which is what a single home or server usually gets.
func keyOf(addr netip.Addr, fallback string) string {
	if !addr.IsValid() {
		return fallback
	}
	if addr.Is6() {
		p, _ := addr.Prefix(64)
		return p.String()
	}
	return addr.String()
}

func within(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
