package duration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/lastfm"
)

type fakeDiscogs struct {
	master   discogs.Master
	release  discogs.Release
	releases map[int64]discogs.Release
	fetched  map[int64]int
	err      error
}

func (f *fakeDiscogs) Master(context.Context, int64) (discogs.Master, error) { return f.master, f.err }
func (f *fakeDiscogs) Release(_ context.Context, id int64) (discogs.Release, error) {
	if f.fetched == nil {
		f.fetched = map[int64]int{}
	}
	f.fetched[id]++
	if rel, ok := f.releases[id]; ok {
		return rel, f.err
	}
	return f.release, f.err
}

type fakeLength struct {
	d        time.Duration
	err      error
	calls    int
	expected int
}

func (f *fakeLength) AlbumLength(_ context.Context, _, _ string, expected int) (time.Duration, error) {
	f.calls++
	f.expected = expected
	return f.d, f.err
}

type fakeLastFM struct {
	d   time.Duration
	err error
}

func (f *fakeLastFM) AlbumLength(context.Context, string, string) (time.Duration, error) {
	return f.d, f.err
}

func tracks(durations ...string) []discogs.Track {
	var ts []discogs.Track
	for _, d := range durations {
		ts = append(ts, discogs.Track{Type: "track", Duration: d})
	}
	return ts
}

type env struct {
	r        *Resolver
	dc       *fakeDiscogs
	mb       *fakeLength
	lf       *fakeLastFM
	resolved []int64
	albumID  int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`)
	res, _ := d.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, cover_status, created_at)
		VALUES (1, 10362, 'The Dark Side Of The Moon', 'Pink Floyd', 'no_data', 1)`)
	albumID, _ := res.LastInsertId()
	d.W.Exec(`INSERT INTO releases (user_id, album_id, discogs_release_id, title, created_at) VALUES (1, ?, 1873013, 'x', 1)`, albumID)

	e := &env{dc: &fakeDiscogs{}, mb: &fakeLength{}, lf: &fakeLastFM{err: lastfm.ErrNoKey}, albumID: albumID}
	e.r = &Resolver{
		DB: d, Discogs: e.dc, MusicBrainz: e.mb, LastFM: e.lf,
		OnResolved: func(_, id int64) { e.resolved = append(e.resolved, id) },
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return e
}

