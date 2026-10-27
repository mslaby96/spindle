package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mslaby96/spindle/server/internal/auth"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/devices"
)

type testEnv struct {
	srv  *Server
	http *httptest.Server
	code string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(func(string) string { return "" })
	a := auth.New(d)
	code, err := a.PrepareSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := New(Deps{
		Config:  cfg,
		DB:      d,
		Auth:    a,
		Devices: devices.NewStore(d),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(s)
	t.Cleanup(func() { hs.Close(); d.Close() })
	return &testEnv{srv: s, http: hs, code: code}
}

func (e *testEnv) client(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func do(t *testing.T, c *http.Client, method, url string, body any, header ...string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *testEnv) loggedIn(t *testing.T) *http.Client {
	t.Helper()
	c := e.client(t)
	resp := do(t, c, "POST", e.http.URL+"/api/setup",
		map[string]string{"code": e.code, "username": "maciej", "password": "correct horse"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
	return c
}

func sessionCookieFrom(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	resp := do(t, e.client(t), "GET", e.http.URL+"/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHealthWithClosedDB(t *testing.T) {
	e := newEnv(t)
	e.srv.DB.Close()
	resp := do(t, e.client(t), "GET", e.http.URL+"/healthz", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSetupFlow(t *testing.T) {
	e := newEnv(t)
	anon := e.client(t)
	var status struct{ Needed bool }
	json.NewDecoder(do(t, anon, "GET", e.http.URL+"/api/setup", nil).Body).Decode(&status)
	if !status.Needed {
		t.Fatal("setup should be needed")
	}
	bad := do(t, anon, "POST", e.http.URL+"/api/setup",
		map[string]string{"code": "WRONG", "username": "x", "password": "correct horse"})
	if bad.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong code: %d", bad.StatusCode)
	}
	c := e.loggedIn(t)
	if resp := do(t, c, "GET", e.http.URL+"/api/me", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("me after setup: %d", resp.StatusCode)
	}
	again := do(t, anon, "POST", e.http.URL+"/api/setup",
		map[string]string{"code": e.code, "username": "y", "password": "correct horse"})
	if again.StatusCode != http.StatusNotFound {
		t.Fatalf("second setup: %d", again.StatusCode)
	}
}

func TestUnauthenticated(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/me", "/api/devices"} {
		if resp := do(t, e.client(t), "GET", e.http.URL+path, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestLoginRememberCookie(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	for _, remember := range []bool{false, true} {
		resp := do(t, e.client(t), "POST", e.http.URL+"/api/login",
			map[string]any{"username": "maciej", "password": "correct horse", "remember": remember})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login remember=%v: %d", remember, resp.StatusCode)
		}
		c := sessionCookieFrom(resp)
		if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Fatalf("bad cookie %+v", c)
		}
		if remember && c.MaxAge <= 0 {
			t.Error("remembered login should set a persistent cookie")
		}
		if !remember && c.MaxAge != 0 {
			t.Error("plain login should set a browser-session cookie")
		}
	}
}

func TestLoginBackoff(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	c := e.client(t)
	last := 0
	for i := 0; i < 5; i++ {
		last = do(t, c, "POST", e.http.URL+"/api/login",
			map[string]string{"username": "maciej", "password": "wrong password"}).StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after repeated failures: %d", last)
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	do(t, c, "POST", e.http.URL+"/api/logout", nil)
	if resp := do(t, c, "GET", e.http.URL+"/api/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", resp.StatusCode)
	}
}

func TestRejectsNonJSON(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("POST", e.http.URL+"/api/login", strings.NewReader("username=a&password=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: %d", resp.StatusCode)
	}
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	other := e.client(t)
	do(t, other, "POST", e.http.URL+"/api/login", map[string]any{"username": "maciej", "password": "correct horse"})

	if resp := do(t, c, "POST", e.http.URL+"/api/account/password",
		map[string]string{"current": "nope nope", "new": "new password"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong current: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", e.http.URL+"/api/account/password",
		map[string]string{"current": "correct horse", "new": "new password"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change: %d", resp.StatusCode)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/me", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("current session after change: %d", resp.StatusCode)
	}
	if resp := do(t, other, "GET", e.http.URL+"/api/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("other session after change: %d", resp.StatusCode)
	}
}

func TestDevicesLifecycle(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	var created struct {
		Device struct{ ID int64 }
		Token  string
	}
	resp := do(t, c, "POST", e.http.URL+"/api/devices", map[string]string{"name": "Stand"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&created)
	if created.Token == "" {
		t.Fatal("no token returned")
	}

	var list []map[string]any
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/devices", nil).Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if _, ok := list[0]["token"]; ok {
		t.Fatal("token exposed in list")
	}

	bearer := do(t, e.client(t), "GET", e.http.URL+"/api/devices", nil, "Authorization", "Bearer "+created.Token)
	if bearer.StatusCode != http.StatusUnauthorized {
		t.Fatalf("device token on app route: %d", bearer.StatusCode)
	}

	url := e.http.URL + "/api/devices/" + jsonNumber(created.Device.ID)
	if resp := do(t, c, "DELETE", url, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	if resp := do(t, c, "DELETE", url, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke twice: %d", resp.StatusCode)
	}
}

func TestDevicesIsolatedBetweenUsers(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	e.srv.DB.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('other', 'h', 1)`)
	dev, _, err := e.srv.Devices.Create(context.Background(), 2, "Other stand")
	if err != nil {
		t.Fatal(err)
	}
	var list []any
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/devices", nil).Body).Decode(&list)
	if len(list) != 0 {
		t.Fatalf("sees other user's devices: %+v", list)
	}
	if resp := do(t, c, "DELETE", e.http.URL+"/api/devices/"+jsonNumber(dev.ID), nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke other user's device: %d", resp.StatusCode)
	}
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
