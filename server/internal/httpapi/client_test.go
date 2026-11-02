package httpapi

import (
	"crypto/tls"
	"net/http/httptest"
	"net/netip"
	"testing"
)

var proxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}

func TestClientIsWhoeverConnectedWithoutATrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := clientOf(r, nil).key; got != "203.0.113.7" {
		t.Fatalf("%q", got)
	}
	if got := clientOf(r, proxies).key; got != "203.0.113.7" {
		t.Fatalf("an untrusted peer chose its own address: %q", got)
	}
}

func TestClientBehindATrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:5000"
	// the left entries are whatever the client claimed; the right ones were added by proxies we trust
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.9, 10.0.0.3")
	if got := clientOf(r, proxies).key; got != "198.51.100.9" {
		t.Fatalf("%q", got)
	}
	r.Header.Del("X-Forwarded-For")
	if got := clientOf(r, proxies).key; got != "10.0.0.2" {
		t.Fatalf("no header: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "garbage")
	if got := clientOf(r, proxies).key; got != "10.0.0.2" {
		t.Fatalf("garbage header: %q", got)
	}
}

func TestIPv6ClientsCountPerNetwork(t *testing.T) {
	a, b := httptest.NewRequest("GET", "/", nil), httptest.NewRequest("GET", "/", nil)
	a.RemoteAddr, b.RemoteAddr = "[2001:db8:1:2::1]:5000", "[2001:db8:1:2:ffff::9]:5000"
	if clientOf(a, nil).key != clientOf(b, nil).key || clientOf(a, nil).key != "2001:db8:1:2::/64" {
		t.Fatalf("%q %q", clientOf(a, nil).key, clientOf(b, nil).key)
	}
}

func TestHTTPSIsKnownDirectlyOrFromATrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5000"
	r.Header.Set("X-Forwarded-Proto", "https")
	if clientOf(r, proxies).https {
		t.Fatal("an untrusted peer said it was https")
	}
	r.TLS = &tls.ConnectionState{}
	if !clientOf(r, nil).https {
		t.Fatal("a TLS connection is https")
	}
	p := httptest.NewRequest("GET", "/", nil)
	p.RemoteAddr = "10.0.0.2:5000"
	p.Header.Set("X-Forwarded-Proto", "https")
	if !clientOf(p, proxies).https {
		t.Fatal("the trusted proxy said https")
	}
}