func (e *env) album(t *testing.T) (status string, secs *int64, source *string) {
	t.Helper()
	err := e.r.DB.R.QueryRow(`SELECT duration_status, duration_sec, duration_source FROM albums WHERE id = ?`, e.albumID).
		Scan(&status, &secs, &source)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func (e *env) resolve(t *testing.T) error {
	t.Helper()
	return e.r.Resolve(context.Background(), 1, e.albumID)
}

func TestDiscogsMasterWins(t *testing.T) {
	e := newEnv(t)
	e.dc.master.Tracklist = tracks("3:00", "4:30")
	if err := e.resolve(t); err != nil {
		t.Fatal(err)
	}
	status, secs, src := e.album(t)
	if status != StatusResolved || *secs != 450 || *src != "discogs_master" || e.mb.calls != 0 {
		t.Fatalf("%s %v %v mb=%d", status, *secs, *src, e.mb.calls)
	}
	if len(e.resolved) != 1 {
		t.Fatalf("resolved callbacks %v", e.resolved)
	}
}

func TestFallsBackToRelease(t *testing.T) {
	e := newEnv(t)
	e.dc.master.Tracklist = tracks("3:00", "")
	e.dc.release.Tracklist = tracks("3:00", "1:00")
	e.resolve(t)
	if status, secs, src := e.album(t); status != StatusResolved || *secs != 240 || *src != "discogs_release" {
		t.Fatalf("%s %v %v", status, *secs, *src)
	}
}

func TestFallsBackToMusicBrainzWithTrackCount(t *testing.T) {
	e := newEnv(t)
	e.dc.master.Tracklist = tracks("", "", "")
	e.dc.release.Tracklist = tracks("", "", "")
	e.mb.d = 42*time.Minute + 58*time.Second
	e.resolve(t)
	status, secs, src := e.album(t)
	if status != StatusResolved || *secs != 2578 || *src != "musicbrainz" {
		t.Fatalf("%s %v %v", status, *secs, *src)
	}
	if e.mb.expected != 3 {
		t.Fatalf("expected tracks passed: %d", e.mb.expected)
	}
}

func TestFallsBackToLastFM(t *testing.T) {
	e := newEnv(t)
	e.lf.err, e.lf.d = nil, 40*time.Minute
	e.resolve(t)
	if status, secs, src := e.album(t); status != StatusResolved || *secs != 2400 || *src != "lastfm" {
		t.Fatalf("%s %v %v", status, *secs, *src)
	}
}

func TestNoDataAnywhere(t *testing.T) {
	e := newEnv(t)
	if err := e.resolve(t); err != nil {
		t.Fatal(err)
	}
	if status, secs, _ := e.album(t); status != StatusNoData || secs != nil {
		t.Fatalf("%s %v", status, secs)
	}
	if len(e.resolved) != 1 {
		t.Fatal("no-data result should settle the album")
	}
}

func TestNetworkErrorIsFailedNotNoData(t *testing.T) {
	e := newEnv(t)
	e.mb.err = errors.New("connection refused")
	if err := e.resolve(t); err == nil {
		t.Fatal("expected error")
	}
	if status, _, _ := e.album(t); status != StatusFailed {
		t.Fatalf("status %s", status)
	}
	if len(e.resolved) != 0 {
		t.Fatal("failed lookup should not settle the album")
	}
	e.mb.err, e.mb.d = nil, time.Hour
	e.resolve(t)
	if status, secs, _ := e.album(t); status != StatusResolved || *secs != 3600 {
		t.Fatalf("after retry: %s %v", status, secs)
	}
}

func TestManualWins(t *testing.T) {
	e := newEnv(t)
	d := 41 * time.Minute
	if err := e.r.SetManual(context.Background(), 1, e.albumID, &d); err != nil {
		t.Fatal(err)
	}
	e.dc.master.Tracklist = tracks("1:00")
	e.r.store(context.Background(), e.albumID, StatusResolved, &d, "discogs_master")
	e.resolve(t)
	if status, secs, src := e.album(t); status != StatusManual || *secs != 2460 || *src != "manual" {
		t.Fatalf("%s %v %v", status, *secs, *src)
	}
	if len(e.resolved) != 2 {
		t.Fatalf("callbacks %v", e.resolved)
	}
}

func TestClearingManualQueuesLookup(t *testing.T) {
	e := newEnv(t)
	d := 41 * time.Minute
	e.r.SetManual(context.Background(), 1, e.albumID, &d)
	if err := e.r.SetManual(context.Background(), 1, e.albumID, nil); err != nil {
		t.Fatal(err)
	}
	if status, secs, _ := e.album(t); status != StatusPending || secs != nil {
		t.Fatalf("%s %v", status, secs)
	}
	select {
	case j := <-e.r.queue:
		if j.albumID != e.albumID {
			t.Fatalf("queued %+v", j)
		}
	default:
		t.Fatal("nothing queued")
	}
	if err := e.r.SetManual(context.Background(), 99, e.albumID, &d); err == nil {
		t.Fatal("other user changed the album")
	}
}

func TestSweepQueuesPendingAndFailed(t *testing.T) {
	e := newEnv(t)
	e.r.DB.W.Exec(`UPDATE albums SET duration_status = 'failed'`)
	e.r.init()
	e.r.sweep(context.Background())
	if len(e.r.queue) != 1 {
		t.Fatalf("queued %d", len(e.r.queue))
	}
}

func TestParseTrack(t *testing.T) {
	cases := map[string]time.Duration{"5:00": 5 * time.Minute, "1:02:03": time.Hour + 2*time.Minute + 3*time.Second, "0:45": 45 * time.Second}
	for in, want := range cases {
		if got, ok := ParseTrack(in); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	if got, ok := ParseTrack("0:01"); !ok || got != time.Second {
		t.Errorf("0:01: %v %v", got, ok)
	}
	for _, bad := range []string{"", "abc", "0:00", "-1:00", "5:xx", "1:-30"} {
		if _, ok := ParseTrack(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestTracklistSkipsHeadings(t *testing.T) {
	ts := append(tracks("2:00"), discogs.Track{Type: "heading", Title: "Side B"}, discogs.Track{Type: "track", Duration: "3:00"})
	d, n, complete := Tracklist(ts)
	if d != 5*time.Minute || n != 2 || !complete {
		t.Fatalf("%v %d %v", d, n, complete)
	}
}

func TestRunRetriesCoverAndLength(t *testing.T) {
	e := newEnv(t)
	e.r.DB.W.Exec(`UPDATE albums SET cover_status = 'failed' WHERE id = ?`, e.albumID)
	e.dc.master.Tracklist = tracks("20:00", "21:00")
	covers := make(chan int64, 1)
	e.r.ResolveCover = func(_ context.Context, _, albumID int64) error {
		covers <- albumID
		return nil
	}
	done := make(chan int64, 1)
	e.r.OnResolved = func(_, albumID int64) { done <- albumID }

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		e.r.Run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	for name, ch := range map[string]chan int64{"cover": covers, "length": done} {
		select {
		case id := <-ch:
			if id != e.albumID {
				t.Fatalf("%s retried for album %d", name, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s was not retried", name)
		}
	}
	if status, secs, _ := e.album(t); status != StatusResolved || *secs != 2460 {
		t.Fatalf("%s %v", status, *secs)
	}
}

func TestTracklistCompleteness(t *testing.T) {
	if _, n, complete := Tracklist(nil); n != 0 || complete {
		t.Fatalf("empty tracklist: %d %v", n, complete)
	}
	if d, n, complete := Tracklist(tracks("7:00")); d != 7*time.Minute || n != 1 || !complete {
		t.Fatalf("single track: %v %d %v", d, n, complete)
	}
}

func TestStandaloneReleaseThenLinkedPressingsEachOnce(t *testing.T) {
	e := newEnv(t)
	e.r.DB.W.Exec(`UPDATE albums SET discogs_release_id = 555 WHERE id = ?`, e.albumID)
	for _, id := range []int64{555, 777} {
		e.r.DB.W.Exec(`INSERT INTO releases (user_id, album_id, discogs_release_id, title, created_at) VALUES (1, ?, ?, 'x', 1)`, e.albumID, id)
	}
	e.dc.release.Tracklist = tracks("3:00", "")
	e.dc.releases = map[int64]discogs.Release{777: {Tracklist: tracks("10:00", "5:00")}}
	if err := e.resolve(t); err != nil {
		t.Fatal(err)
	}
	if status, secs, src := e.album(t); status != StatusResolved || *secs != 900 || *src != "discogs_release" {
		t.Fatalf("%s %v %v", status, secs, src)
	}
	for id, n := range e.dc.fetched {
		if n != 1 {
			t.Errorf("release %d fetched %d times", id, n)
		}
	}
}

func runUntilResolved(t *testing.T, r *Resolver, albumID int64) {
	t.Helper()
	done := make(chan int64, 1)
	r.OnResolved = func(_, id int64) { done <- id }
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	select {
	case id := <-done:
		if id != albumID {
			t.Fatalf("resolved album %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("length was not resolved")
	}
}

func TestRunSkipsCoverThatIsSettled(t *testing.T) {
	e := newEnv(t)
	e.dc.master.Tracklist = tracks("20:00")
	var covers int
	e.r.ResolveCover = func(context.Context, int64, int64) error {
		covers++
		return nil
	}
	runUntilResolved(t, e.r, e.albumID)
	if covers != 0 {
		t.Fatalf("settled cover downloaded again %d times", covers)
	}
}

func TestRunWithoutCoverResolver(t *testing.T) {
	e := newEnv(t)
	e.r.DB.W.Exec(`UPDATE albums SET cover_status = 'pending' WHERE id = ?`, e.albumID)
	e.dc.master.Tracklist = tracks("20:00")
	runUntilResolved(t, e.r, e.albumID)
	if status, secs, _ := e.album(t); status != StatusResolved || *secs != 1200 {
		t.Fatalf("%s %v", status, secs)
	}
}

func TestEnqueueLaterWaitsForRoomInsteadOfDropping(t *testing.T) {
	e := newEnv(t)
	e.r.init()
	for i := 0; i < cap(e.r.queue); i++ {
		e.r.Enqueue(1, int64(1000+i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.r.EnqueueLater(ctx, 1, 42)
	first := <-e.r.queue
	if first.albumID != 1000 {
		t.Fatalf("queue order %+v", first)
	}
	for i := 1; i < cap(e.r.queue); i++ {
		<-e.r.queue
	}
	select {
	case j := <-e.r.queue:
		if j.albumID != 42 {
			t.Fatalf("got %+v", j)
		}
	case <-time.After(time.Second):
		t.Fatal("the album waiting for room was dropped")
	}
}

func TestEnqueueLaterGivesUpWhenStopped(t *testing.T) {
	e := newEnv(t)
	e.r.init()
	for i := 0; i < cap(e.r.queue); i++ {
		e.r.Enqueue(1, int64(i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.r.EnqueueLater(ctx, 1, 42)
	cancel()
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < cap(e.r.queue); i++ {
		<-e.r.queue
	}
	select {
	case j := <-e.r.queue:
		t.Fatalf("queued after stopping: %+v", j)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAFailedRecheckKeepsTheLengthItHad(t *testing.T) {
	e := newEnv(t)
	e.r.DB.W.Exec(`UPDATE albums SET duration_status = 'pending', duration_sec = 2627, duration_source = 'discogs_release'`)
	e.dc.err = errors.New("Discogs is rate limiting requests")
	e.mb.err = errors.New("connection refused")
	e.resolve(t)
	if status, secs, source := e.album(t); status != StatusFailed || secs == nil || *secs != 2627 || source == nil {
		t.Fatalf("after a failed re-check: %s %v %v", status, secs, source)
	}
}
