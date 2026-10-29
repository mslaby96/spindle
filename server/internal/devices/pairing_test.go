package devices

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPairingCodeRegistersTheStand(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	p, err := s.StartPairing(ctx, userA, "Living room")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Code) != 7 || p.Code[3] != '-' {
		t.Fatalf("code %q", p.Code)
	}
	if want := s.Now().Add(PairingTTL); !p.ExpiresAt.Equal(want.UTC().Truncate(time.Second)) {
		t.Fatalf("expires %v, want %v", p.ExpiresAt, want)
	}

	typed := strings.ToLower(strings.ReplaceAll(p.Code, "-", " "))
	dev, token, err := s.Pair(ctx, typed)
	if err != nil {
		t.Fatal(err)
	}
	if dev.UserID != userA || dev.Name != "Living room" {
		t.Fatalf("device %+v", dev)
	}
	if got, err := s.Authenticate(ctx, token); err != nil || got.ID != dev.ID {
		t.Fatalf("token does not authenticate: %+v %v", got, err)
	}
}

func TestPairingCodeWorksOnce(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	p, _ := s.StartPairing(ctx, userA, "Living room")
	if _, _, err := s.Pair(ctx, p.Code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Pair(ctx, p.Code); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("second use: %v", err)
	}
}

func TestPairingCodeExpires(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 14, 20, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	p, _ := s.StartPairing(ctx, userA, "Living room")

	now = now.Add(PairingTTL)
	if _, _, err := s.Pair(ctx, p.Code); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("code used when it ran out: %v", err)
	}
}

func TestPairingCodeStillWorksJustBeforeItExpires(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 14, 20, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	p, _ := s.StartPairing(ctx, userA, "Living room")

	now = now.Add(PairingTTL - time.Second)
	if _, _, err := s.Pair(ctx, p.Code); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownPairingCode(t *testing.T) {
	s, _, _ := newStore(t)
	for _, code := range []string{"", "ABC-DEF", "not a code at all"} {
		if _, _, err := s.Pair(context.Background(), code); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("%q: %v", code, err)
		}
	}
}

func TestPairingCodesAvoidLookAlikeCharacters(t *testing.T) {
	s, userA, _ := newStore(t)
	seen := map[string]bool{}
	for range 200 {
		p, err := s.StartPairing(context.Background(), userA, "Stand")
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(p.Code, "01OIL") {
			t.Fatalf("code %q has a character that is easy to misread", p.Code)
		}
		seen[p.Code] = true
	}
	if len(seen) < 195 {
		t.Fatalf("only %d distinct codes out of 200", len(seen))
	}
}

func TestPairingNeedsAName(t *testing.T) {
	s, userA, _ := newStore(t)
	if _, err := s.StartPairing(context.Background(), userA, "  "); err == nil {
		t.Fatal("pairing without a stand name")
	}
}
