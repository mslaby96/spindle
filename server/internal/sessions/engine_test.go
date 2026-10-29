package sessions

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/presence"
)

const (
	tagA = "04AAAAAAAAAAAA"
	tagB = "04BBBBBBBBBBBB"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type queue struct{ albums []int64 }

func (q *queue) Enqueue(_, albumID int64) { q.albums = append(q.albums, albumID) }

type env struct {
	e      *Engine
	clock  *clock
	tags   *catalog.Service
	queue  *queue
	events <-chan events.Event
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`)
	d.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 'stand', 'h', 1)`)
	cfg, _ := config.Load(func(string) string { return "" })
	c := &clock{t: time.Date(2026, 3, 14, 20, 0, 0, 0, time.UTC)}
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(1)
	t.Cleanup(cancel)
	tags := &catalog.Service{DB: d, Now: c.now}
	q := &queue{}
	return &env{
		e:      &Engine{DB: d, Tags: tags, Bus: bus, Cfg: cfg.Sessions, Now: c.now, Durations: q},
		clock:  c,
		tags:   tags,
		queue:  q,
		events: ch,
	}
}

// album creates an album with the given length (nil for unknown) and assigns tag to it.
func (v *env) album(t *testing.T, uid string, length *time.Duration) int64 {
	t.Helper()
	status, secs := "no_data", any(nil)
	if length != nil {
		status, secs = "resolved", int64(*length/time.Second)
	}
	res, err := v.e.DB.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, duration_sec, duration_status, created_at)
		VALUES (1, ?, 'Album', 'Artist', ?, ?, 1)`, time.Now().UnixNano(), secs, status)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tag, _, err := v.tags.EnsureTag(context.Background(), 1, uid)
	if err != nil {
		t.Fatal(err)
	}
	v.e.DB.W.Exec(`UPDATE tags SET album_id = ? WHERE id = ?`, id, tag.ID)
	return id
}

func minutes(n int) *time.Duration {
	d := time.Duration(n) * time.Minute
	return &d
}

func (v *env) see(t *testing.T, uid string) presence.LED {
	t.Helper()
	led, err := v.e.Observe(context.Background(), presence.Observation{UserID: 1, DeviceID: 1, UID: uid, At: v.clock.now()})
	if err != nil {
		t.Fatal(err)
	}
	return led
}

// play keeps the sleeve on the stand for d, reporting every 1.5 s like the firmware does.
func (v *env) play(t *testing.T, uid string, d time.Duration) {
	t.Helper()
	for elapsed := time.Duration(0); elapsed < d; elapsed += 30 * time.Second {
		v.see(t, uid)
		v.clock.advance(30 * time.Second)
	}
	v.see(t, uid)
}

// remove takes the sleeve away and lets the sweeper run after the tolerance.
func (v *env) remove(t *testing.T) {
	t.Helper()
	v.see(t, "")
	v.clock.advance(v.e.Cfg.AbsenceTolerance + 10*time.Second)
	if err := v.e.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (v *env) sessions(t *testing.T) []Session {
	t.Helper()
	list, err := v.e.List(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (v *env) only(t *testing.T) Session {
	t.Helper()
	list := v.sessions(t)
	if len(list) != 1 {
		t.Fatalf("expected one session, got %+v", list)
	}
	return list[0]
}

func TestShortBreakKeepsOneSession(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 10*time.Minute)
	v.see(t, "")
	v.clock.advance(60 * time.Second)
	v.e.Sweep(context.Background())
	v.play(t, tagA, 20*time.Minute)
	v.remove(t)

	s := v.only(t)
	if s.State != StateCounted || s.PlayCount != 1 {
		t.Fatalf("session %+v", s)
	}
	if got := time.Duration(s.ListenedSec) * time.Second; got < 31*time.Minute || got > 32*time.Minute {
		t.Fatalf("listened %v", got)
	}
}

func TestShortSessionIsDiscarded(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 2*time.Minute)
	v.remove(t)
	if s := v.only(t); s.State != StateDiscarded || s.PlayCount != 0 || s.ListenedSec != 0 {
		t.Fatalf("session %+v", s)
	}
}

func TestMinimumIsInclusive(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.see(t, tagA)
	v.clock.advance(v.e.Cfg.MinDuration)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StateCounted {
		t.Fatalf("session exactly at the minimum: %+v", s)
	}
}

func TestSwappingSleeves(t *testing.T) {
	v := newEnv(t)
	a := v.album(t, tagA, minutes(42))
	b := v.album(t, tagB, minutes(40))
	v.play(t, tagA, 20*time.Minute)
	lastA := v.clock.now()
	v.clock.advance(2 * time.Second)
	if led := v.see(t, tagB); led != presence.LEDKnown {
		t.Fatalf("led %s", led)
	}

	list := v.sessions(t)
	if len(list) != 2 {
		t.Fatalf("sessions %+v", list)
	}
	first, second := list[1], list[0]
	if *first.AlbumID != a || first.State != StateCounted || first.EndedAt == nil || !first.EndedAt.Equal(lastA) {
		t.Fatalf("first %+v want end %v", first, lastA)
	}
	if *second.AlbumID != b || second.State != StateActive {
		t.Fatalf("second %+v", second)
	}
}

func TestLongSessionNeedsConfirmation(t *testing.T) {
	cases := []struct {
		resolution Resolution
		plays      int
		listened   time.Duration
	}{
		{ResolutionForgot, 1, 42 * time.Minute},
		{ResolutionRepeated, 4, 3 * time.Hour},
		{ResolutionLonger, 1, 3 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(string(tc.resolution), func(t *testing.T) {
			v := newEnv(t)
			v.album(t, tagA, minutes(42))
			v.see(t, tagA)
			v.clock.advance(3 * time.Hour)
			v.see(t, tagA)
			v.remove(t)

			s := v.only(t)
			if s.State != StatePendingConfirmation || s.PlayCount != 1 || s.DurationConfirmed {
				t.Fatalf("before answer %+v", s)
			}
			if err := v.e.Resolve(context.Background(), 1, s.ID, tc.resolution); err != nil {
				t.Fatal(err)
			}
			s = v.only(t)
			if s.State != StateCounted || s.PlayCount != tc.plays || time.Duration(s.ListenedSec)*time.Second != tc.listened ||
				!s.DurationConfirmed || s.Resolution != tc.resolution {
				t.Fatalf("after %s: %+v", tc.resolution, s)
			}
		})
	}
}

func TestUnansweredConfirmationResolvesAfterAWeek(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.see(t, tagA)
	v.clock.advance(3 * time.Hour)
	v.see(t, tagA)
	v.remove(t)

	v.clock.advance(6 * 24 * time.Hour)
	v.e.Sweep(context.Background())
	if s := v.only(t); s.State != StatePendingConfirmation {
		t.Fatalf("after 6 days %+v", s)
	}
	v.clock.advance(25 * time.Hour)
	v.e.Sweep(context.Background())
	s := v.only(t)
	if s.State != StateCounted || s.PlayCount != 1 || s.ListenedSec != 42*60 || !s.DurationConfirmed || s.Resolution != ResolutionAuto {
		t.Fatalf("after 7 days %+v", s)
	}
}

func TestUnknownLengthUsesFallbackThreshold(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, nil)
	v.see(t, tagA)
	v.clock.advance(100 * time.Minute)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StatePendingConfirmation {
		t.Fatalf("session %+v", s)
	}

	v = newEnv(t)
	v.album(t, tagA, nil)
	v.see(t, tagA)
	v.clock.advance(80 * time.Minute)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StateCounted {
		t.Fatalf("80 min is within the fallback: %+v", s)
	}
}

func TestExactlyTheLongThresholdCounts(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(40))
	v.see(t, tagA)
	v.clock.advance(52 * time.Minute)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StateCounted {
		t.Fatalf("1.3 x 40 min exactly: %+v", s)
	}
}

func TestResolvingUpdatesTheInbox(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.see(t, tagA)
	v.clock.advance(3 * time.Hour)
	v.see(t, tagA)
	v.remove(t)
	drainEvents(v.events)
	if err := v.e.Resolve(context.Background(), 1, v.only(t).ID, ResolutionForgot); err != nil {
		t.Fatal(err)
	}
	if evs := drainEvents(v.events); len(evs) != 1 || evs[0].Type != EventInboxChanged {
		t.Fatalf("events %+v", evs)
	}
}

func TestClassifyAlbumClassifiesEveryWaitingListen(t *testing.T) {
	v := newEnv(t)
	albumID := v.album(t, tagA, nil)
	v.e.DB.W.Exec(`UPDATE albums SET duration_status = 'pending' WHERE id = ?`, albumID)
	for range 2 {
		v.play(t, tagA, 20*time.Minute)
		v.remove(t)
	}
	if err := v.e.ClassifyAlbum(context.Background(), 1, albumID); err != nil {
		t.Fatal(err)
	}
	for _, s := range v.sessions(t) {
		if s.State != StateUnclassified {
			t.Fatalf("classified while the length is still being looked up: %+v", s)
		}
	}

	v.e.DB.W.Exec(`UPDATE albums SET duration_status = 'resolved', duration_sec = 2520 WHERE id = ?`, albumID)
	if err := v.e.ClassifyAlbum(context.Background(), 1, albumID); err != nil {
		t.Fatal(err)
	}
	list := v.sessions(t)
	if len(list) != 2 || list[0].State != StateCounted || list[1].State != StateCounted {
		t.Fatalf("sessions %+v", list)
	}
}

func TestUnassignedTagIsCountedOnceAssigned(t *testing.T) {
	v := newEnv(t)
	if led := v.see(t, tagA); led != presence.LEDUnknown {
		t.Fatalf("led %s", led)
	}
	v.clock.advance(40 * time.Minute)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StateUnclassified || s.AlbumID != nil {
		t.Fatalf("before assignment %+v", s)
	}

	v.clock.advance(24 * time.Hour)
	albumID := v.album(t, tagA, minutes(42))
	v.e.DB.W.Exec(`UPDATE sessions SET album_id = ?`, albumID)
	v.e.ClassifyAlbum(context.Background(), 1, albumID)
	s := v.only(t)
	if s.State != StateCounted || s.ListenedSec != 40*60 || *s.AlbumID != albumID {
		t.Fatalf("after assignment %+v", s)
	}
}

func TestClassificationWaitsForAlbumLength(t *testing.T) {
	v := newEnv(t)
	albumID := v.album(t, tagA, nil)
	v.e.DB.W.Exec(`UPDATE albums SET duration_status = 'pending' WHERE id = ?`, albumID)
	v.see(t, tagA)
	v.clock.advance(60 * time.Minute)
	v.see(t, tagA)
	v.remove(t)
	if s := v.only(t); s.State != StateUnclassified {
		t.Fatalf("classified before length was known %+v", s)
	}
	if len(v.queue.albums) == 0 {
		t.Fatal("album length lookup was not requested")
	}

	v.e.DB.W.Exec(`UPDATE albums SET duration_status = 'resolved', duration_sec = 2520 WHERE id = ?`, albumID)
	v.e.ClassifyAlbum(context.Background(), 1, albumID)
	if s := v.only(t); s.State != StatePendingConfirmation {
		t.Fatalf("60 min of a 42 min album should need confirmation: %+v", s)
	}
}

func TestRepeatedRoundsAndHasMinimum(t *testing.T) {
	if got := repeatedPlays(50*time.Minute, 42*time.Minute); got != 1 {
		t.Errorf("50 of 42 min: %d", got)
	}
	if got := repeatedPlays(100*time.Minute, 42*time.Minute); got != 2 {
		t.Errorf("100 of 42 min: %d", got)
	}
	if got := repeatedPlays(20*time.Minute, 42*time.Minute); got != 1 {
		t.Errorf("20 of 42 min: %d", got)
	}
}

func TestRestartClosesStaleSession(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 30*time.Minute)

	restarted := &Engine{DB: v.e.DB, Tags: v.tags, Bus: v.e.Bus, Cfg: v.e.Cfg, Now: v.clock.now, Durations: v.queue}
	v.clock.advance(5 * time.Minute)
	restarted.Sweep(context.Background())
	if s := v.only(t); s.State != StateCounted {
		t.Fatalf("session after restart %+v", s)
	}
}

func TestConcurrentReportAndSweepMakeOneSession(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 10*time.Minute)
	v.clock.advance(v.e.Cfg.AbsenceTolerance + time.Second)

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	obs := presence.Observation{UserID: 1, DeviceID: 1, UID: tagA, At: v.clock.now()}
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := v.e.Observe(context.Background(), obs)
			errs <- err
		}()
		go func() { defer wg.Done(); errs <- v.e.Sweep(context.Background()) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var active int
	v.e.DB.R.QueryRow(`SELECT count(*) FROM sessions WHERE state = 'active'`).Scan(&active)
	if active > 1 {
		t.Fatalf("active sessions %d: %+v", active, v.sessions(t))
	}
}

func TestFailedLengthLookupFallsBackAfterADay(t *testing.T) {
	cases := []struct {
		minutes int
		want    State
	}{{40, StateCounted}, {100, StatePendingConfirmation}}
	for _, tc := range cases {
		v := newEnv(t)
		albumID := v.album(t, tagA, nil)
		v.e.DB.W.Exec(`UPDATE albums SET duration_status = 'failed' WHERE id = ?`, albumID)
		v.see(t, tagA)
		v.clock.advance(time.Duration(tc.minutes) * time.Minute)
		v.see(t, tagA)
		v.remove(t)

		v.clock.advance(23 * time.Hour)
		v.e.Sweep(context.Background())
		if s := v.only(t); s.State != StateUnclassified {
			t.Fatalf("%d min after 23 h: %+v", tc.minutes, s)
		}
		v.clock.advance(2 * time.Hour)
		v.e.Sweep(context.Background())
		if s := v.only(t); s.State != tc.want {
			t.Fatalf("%d min after 25 h: %+v", tc.minutes, s)
		}
	}
}

func TestResolvingWithUnknownAlbumLength(t *testing.T) {
	cases := []struct {
		resolution Resolution
		plays      int
		listened   time.Duration
	}{
		{ResolutionForgot, 1, typicalLP},
		{ResolutionRepeated, 2, 100 * time.Minute},
		{ResolutionLonger, 1, 100 * time.Minute},
	}
	for _, tc := range cases {
		v := newEnv(t)
		v.album(t, tagA, nil)
		v.see(t, tagA)
		v.clock.advance(100 * time.Minute)
		v.see(t, tagA)
		v.remove(t)
		if err := v.e.Resolve(context.Background(), 1, v.only(t).ID, tc.resolution); err != nil {
			t.Fatal(err)
		}
		s := v.only(t)
		if s.PlayCount != tc.plays || time.Duration(s.ListenedSec)*time.Second != tc.listened {
			t.Fatalf("%s: %+v", tc.resolution, s)
		}
	}
}

func TestRepeatedRoundsToNearest(t *testing.T) {
	if got := repeatedPlays(63*time.Minute, 42*time.Minute); got != 2 {
		t.Fatalf("63 of 42 min: %d", got)
	}
}

func TestEditKeepsTheResolution(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.see(t, tagA)
	v.clock.advance(3 * time.Hour)
	v.see(t, tagA)
	v.remove(t)
	s := v.only(t)
	v.e.Resolve(context.Background(), 1, s.ID, ResolutionForgot)

	if err := v.e.Edit(context.Background(), 1, s.ID, s.StartedAt, s.StartedAt.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s = v.only(t); s.ListenedSec != 42*60 || s.PlayCount != 1 {
		t.Fatalf("forgot listen after edit %+v", s)
	}

	v.e.DB.W.Exec(`UPDATE sessions SET resolution = 'repeated'`)
	v.e.Edit(context.Background(), 1, s.ID, s.StartedAt, s.StartedAt.Add(126*time.Minute))
	if s = v.only(t); s.PlayCount != 3 || s.ListenedSec != 126*60 {
		t.Fatalf("repeated listen after edit %+v", s)
	}
}

func TestUnknownResolutionIsRejected(t *testing.T) {
	v := newEnv(t)
	if err := v.e.Resolve(context.Background(), 1, 1, ResolutionAuto); !errors.Is(err, ErrUnknownResolution) {
		t.Fatalf("auto from a client: %v", err)
	}
}

func TestPendingCountsAsOneListen(t *testing.T) {
	v := newEnv(t)
	albumID := v.album(t, tagA, minutes(42))
	v.see(t, tagA)
	v.clock.advance(3 * time.Hour)
	v.see(t, tagA)
	v.remove(t)
	var plays int
	v.e.DB.R.QueryRow(`SELECT coalesce(sum(play_count), 0) FROM sessions WHERE album_id = ? AND state IN ('counted', 'pending_confirmation')`,
		albumID).Scan(&plays)
	if plays != 1 {
		t.Fatalf("plays %d", plays)
	}
}

func TestResolveOnlyPending(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 30*time.Minute)
	v.remove(t)
	s := v.only(t)
	if err := v.e.Resolve(context.Background(), 1, s.ID, ResolutionRepeated); !errors.Is(err, ErrNotPending) {
		t.Fatalf("got %v", err)
	}
	if err := v.e.Resolve(context.Background(), 2, s.ID, ResolutionForgot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user: %v", err)
	}
}

func TestNowPlayingEvents(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	start := v.clock.now()
	v.see(t, tagA)
	first := (<-v.events).Data.(NowPlaying)
	if !first.Active || first.UID != tagA || first.Album == nil || first.StartedAt == nil || !first.StartedAt.Equal(start) {
		t.Fatalf("listen started %+v", first)
	}
	v.play(t, tagA, 10*time.Minute)
	v.remove(t)

	var types []string
	for {
		select {
		case e := <-v.events:
			types = append(types, e.Type)
			continue
		default:
		}
		break
	}
	want := []string{EventNowPlaying, EventNowPlaying, EventInboxChanged}
	if len(types) != len(want) {
		t.Fatalf("events %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events %v", types)
		}
	}
}

func TestEditRecomputesListenedTime(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 30*time.Minute)
	v.remove(t)
	s := v.only(t)
	start := s.StartedAt.Add(-10 * time.Minute)
	if err := v.e.Edit(context.Background(), 1, s.ID, start, start.Add(40*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s = v.only(t); s.ListenedSec != 40*60 || !s.StartedAt.Equal(start) || s.PlayCount != 1 {
		t.Fatalf("after edit %+v", s)
	}
	if err := v.e.Edit(context.Background(), 1, s.ID, start, start.Add(-time.Minute)); !errors.Is(err, ErrInvalidTimes) {
		t.Fatalf("end before start: %v", err)
	}
	if err := v.e.Edit(context.Background(), 1, s.ID, start, v.clock.now().Add(time.Hour)); !errors.Is(err, ErrInvalidTimes) {
		t.Fatalf("end in the future: %v", err)
	}
	if err := v.e.Edit(context.Background(), 2, s.ID, start, start.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user: %v", err)
	}
}

func TestDeleteRemovesListen(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 30*time.Minute)
	v.remove(t)
	v.play(t, tagA, 1*time.Minute)
	if err := v.e.Delete(context.Background(), 1, v.sessions(t)[1].ID); err != nil {
		t.Fatal(err)
	}
	list := v.sessions(t)
	if len(list) != 1 || list[0].State != StateActive {
		t.Fatalf("after delete %+v", list)
	}
	if err := v.e.Delete(context.Background(), 1, list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting the active session: %v", err)
	}
}

func (e *Engine) List(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := e.DB.R.QueryContext(ctx,
		`SELECT `+Columns("sessions")+` FROM sessions WHERE user_id = ? ORDER BY started_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		s, err := ScanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func drainEvents(ch <-chan events.Event) []events.Event {
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

func TestRemovingSleeveAnnouncesWhenListenEnds(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	start := v.clock.now()
	v.play(t, tagA, 10*time.Minute)
	drainEvents(v.events)

	v.see(t, "")
	removedAt := v.clock.now()
	v.see(t, "")
	evs := drainEvents(v.events)
	if len(evs) != 1 || evs[0].Type != EventNowPlaying {
		t.Fatalf("events after removal %+v", evs)
	}
	np := evs[0].Data.(NowPlaying)
	want := removedAt.Add(v.e.Cfg.AbsenceTolerance)
	if !np.Active || np.Album == nil || np.EndsAt == nil || !np.EndsAt.Equal(want) || !np.StartedAt.Equal(start) {
		t.Fatalf("absent event %+v, ends at %v want %v", np, np.EndsAt, want)
	}

	v.clock.advance(20 * time.Second)
	v.see(t, tagA)
	evs = drainEvents(v.events)
	if len(evs) != 1 || evs[0].Data.(NowPlaying).EndsAt != nil || !evs[0].Data.(NowPlaying).Active ||
		!evs[0].Data.(NowPlaying).StartedAt.Equal(start) {
		t.Fatalf("events after putting it back %+v", evs)
	}
	v.see(t, tagA)
	if evs := drainEvents(v.events); len(evs) != 0 {
		t.Fatalf("sleeve still on the stand published again: %+v", evs)
	}
}

func TestEmptyStandWithoutListenIsQuiet(t *testing.T) {
	v := newEnv(t)
	v.see(t, "")
	if evs := drainEvents(v.events); len(evs) != 0 {
		t.Fatalf("events %+v", evs)
	}
}

func TestCurrentReportsSleeveOffTheStand(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	start := v.clock.now()
	v.play(t, tagA, 5*time.Minute)
	albums := func(ctx context.Context, user, id int64) (catalog.Album, error) { return v.tags.Album(ctx, user, id) }

	current, err := v.e.Current(context.Background(), 1, albums)
	if err != nil || len(current) != 1 || current[0].EndsAt != nil || !current[0].Active || !current[0].StartedAt.Equal(start) {
		t.Fatalf("while playing %+v %v", current, err)
	}
	lastSeen := v.clock.now()
	v.clock.advance(staleReport)
	if current, _ = v.e.Current(context.Background(), 1, albums); current[0].EndsAt != nil {
		t.Fatalf("a report %v ago is not stale yet: %+v", staleReport, current)
	}
	v.see(t, "")
	v.clock.advance(15 * time.Second)
	current, _ = v.e.Current(context.Background(), 1, albums)
	if len(current) != 1 || current[0].EndsAt == nil || !current[0].EndsAt.Equal(lastSeen.Add(v.e.Cfg.AbsenceTolerance)) {
		t.Fatalf("after removal %+v", current)
	}
}

func TestSweeperEndsAbandonedListen(t *testing.T) {
	v := newEnv(t)
	v.album(t, tagA, minutes(42))
	v.play(t, tagA, 20*time.Minute)
	v.see(t, "")
	v.clock.advance(v.e.Cfg.AbsenceTolerance + 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		v.e.RunSweeper(ctx, 10*time.Millisecond)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	deadline := time.Now().Add(5 * time.Second)
	for v.only(t).State == StateActive {
		if time.Now().After(deadline) {
			t.Fatal("sweeper never ended the listen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := v.only(t); s.State != StateCounted {
		t.Fatalf("session %+v", s)
	}
}
