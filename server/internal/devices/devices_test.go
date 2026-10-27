package devices

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
