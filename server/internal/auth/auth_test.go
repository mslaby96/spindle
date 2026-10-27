package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newService(t *testing.T) (*Service, *clock) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s := New(d)
	s.Now = c.now
	return s, c
}

func setupUser(t *testing.T, s *Service) int64 {
	t.Helper()
	ctx := context.Background()
	code, err := s.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CompleteSetup(ctx, code, "maciej", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if h == "correct horse" {
		t.Fatal("hash equals password")
	}
	if ok, _ := VerifyPassword(h, "correct horse"); !ok {
		t.Error("valid password rejected")
	}
	if ok, _ := VerifyPassword(h, "wrong horse"); ok {
		t.Error("wrong password accepted")
	}
	if _, err := HashPassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("short password: %v", err)
	}
}

func TestSetupWithCode(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	setupUser(t, s)
	if needed, _ := s.SetupNeeded(ctx); needed {
		t.Error("setup still needed after account creation")
	}
	if _, err := s.CompleteSetup(ctx, "ANY", "other", "password123"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second setup: %v", err)
	}
	if code, _ := s.PrepareSetup(ctx); code != "" {
		t.Error("setup code issued after account exists")
	}
}

func TestSetupRejectsMissingOrWrongCode(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	for _, c := range []string{"", "WRONGCODE1", code + "X"} {
		if _, err := s.CompleteSetup(ctx, c, "maciej", "correct horse"); !errors.Is(err, ErrInvalidSetupCode) {
			t.Errorf("code %q: %v", c, err)
		}
	}
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); err != nil {
		t.Errorf("valid code after failures: %v", err)
	}
}

func TestConcurrentSetupCreatesOneUser(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var n int
	s.db.R.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	if ok != 1 || n != 1 {
		t.Fatalf("successes=%d users=%d", ok, n)
	}
}

func TestAuthenticate(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	got, err := s.Authenticate(ctx, "maciej", "correct horse")
	if err != nil || got != id {
		t.Fatalf("got %d, %v", got, err)
	}
	if _, err := s.Authenticate(ctx, "maciej", "nope nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := s.Authenticate(ctx, "nobody", "correct horse"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestSessionWithoutRememberExpiresAfterIdle(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, err := s.CreateSession(ctx, id, false)
	if err != nil {
		t.Fatal(err)
	}
	c.advance(6 * 24 * time.Hour)
	if _, err := s.Lookup(ctx, token); err != nil {
		t.Fatalf("after 6 days: %v", err)
	}
	c.advance(SessionIdle + time.Second)
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after 7 idle days: %v", err)
	}
}

func TestRememberedSessionSlides(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, _ := s.CreateSession(ctx, id, true)
	for i := 0; i < 3; i++ {
		c.advance(300 * 24 * time.Hour)
		if _, err := s.Lookup(ctx, token); err != nil {
			t.Fatalf("use %d after 300 days: %v", i, err)
		}
	}
	c.advance(RememberIdle + time.Second)
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after a year idle: %v", err)
	}
}

func TestLogout(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, _ := s.CreateSession(ctx, id, true)
	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestChangePasswordInvalidatesOtherSessions(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	current, _, _ := s.CreateSession(ctx, id, false)
	other, _, _ := s.CreateSession(ctx, id, true)
	if err := s.ChangePassword(ctx, id, "wrong horse", "new password", current); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := s.ChangePassword(ctx, id, "correct horse", "new password", current); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, current); err != nil {
		t.Errorf("current session dropped: %v", err)
	}
	if _, err := s.Lookup(ctx, other); !errors.Is(err, ErrNoSession) {
		t.Errorf("other session kept: %v", err)
	}
	if _, err := s.Authenticate(ctx, "maciej", "new password"); err != nil {
		t.Errorf("new password: %v", err)
	}
}

func TestResetPasswordDropsAllSessions(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, _ := s.CreateSession(ctx, id, true)
	if err := s.ResetPassword(ctx, "maciej", "reset password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("session survived reset: %v", err)
	}
	if err := s.ResetPassword(ctx, "nobody", "reset password"); err == nil {
		t.Error("reset of unknown user succeeded")
	}
}

func TestLimiterBacksOff(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i < freeAttempts; i++ {
		l.Fail("1.2.3.4")
	}
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Fatal("blocked within free attempts")
	}
	l.Fail("1.2.3.4")
	ok, wait := l.Allow("1.2.3.4")
	if ok || wait != time.Second {
		t.Fatalf("after 4th failure: ok=%v wait=%v", ok, wait)
	}
	l.Fail("1.2.3.4")
	if _, wait := l.Allow("1.2.3.4"); wait != 2*time.Second {
		t.Fatalf("after 5th failure: wait=%v", wait)
	}
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Error("other client blocked")
	}
	c.advance(2 * time.Second)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Error("still blocked after backoff")
	}
}
