package duration

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/lastfm"
)

type DiscogsSource interface {
	Master(ctx context.Context, id int64) (discogs.Master, error)
	Release(ctx context.Context, id int64) (discogs.Release, error)
}

type MusicBrainzSource interface {
	AlbumLength(ctx context.Context, artist, title string, expectedTracks int) (time.Duration, error)
}

type LastFMSource interface {
	AlbumLength(ctx context.Context, artist, album string) (time.Duration, error)
}

const (
	StatusPending  = "pending"
	StatusResolved = "resolved"
	StatusNoData   = "no_data"
	StatusFailed   = "failed"
	StatusManual   = "manual"
)

// Settled reports whether an album length status is final, so listens of the album can be classified.
func Settled(status string) bool {
	return status == StatusResolved || status == StatusNoData || status == StatusManual
}

type job struct{ userID, albumID int64 }

type Resolver struct {
	DB          *db.DB
	Discogs     DiscogsSource
	MusicBrainz MusicBrainzSource
	LastFM      LastFMSource
	// ResolveCover is retried by the same worker so a failed download gets another chance.
	ResolveCover func(ctx context.Context, userID, albumID int64) error
	// OnResolved fires once an album's length is settled (found, missing everywhere, or set by hand).
	OnResolved func(userID, albumID int64)
	Log        *slog.Logger
	RetryEvery time.Duration

	queue chan job
}

func (r *Resolver) init() {
	if r.queue == nil {
		r.queue = make(chan job, 256)
	}
	if r.RetryEvery == 0 {
		r.RetryEvery = 30 * time.Minute
	}
}

// Enqueue schedules an album for resolution. When the queue is full the periodic sweep picks the album up later.
func (r *Resolver) Enqueue(userID, albumID int64) {
	r.init()
	select {
	case r.queue <- job{userID, albumID}:
	default:
	}
}

// EnqueueLater schedules an album and, when the queue is full, waits in the background for room instead of leaving it
// to the next sweep, so a large import gets its covers and lengths without a half-hour wait.
func (r *Resolver) EnqueueLater(ctx context.Context, userID, albumID int64) {
	r.init()
	go func() {
		select {
		case r.queue <- job{userID, albumID}:
		case <-ctx.Done():
		}
	}()
}

func (r *Resolver) Run(ctx context.Context) {
	r.init()
	r.sweep(ctx)
	tick := time.NewTicker(r.RetryEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.sweep(ctx)
		case j := <-r.queue:
			r.process(ctx, j)
		}
	}
}

func (r *Resolver) sweep(ctx context.Context) {
	rows, err := r.DB.R.QueryContext(ctx,
		`SELECT user_id, id FROM albums
		 WHERE duration_status IN ('pending', 'failed') OR cover_status IN ('pending', 'failed')`)
	if err != nil {
		r.Log.Error("metadata sweep", "err", err)
		return
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.userID, &j.albumID) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		r.Enqueue(j.userID, j.albumID)
	}
}

func (r *Resolver) process(ctx context.Context, j job) {
	if r.needsCover(ctx, j.albumID) {
		if err := r.ResolveCover(ctx, j.userID, j.albumID); err != nil {
			r.Log.Warn("cover download failed", "album", j.albumID, "err", err)
		}
	}
	if err := r.Resolve(ctx, j.userID, j.albumID); err != nil && !errors.Is(err, context.Canceled) {
		r.Log.Warn("album length lookup failed", "album", j.albumID, "err", err)
	}
}

func (r *Resolver) needsCover(ctx context.Context, albumID int64) bool {
	if r.ResolveCover == nil {
		return false
	}
	var status string
	err := r.DB.R.QueryRowContext(ctx, `SELECT cover_status FROM albums WHERE id = ?`, albumID).Scan(&status)
	return err == nil && (status == "pending" || status == "failed")
}

type albumInfo struct {
	masterID, releaseID sql.NullInt64
	title, artist       string
	status              string
}

// Resolve walks the sources in order and stores the first complete album length it finds.
func (r *Resolver) Resolve(ctx context.Context, userID, albumID int64) error {
	var a albumInfo
	err := r.DB.R.QueryRowContext(ctx,
		`SELECT discogs_master_id, discogs_release_id, title, artist, duration_status FROM albums WHERE id = ? AND user_id = ?`,
		albumID, userID).Scan(&a.masterID, &a.releaseID, &a.title, &a.artist, &a.status)
	if err != nil {
		return err
	}
	if !Settled(a.status) {
		source, d, err := r.find(ctx, userID, albumID, a)
		switch {
		case d > 0:
			r.store(ctx, albumID, StatusResolved, &d, source)
		case err != nil:
			r.store(ctx, albumID, StatusFailed, nil, "")
			return err
		default:
			r.store(ctx, albumID, StatusNoData, nil, "")
		}
	}
	r.resolved(userID, albumID)
	return nil
}

