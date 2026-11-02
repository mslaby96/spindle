package auth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	for day := 20; day <= 160; day += 20 {
		c.advance(20 * 24 * time.Hour)
		if _, err := s.Lookup(ctx, token); err != nil {
			t.Fatalf("used every 20 days, day %d: %v", day, err)
		}
	}
	other, _, _ := s.CreateSession(ctx, id, true)
	c.advance(RememberIdle + time.Second)
	if _, err := s.Lookup(ctx, other); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after a month unused: %v", err)
	}
}

func TestEvenAUsedSessionEndsAtItsMaximumAge(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, sess, _ := s.CreateSession(ctx, id, true)
	if !sess.ExpiresAt.Equal(c.t.Add(RememberIdle)) {
		t.Fatalf("a remembered session first expires at %v", sess.ExpiresAt)
	}
	for used := time.Duration(0); used < MaxSessionAge-24*time.Hour; used += 24 * time.Hour {
		c.advance(24 * time.Hour)
		got, err := s.Lookup(ctx, token)
		if err != nil {
			t.Fatalf("used daily, day %d: %v", used/(24*time.Hour), err)
		}
		if got.ExpiresAt.After(c.t.Add(MaxSessionAge)) {
			t.Fatalf("slid past its maximum age: %v", got.ExpiresAt)
		}
	}
	c.advance(48 * time.Hour)
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after its maximum age: %v", err)
	}
}

func TestPurgeDropsExpiredSessions(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	s.CreateSession(ctx, id, false)
	c.advance(SessionIdle + time.Second)
	live, _, _ := s.CreateSession(ctx, id, false)
	if err := s.Purge(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.R.QueryRow(`SELECT count(*) FROM login_sessions`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d sessions left", n)
	}
	if _, err := s.Lookup(ctx, live); err != nil {
		t.Fatalf("live session purged: %v", err)
	}
}

func TestSetupCodeExpires(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	c.advance(SetupCodeLife)
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); !errors.Is(err, ErrSetupCodeExpired) {
		t.Fatalf("old code: %v", err)
	}
	code, _ = s.PrepareSetup(ctx)
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); err != nil {
		t.Fatalf("a new code: %v", err)
	}
}

func TestBusyHashingGivesUpInsteadOfQueueingForEver(t *testing.T) {
	old := hashWait
	hashWait = 20 * time.Millisecond
	defer func() { hashWait = old }()
	for i := 0; i < cap(hashing); i++ {
		hashing <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(hashing); i++ {
			<-hashing
		}
	}()
	if _, err := VerifyPassword(dummyHash, "x"); !errors.Is(err, ErrBusy) {
		t.Fatalf("verify while busy: %v", err)
	}
	if _, err := HashPassword("correct horse"); !errors.Is(err, ErrBusy) {
		t.Fatalf("hash while busy: %v", err)
	}
}

func TestResetPasswordTrimsTheUsername(t *testing.T) {
	s, _ := newService(t)
	setupUser(t, s)
	if err := s.ResetPassword(context.Background(), " maciej ", "new password"); err != nil {
		t.Fatal(err)
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
		if ok, _ := l.Begin("1.2.3.4"); !ok {
			t.Fatalf("attempt %d blocked within free attempts", i+1)
		}
	}
	if ok, _ := l.Begin("1.2.3.4"); !ok {
		t.Fatal("4th attempt should run and start the backoff")
	}
	ok, wait := l.Begin("1.2.3.4")
	if ok || wait != time.Second {
		t.Fatalf("right after the 4th attempt: ok=%v wait=%v", ok, wait)
	}
	c.advance(time.Second)
	l.Begin("1.2.3.4")
	if _, wait := l.Begin("1.2.3.4"); wait != 2*time.Second {
		t.Fatalf("after the 5th attempt: wait=%v", wait)
	}
	if ok, _ := l.Begin("5.6.7.8"); !ok {
		t.Error("other client blocked")
	}
	l.Reset("1.2.3.4")
	if ok, _ := l.Begin("1.2.3.4"); !ok {
		t.Error("still blocked after a successful login")
	}
}

func TestLimiterHoldsUnderConcurrency(t *testing.T) {
	l := NewLimiter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Begin("1.2.3.4"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != freeAttempts+1 {
		t.Fatalf("%d of 50 parallel attempts got through", allowed)
	}
}

func TestPasswordOfMinimumLengthAccepted(t *testing.T) {
	pw := "12345678"
	if len(pw) != MinPassword {
		t.Fatalf("fixture has %d characters", len(pw))
	}
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("password of exactly %d characters: %v", MinPassword, err)
	}
	if ok, _ := VerifyPassword(h, pw); !ok {
		t.Error("valid password rejected")
	}
}

func TestPasswordHashRecordsArgonParameters(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash %q", h)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=x,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!",
	} {
		if ok, err := VerifyPassword(h, "correct horse"); ok || err == nil {
			t.Errorf("hash %q: ok=%v err=%v", h, ok, err)
		}
	}
}

func TestSetupRequiresUsernameAndStrongPassword(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	if _, err := s.CompleteSetup(ctx, code, "  ", "correct horse"); !errors.Is(err, ErrNoUsername) {
		t.Errorf("blank username: %v", err)
	}
	if _, err := s.CompleteSetup(ctx, code, "maciej", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password: %v", err)
	}
	if needed, _ := s.SetupNeeded(ctx); !needed {
		t.Fatal("account created by a rejected setup")
	}
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); err != nil {
		t.Errorf("valid setup after rejections: %v", err)
	}
}

