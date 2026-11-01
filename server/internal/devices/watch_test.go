package devices

import (
	"context"
	"testing"
	"time"
)

func TestChangesReportsOnlyFlips(t *testing.T) {
	prev := map[int64]bool{1: true, 2: false, 3: true}
	next, changed := changes(prev, []Device{{ID: 1, Online: false}, {ID: 2, Online: false}, {ID: 4, Online: true}})
	if len(changed) != 1 || changed[0].ID != 1 {
		t.Fatalf("changed: %+v", changed)
	}
	if len(next) != 3 || next[1] || next[2] || !next[4] {
		t.Fatalf("next: %v", next)
	}
}

func TestWatcherTellsWhenAStandGoesQuietAndComesBack(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return start }
	dev, _, _ := s.Create(ctx, userA, "Stand")
	s.Touch(ctx, dev.ID, Report{FW: "0.4.0"})
	var got []Device
	w := &Watcher{Store: s, Publish: func(d Device) { got = append(got, d) }}
	if err := w.Check(ctx); err != nil || len(got) != 0 {
		t.Fatalf("first check announces what is already known: %+v %v", got, err)
	}
	s.Now = func() time.Time { return start.Add(OfflineAfter) }
	w.Check(ctx)
	w.Check(ctx)
	if len(got) != 1 || got[0].Online || got[0].UserID != userA {
		t.Fatalf("after silence: %+v", got)
	}
	back, _ := s.Touch(ctx, dev.ID, Report{FW: "0.4.0"})
	w.Seen(back)
	w.Seen(back)
	w.Check(ctx)
	if len(got) != 2 || !got[1].Online {
		t.Fatalf("after the stand reported again: %+v", got)
	}
}

func TestWatcherForgetsRemovedStands(t *testing.T) {
	s, userA, _ := newStore(t)
	ctx := context.Background()
	dev, _, _ := s.Create(ctx, userA, "Stand")
	s.Touch(ctx, dev.ID, Report{})
	var got []Device
	w := &Watcher{Store: s, Publish: func(d Device) { got = append(got, d) }}
	w.Check(ctx)
	s.Revoke(ctx, userA, dev.ID)
	w.Check(ctx)
	if len(got) != 0 || len(w.online) != 0 {
		t.Fatalf("removed stand: published %+v, remembered %v", got, w.online)
	}
}
