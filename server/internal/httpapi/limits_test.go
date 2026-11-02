package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func (e *testEnv) behindProxy() {
	e.srv.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
}

func login(t *testing.T, e *testEnv, password string, header ...string) int {
	t.Helper()
	return do(t, e.client(t), "POST", e.http.URL+"/api/login",
		map[string]string{"username": "maciej", "password": password}, header...).StatusCode
}

func TestASuccessfulLoginDoesNotWipeTheAddressesFailures(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	for i := 0; i < 3; i++ {
		login(t, e, "wrong password")
	}
	if got := login(t, e, "correct horse"); got != http.StatusNoContent {
		t.Fatalf("right password: %d", got)
	}
	login(t, e, "wrong password")
	if got := login(t, e, "wrong password"); got != http.StatusTooManyRequests {
		t.Fatalf("a success in between let more guesses through: %d", got)
	}
}

func TestGuessesAtOneAccountAreLimitedFromAnyAddress(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	e.behindProxy()
	last := 0
	for i := 0; i < 12; i++ {
		last = login(t, e, "wrong password", "X-Forwarded-For", "198.51.100."+string(rune('1'+i%9))+string(rune('0'+i/9)))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("12 guesses from 12 addresses: %d", last)
	}
}

func TestOneProxiedClientDoesNotLockOutAnother(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	e.behindProxy()
	for i := 0; i < 5; i++ {
		do(t, e.client(t), "POST", e.http.URL+"/api/setup", map[string]string{"code": "WRONG", "username": "x", "password": "y"},
			"X-Forwarded-For", "203.0.113.66")
	}
	if got := login(t, e, "correct horse", "X-Forwarded-For", "198.51.100.1"); got != http.StatusNoContent {
		t.Fatalf("the owner behind the same proxy: %d", got)
	}
}

func TestWrongPairingCodesDoNotBlockLogin(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	for i := 0; i < 5; i++ {
		do(t, e.client(t), "POST", e.http.URL+"/api/device/v1/pair", map[string]string{"code": "AAAAAA", "fw": "0.7.0"})
	}
	if got := login(t, e, "correct horse"); got != http.StatusNoContent {
		t.Fatalf("login after wrong pairing codes: %d", got)
	}
}

func TestCookiesAreSecureOverHTTPS(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	e.behindProxy()
	resp := do(t, e.client(t), "POST", e.http.URL+"/api/login",
		map[string]string{"username": "maciej", "password": "correct horse"}, "X-Forwarded-Proto", "https")
	if c := resp.Header.Get("Set-Cookie"); !strings.Contains(c, "Secure") {
		t.Fatalf("cookie over https: %s", c)
	}
	if h := resp.Header.Get("Strict-Transport-Security"); !strings.HasPrefix(h, "max-age=") {
		t.Fatalf("HSTS over https: %q", h)
	}
	plain := do(t, e.client(t), "POST", e.http.URL+"/api/login", map[string]string{"username": "maciej", "password": "correct horse"})
	if c := plain.Header.Get("Set-Cookie"); strings.Contains(c, "Secure") || plain.Header.Get("Strict-Transport-Security") != "" {
		t.Fatalf("plain http on the LAN: cookie %s, HSTS %q", c, plain.Header.Get("Strict-Transport-Security"))
	}
}