// find returns the first complete album length a source knows, or the errors that kept the sources from answering.
func (r *Resolver) find(ctx context.Context, userID, albumID int64, a albumInfo) (string, time.Duration, error) {
	var failed failures
	answered := failed.answered
	source, d, expected := r.fromDiscogs(ctx, userID, albumID, a, answered)
	if d > 0 {
		return source, d, nil
	}
	if r.MusicBrainz != nil {
		if d, err := r.MusicBrainz.AlbumLength(ctx, a.artist, a.title, expected); answered(d, err) {
			return "musicbrainz", d, nil
		}
	}
	if r.LastFM != nil {
		if d, err := r.LastFM.AlbumLength(ctx, a.artist, a.title); answered(d, err) {
			return "lastfm", d, nil
		}
	}
	return "", 0, errors.Join(failed...)
}

// failures collects the errors of sources that could not be asked. A source that doesn't know the album, or isn't
// configured, has answered.
type failures []error

func (f *failures) answered(d time.Duration, err error) bool {
	if err != nil && !errors.Is(err, lastfm.ErrNoKey) && !errors.Is(err, discogs.ErrNotFound) {
		*f = append(*f, err)
	}
	return err == nil && d > 0
}

// fromDiscogs sums the tracklist of the master, then of each pressing, until one lists every track's length. It also
// returns the track count it saw first, which helps pick the right release elsewhere.
func (r *Resolver) fromDiscogs(ctx context.Context, userID, albumID int64, a albumInfo,
	answered func(time.Duration, error) bool) (string, time.Duration, int) {
	expected := 0
	complete := func(tracks []discogs.Track, err error) (time.Duration, bool) {
		d, n, complete := Tracklist(tracks)
		if expected == 0 {
			expected = n
		}
		return d, answered(d, err) && complete
	}
	if a.masterID.Valid {
		m, err := r.Discogs.Master(ctx, a.masterID.Int64)
		if d, ok := complete(m.Tracklist, err); ok {
			return "discogs_master", d, expected
		}
	}
	for _, id := range r.releaseIDs(ctx, userID, albumID, a.releaseID) {
		rel, err := r.Discogs.Release(ctx, id)
		if d, ok := complete(rel.Tracklist, err); ok {
			return "discogs_release", d, expected
		}
	}
	return "", 0, expected
}

func (r *Resolver) releaseIDs(ctx context.Context, userID, albumID int64, standalone sql.NullInt64) []int64 {
	var ids []int64
	if standalone.Valid {
		ids = append(ids, standalone.Int64)
	}
	rows, err := r.DB.R.QueryContext(ctx,
		`SELECT discogs_release_id FROM releases WHERE album_id = ? AND user_id = ? ORDER BY id`, albumID, userID)
	if err != nil {
		return ids
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil && (!standalone.Valid || id != standalone.Int64) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *Resolver) store(ctx context.Context, albumID int64, status string, d *time.Duration, source string) {
	var secs, src any
	if d != nil {
		secs, src = int64(d.Round(time.Second)/time.Second), source
	}
	query := `UPDATE albums SET duration_status = ?, duration_sec = ?, duration_source = ?
		 WHERE id = ? AND duration_status != 'manual'`
	args := []any{status, secs, src, albumID}
	if status == StatusFailed {
		// a lookup that failed knows nothing new, so a length found before stays until a retry does better
		query, args = `UPDATE albums SET duration_status = ? WHERE id = ? AND duration_status != 'manual'`, []any{status, albumID}
	}
	_, err := r.DB.W.ExecContext(ctx, query, args...)
	if err != nil {
		r.Log.Error("store album length", "album", albumID, "err", err)
	}
}

func (r *Resolver) resolved(userID, albumID int64) {
	if r.OnResolved != nil {
		r.OnResolved(userID, albumID)
	}
}

// SetManual stores a length entered by the user, or clears it and looks the length up again when d is nil.
func (r *Resolver) SetManual(ctx context.Context, userID, albumID int64, d *time.Duration) error {
	var res sql.Result
	var err error
	if d != nil {
		res, err = r.DB.W.ExecContext(ctx,
			`UPDATE albums SET duration_status = 'manual', duration_sec = ?, duration_source = 'manual' WHERE id = ? AND user_id = ?`,
			int64(*d/time.Second), albumID, userID)
	} else {
		res, err = r.DB.W.ExecContext(ctx,
			`UPDATE albums SET duration_status = 'pending', duration_sec = NULL, duration_source = NULL WHERE id = ? AND user_id = ?`,
			albumID, userID)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if d != nil {
		r.resolved(userID, albumID)
	} else {
		r.Enqueue(userID, albumID)
	}
	return nil
}