func TestSetupFailsWhenAccountAppearedAfterCodeWasIssued(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	if _, err := s.db.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('first', 'h', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("setup with existing account: %v", err)
	}
	var n int
	s.db.R.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("users=%d", n)
	}
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("code still usable: %v", err)
	}
}

func TestSetupWithClosedDatabase(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	code, _ := s.PrepareSetup(ctx)
	s.db.Close()
	if _, err := s.CompleteSetup(ctx, code, "maciej", "correct horse"); err == nil || errors.Is(err, ErrSetupDone) {
		t.Fatalf("setup on closed database: %v", err)
	}
}

func TestAuthenticateWithClosedDatabase(t *testing.T) {
	s, _ := newService(t)
	setupUser(t, s)
	s.db.Close()
	_, err := s.Authenticate(context.Background(), "maciej", "correct horse")
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v", err)
	}
}

func TestAuthenticateWithCorruptStoredHash(t *testing.T) {
	s, _ := newService(t)
	setupUser(t, s)
	s.db.W.Exec(`UPDATE users SET password_hash = 'garbage'`)
	_, err := s.Authenticate(context.Background(), "maciej", "correct horse")
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionExpiresAtExactIdleDeadline(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, _ := s.CreateSession(ctx, id, false)
	c.advance(SessionIdle)
	if _, err := s.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("at the idle deadline: %v", err)
	}
}

func TestLookupKeepsExpiryWithinTouchGranularity(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, created, _ := s.CreateSession(ctx, id, false)
	c.advance(touchGranularity - time.Second)
	sess, err := s.Lookup(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != id || !sess.ExpiresAt.Equal(created.ExpiresAt) {
		t.Fatalf("got %+v, created %+v", sess, created)
	}
}

func TestLookupSlidesExpiryAfterTouchGranularity(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	id := setupUser(t, s)
	token, _, _ := s.CreateSession(ctx, id, false)
	c.advance(touchGranularity)
	sess, err := s.Lookup(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if want := c.now().Add(SessionIdle); sess.UserID != id || !sess.ExpiresAt.Equal(want) {
		t.Fatalf("got %+v, want expiry %v", sess, want)
	}
	c.advance(SessionIdle - time.Second)
	if _, err := s.Lookup(ctx, token); err != nil {
		t.Fatalf("slid expiry not stored: %v", err)
	}
}

func TestLimiterBlocksUntilBackoffFullyElapses(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i <= freeAttempts; i++ {
		l.Begin("1.2.3.4")
	}
	c.advance(time.Second - time.Nanosecond)
	if ok, wait := l.Begin("1.2.3.4"); ok || wait != time.Nanosecond {
		t.Fatalf("1ns before the backoff ends: ok=%v wait=%v", ok, wait)
	}
	c.advance(time.Nanosecond)
	if ok, _ := l.Begin("1.2.3.4"); !ok {
		t.Fatal("blocked after the backoff ended")
	}
}

func TestLimiterBackoffIsCapped(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i < 100; i++ {
		if ok, _ := l.Begin("1.2.3.4"); !ok {
			t.Fatalf("attempt %d blocked after waiting out the backoff", i+1)
		}
		if i < freeAttempts {
			continue
		}
		ok, wait := l.Begin("1.2.3.4")
		if ok || wait <= 0 || wait > maxBackoff {
			t.Fatalf("after attempt %d: ok=%v wait=%v", i+1, ok, wait)
		}
		if i >= freeAttempts+9 && wait != maxBackoff {
			t.Fatalf("after attempt %d: wait=%v", i+1, wait)
		}
		c.advance(maxBackoff)
	}
}

func TestLimiterForgetsOldFailures(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i < freeAttempts; i++ {
		l.Begin("1.2.3.4")
	}
	c.advance(forgetAfter)
	for i := 0; i <= freeAttempts; i++ {
		if ok, _ := l.Begin("1.2.3.4"); !ok {
			t.Fatalf("attempt %d after a quiet spell was blocked", i+1)
		}
	}
	if ok, _ := l.Begin("1.2.3.4"); ok {
		t.Fatal("fresh failures stopped counting")
	}
}

func TestLimiterDropsClientsItNoLongerNeeds(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i < 1000; i++ {
		l.Begin(fmt.Sprint("client-", i))
	}
	c.advance(forgetAfter + maxBackoff)
	l.Begin("late")
	if n := l.size(); n != 1 {
		t.Fatalf("%d clients kept", n)
	}
}

func TestLimiterWithMoreFreeAttempts(t *testing.T) {
	l := NewLimiter()
	l.Free = 10
	for i := 0; i <= 10; i++ {
		if ok, _ := l.Begin("user:maciej"); !ok {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	if ok, _ := l.Begin("user:maciej"); ok {
		t.Fatal("no backoff after the free attempts")
	}
}

func TestASuccessIsNotCountedButEarlierFailuresStay(t *testing.T) {
	l := NewLimiter()
	c := &clock{t: time.Unix(0, 0)}
	l.Now = c.now
	for i := 0; i < freeAttempts; i++ {
		l.Begin("1.2.3.4")
	}
	for i := 0; i < 5; i++ {
		if ok, _ := l.Begin("1.2.3.4"); !ok {
			t.Fatalf("success %d was blocked", i+1)
		}
		l.Done("1.2.3.4")
	}
	l.Begin("1.2.3.4")
	if ok, _ := l.Begin("1.2.3.4"); ok {
		t.Fatal("the failures before the successes were forgotten")
	}
	l.Done("unknown")
}
