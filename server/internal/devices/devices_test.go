package devices

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

func newStore(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, u := range []string{"a", "b"} {
		if _, err := d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES (?, 'h', 1)`, u); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(d), 1, 2
}

func TestTokenAuthenticates(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	dev, token, err := s.Create(ctx, userA, "Living room")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, tokenPrefix) || len(token) < 40 {
		t.Fatalf("unexpected token shape %q", token)
	}
	var stored string
	s.db.R.QueryRow(`SELECT token_hash FROM devices WHERE id = ?`, dev.ID).Scan(&stored)
	if stored == token || strings.Contains(stored, token) {
		t.Fatal("token stored in plain text")
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got.ID != dev.ID || got.UserID != userA {
		t.Fatalf("authenticate: %+v %v", got, err)
	}
}

func TestRevokedTokenRejected(t *testing.T) {
	s, userA, userB := newStore(t)
	ctx := context.Background()
	dev, token, _ := s.Create(ctx, userA, "Stand")
	if err := s.Revoke(ctx, userB, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke by other user: %v", err)
	}
	if err := s.Revoke(ctx, userA, dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked token: %v", err)
	}
	if list, _ := s.List(ctx, userA); len(list) != 0 {
		t.Fatalf("revoked device still listed: %+v", list)
	}
}

func TestListScopedToUser(t *testing.T) {
	s, userA, userB := newStore(t)
	ctx := context.Background()
	s.Create(ctx, userA, "A stand")
	s.Create(ctx, userB, "B stand")
	list, err := s.List(ctx, userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "A stand" {
		t.Fatalf("got %+v", list)
	}
}

func TestBadTokens(t *testing.T) {
	s, _, _ := newStore(t)
	for _, tok := range []string{"", "spd_", "random", "spd_doesnotexist"} {
		if _, err := s.Authenticate(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: %v", tok, err)
		}
	}
}

func TestTouchRecordsLastSeenAndFirmware(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	created := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return created }
	dev, token, err := s.Create(ctx, userA, "Stand")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Authenticate(ctx, token); got.LastSeenAt != nil || !got.CreatedAt.Equal(dev.CreatedAt) {
		t.Fatalf("before touch: %+v, created %+v", got, dev)
	}
	seen := created.Add(time.Hour)
	s.Now = func() time.Time { return seen }
	if _, err := s.Touch(ctx, dev.ID, Report{FW: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, userA)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	got := list[0]
	if got.CreatedAt != created || got.LastSeenAt == nil || *got.LastSeenAt != seen || got.FWVersion != "0.2.0" {
		t.Fatalf("after touch: %+v", got)
	}
}

func TestTouchKeepsTheLatestDiagnostics(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	dev, _, _ := s.Create(ctx, userA, "Stand")
	rssi, resets, uptime := -58, 2, int64(3600)
	got, err := s.Touch(ctx, dev.ID, Report{FW: "0.4.0", RSSI: &rssi, ReaderResets: &resets, UptimeSec: &uptime})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Stand" || !got.Online || *got.RSSI != -58 || *got.ReaderResets != 2 || *got.UptimeSec != 3600 {
		t.Fatalf("touched: %+v", got)
	}
	got, _ = s.Touch(ctx, dev.ID, Report{FW: "0.3.0"})
	if got.RSSI != nil || got.ReaderResets != nil || got.UptimeSec != nil {
		t.Fatalf("firmware without diagnostics still shows old ones: %+v", got)
	}
}

func TestStandGoesOfflineAfterNinetySecondsOfSilence(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	seen := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return seen }
	dev, _, _ := s.Create(ctx, userA, "Stand")
	if list, _ := s.List(ctx, userA); list[0].Online {
		t.Fatal("a stand that never reported is online")
	}
	s.Touch(ctx, dev.ID, Report{FW: "0.4.0"})
	for _, c := range []struct {
		after  time.Duration
		online bool
	}{{0, true}, {OfflineAfter - time.Second, true}, {OfflineAfter, false}} {
		s.Now = func() time.Time { return seen.Add(c.after) }
		if list, _ := s.List(ctx, userA); list[0].Online != c.online {
			t.Errorf("%v after the last report: online %v", c.after, list[0].Online)
		}
	}
}

func TestAllListsEveryUsersStands(t *testing.T) {
	s, userA, userB := newStore(t)
	ctx := context.Background()
	s.Create(ctx, userA, "A")
	b, _, _ := s.Create(ctx, userB, "B")
	gone, _, _ := s.Create(ctx, userB, "Gone")
	s.Revoke(ctx, userB, gone.ID)
	all, err := s.All(ctx)
	if err != nil || len(all) != 2 || all[1].ID != b.ID || all[1].UserID != userB {
		t.Fatalf("all: %+v %v", all, err)
	}
}

func TestWiFiChangeIsHandedOverOnce(t *testing.T) {
	s, userA, userB := newStore(t)
	ctx := context.Background()
	dev, _, _ := s.Create(ctx, userA, "Stand")
	if err := s.RequestWiFiChange(ctx, userB, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's stand: %v", err)
	}
	if err := s.RequestWiFiChange(ctx, userA, dev.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, userA); !list[0].WiFiChangePending {
		t.Fatalf("not pending: %+v", list[0])
	}
	if taken, err := s.TakeWiFiChange(ctx, dev.ID); !taken || err != nil {
		t.Fatalf("first take: %v %v", taken, err)
	}
	if taken, _ := s.TakeWiFiChange(ctx, dev.ID); taken {
		t.Fatal("handed over twice")
	}
	if list, _ := s.List(ctx, userA); list[0].WiFiChangePending {
		t.Fatal("still pending after the stand took it")
	}
}

func TestWiFiChangeCanBeCancelled(t *testing.T) {
	s, userA, userB := newStore(t)
	ctx := context.Background()
	dev, _, _ := s.Create(ctx, userA, "Stand")
	s.RequestWiFiChange(ctx, userA, dev.ID)
	if err := s.CancelWiFiChange(ctx, userB, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's stand: %v", err)
	}
	if err := s.CancelWiFiChange(ctx, userA, dev.ID); err != nil {
		t.Fatal(err)
	}
	if taken, _ := s.TakeWiFiChange(ctx, dev.ID); taken {
		t.Fatal("cancelled change handed over")
	}
}
