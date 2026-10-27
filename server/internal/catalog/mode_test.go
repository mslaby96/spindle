package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/presence"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newMode(t *testing.T) (*Mode, *testClock, <-chan events.Event) {
	t.Helper()
	s, _ := newService(t)
	c := &testClock{t: time.Unix(1_800_000_000, 0)}
	s.Now = c.now
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(1)
	t.Cleanup(cancel)
	return &Mode{Service: s, Bus: bus, Idle: 10 * time.Minute}, c, ch
}

func drain(ch <-chan events.Event) []events.Event {
	var out []events.Event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func obs(uid string, at time.Time) presence.Observation {
	return presence.Observation{UserID: 1, DeviceID: 1, UID: uid, At: at}
}

func TestNewTagInCatalogMode(t *testing.T) {
	m, c, ch := newMode(t)
	ctx := context.Background()
	m.Set(ctx, 1, true)
	drain(ch)

	led, err := m.Observe(ctx, obs("04A1", c.t))
	if err != nil || led != presence.LEDCatalogReady {
		t.Fatalf("led %s err %v", led, err)
	}
	evs := drain(ch)
	if len(evs) != 2 || evs[1].Type != EventCatalogTag {
		t.Fatalf("events %+v", evs)
	}
	ct := evs[1].Data.(CatalogTag)
	if !ct.New || ct.Tag.UID != "04A1" || ct.Tag.Album != nil {
		t.Fatalf("catalog tag %+v", ct)
	}
	if tags, _ := m.Service.UnassignedTags(ctx, 1); len(tags) != 1 {
		t.Fatalf("unassigned %+v", tags)
	}

	m.Observe(ctx, obs("04A1", c.t))
	if evs := drain(ch); len(evs) != 0 {
		t.Fatalf("same sleeve published again: %+v", evs)
	}
}

func TestKnownTagShowsAssignment(t *testing.T) {
	m, c, ch := newMode(t)
	ctx := context.Background()
	if _, err := m.Service.Assign(ctx, 1, "04A1", Assignment{MasterID: 10362}); err != nil {
		t.Fatal(err)
	}
	m.Set(ctx, 1, true)
	drain(ch)
	led, _ := m.Observe(ctx, obs("04A1", c.t))
	if led != presence.LEDCatalogSaved {
		t.Fatalf("led %s", led)
	}
	evs := drain(ch)
	ct := evs[len(evs)-1].Data.(CatalogTag)
	if ct.New || ct.Tag.Album == nil || ct.Tag.Album.Title != "The Dark Side Of The Moon" {
		t.Fatalf("catalog tag %+v", ct)
	}
	var n int
	m.Service.DB.R.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("catalog mode created a session")
	}
}

func TestModeExpiresWithoutNewSleeves(t *testing.T) {
	m, c, _ := newMode(t)
	ctx := context.Background()
	m.Set(ctx, 1, true)
	c.t = c.t.Add(8 * time.Minute)
	m.Observe(ctx, obs("04A1", c.t))
	c.t = c.t.Add(8 * time.Minute)
	if on, _ := m.Active(ctx, 1); !on {
		t.Fatal("new sleeve should have extended the mode")
	}
	m.Observe(ctx, obs("04A1", c.t))
	c.t = c.t.Add(3 * time.Minute)
	if on, _ := m.Active(ctx, 1); on {
		t.Fatal("same sleeve sitting on the stand kept the mode alive")
	}
}

func TestTurningModeOff(t *testing.T) {
	m, _, ch := newMode(t)
	ctx := context.Background()
	m.Set(ctx, 1, true)
	state, err := m.Set(ctx, 1, false)
	if err != nil || state.Active {
		t.Fatalf("state %+v err %v", state, err)
	}
	evs := drain(ch)
	if last := evs[len(evs)-1]; last.Type != EventCatalogMode || last.Data.(ModeState).Active {
		t.Fatalf("events %+v", evs)
	}
}

func TestReassignKeepsHistory(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	f.masters[777] = f.masters[10362]
	m := f.masters[777]
	m.ID, m.Title = 777, "Wish You Were Here"
	f.masters[777] = m

	tag, err := s.Assign(ctx, 1, "04A1", Assignment{MasterID: 10362})
	if err != nil {
		t.Fatal(err)
	}
	s.DB.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 'stand', 'h', 1)`)
	s.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at)
		VALUES (1, 1, ?, ?, 'counted', 1, 2)`, tag.ID, tag.Album.ID)
	s.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, state, started_at, last_seen_at)
		VALUES (1, 1, ?, 'unclassified', 3, 4)`, tag.ID)

	re, err := s.Assign(ctx, 1, "04A1", Assignment{MasterID: 777})
	if err != nil {
		t.Fatal(err)
	}
	var old, moved int
	s.DB.R.QueryRow(`SELECT count(*) FROM sessions WHERE album_id = ?`, tag.Album.ID).Scan(&old)
	s.DB.R.QueryRow(`SELECT count(*) FROM sessions WHERE album_id = ?`, re.Album.ID).Scan(&moved)
	if old != 1 || moved != 1 {
		t.Fatalf("old album %d sessions, new album %d", old, moved)
	}
}

func TestAssignStandaloneRelease(t *testing.T) {
	s, _ := newService(t)
	tag, err := s.Assign(context.Background(), 1, "04B2", Assignment{ReleaseID: 4455667})
	if err != nil {
		t.Fatal(err)
	}
	if tag.Album == nil || tag.Release == nil || tag.Release.AlbumID != tag.Album.ID || tag.Album.MasterID != nil {
		t.Fatalf("tag %+v", tag)
	}
}

func TestAssignNeedsSomething(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Assign(context.Background(), 1, "04B2", Assignment{}); !errors.Is(err, ErrNothingToAssign) {
		t.Fatalf("got %v", err)
	}
}
