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
	master  discogs.Master
	release discogs.Release
	err     error
}

func (f *fakeDiscogs) Master(context.Context, int64) (discogs.Master, error) { return f.master, f.err }
func (f *fakeDiscogs) Release(context.Context, int64) (discogs.Release, error) {
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
	for _, bad := range []string{"", "abc", "0:00", "-1:00"} {
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
