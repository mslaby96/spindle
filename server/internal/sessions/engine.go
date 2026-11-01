package sessions

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/metadata/duration"
	"github.com/mslaby96/spindle/server/internal/presence"
)

type State string

const (
	StateActive              State = "active"
	StateUnclassified        State = "unclassified"
	StateDiscarded           State = "discarded"
	StateCounted             State = "counted"
	StatePendingConfirmation State = "pending_confirmation"
)

type Resolution string

const (
	ResolutionForgot   Resolution = "forgot"
	ResolutionRepeated Resolution = "repeated"
	ResolutionLonger   Resolution = "longer"
	ResolutionAuto     Resolution = "auto"
)

const (
	EventNowPlaying   = "now_playing"
	EventInboxChanged = "inbox_changed"

	// typicalLP stands in for the album length when no source knows it and the listener still has to be credited.
	typicalLP = 45 * time.Minute
	// staleReport is how long without a report of the tag before Current treats the sleeve as off the stand.
	staleReport = 5 * time.Second
	// failedLookupGrace is how long a listen waits for a failing album length lookup before the fallback is used.
	failedLookupGrace = 24 * time.Hour
)

var (
	ErrNotFound          = errors.New("session not found")
	ErrNotPending        = errors.New("session is not waiting for confirmation")
	ErrUnknownResolution = errors.New("resolution must be forgot, repeated or longer")
	ErrSleeveOnStand     = errors.New("the sleeve is still on the stand")
)

type Tags interface {
	EnsureTag(ctx context.Context, userID int64, uid string) (catalog.Tag, bool, error)
}

type LengthQueue interface {
	Enqueue(userID, albumID int64)
}

type NowPlaying struct {
	DeviceID  int64          `json:"device_id"`
	Active    bool           `json:"active"`
	UID       string         `json:"uid,omitempty"`
	Album     *catalog.Album `json:"album,omitempty"`
	StartedAt *time.Time     `json:"started_at,omitempty"`
	// EndsAt is set while the sleeve is off the stand: the listen ends then unless it comes back.
	EndsAt *time.Time `json:"ends_at,omitempty"`
}

// Engine turns tag observations into listening sessions. All state lives in the database; the mutex only makes
// observations and the sweeper take turns so a report racing a timeout can't open a second session.
type Engine struct {
	DB        *db.DB
	Tags      Tags
	Bus       *events.Bus
	Cfg       config.Sessions
	Now       func() time.Time
	Durations LengthQueue
	Log       *slog.Logger

	mu   sync.Mutex
	away map[int64]bool
}

func (e *Engine) Observe(ctx context.Context, o presence.Observation) (presence.LED, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if o.UID == "" {
		return presence.LEDIdle, e.sleeveRemoved(ctx, o)
	}

	tag, _, err := e.Tags.EnsureTag(ctx, o.UserID, o.UID)
	if err != nil {
		return "", err
	}
	led := presence.LEDUnknown
	if tag.Album != nil {
		led = presence.LEDKnown
	}
	if err := e.track(ctx, o, tag); err != nil {
		return "", err
	}
	return led, nil
}

// track keeps the device's active listen going while the same sleeve is reported, or swaps it for a new one.
func (e *Engine) track(ctx context.Context, o presence.Observation, tag catalog.Tag) error {
	var id, tagID, startedAt int64
	err := e.DB.R.QueryRowContext(ctx,
		`SELECT id, tag_id, started_at FROM sessions WHERE device_id = ? AND state = 'active'`, o.DeviceID).
		Scan(&id, &tagID, &startedAt)
	switch {
	case err == nil && tagID == tag.ID:
		return e.stillPlaying(ctx, o, tag, id, time.Unix(startedAt, 0).UTC())
	case err == nil:
		if err := e.close(ctx, o.UserID, id); err != nil {
			return err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	return e.start(ctx, o, tag)
}

func (e *Engine) stillPlaying(ctx context.Context, o presence.Observation, tag catalog.Tag, id int64, started time.Time) error {
	if _, err := e.DB.W.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, o.At.Unix(), id); err != nil {
		return err
	}
	if e.away[o.DeviceID] {
		delete(e.away, o.DeviceID)
		e.publish(o.UserID, EventNowPlaying, NowPlaying{DeviceID: o.DeviceID, Active: true, UID: tag.UID, Album: tag.Album, StartedAt: &started})
	}
	return nil
}

func (e *Engine) start(ctx context.Context, o presence.Observation, tag catalog.Tag) error {
	var albumID, releaseID any
	if tag.Album != nil {
		albumID = tag.Album.ID
	}
	if tag.Release != nil {
		releaseID = tag.Release.ID
	}
	_, err := e.DB.W.ExecContext(ctx,
		`INSERT INTO sessions (user_id, device_id, tag_id, album_id, release_id, state, started_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
		o.UserID, o.DeviceID, tag.ID, albumID, releaseID, o.At.Unix(), o.At.Unix())
	if err != nil {
		return err
	}
	started := time.Unix(o.At.Unix(), 0).UTC()
	e.publish(o.UserID, EventNowPlaying, NowPlaying{DeviceID: o.DeviceID, Active: true, UID: tag.UID, Album: tag.Album, StartedAt: &started})
	return nil
}

// Sweep ends sessions whose sleeve has been gone longer than the tolerance, settles week-old confirmations and
// classifies listens whose album length has become known in the meantime.
func (e *Engine) Sweep(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()

	stale, err := e.ids(ctx, `SELECT id, user_id FROM sessions WHERE state = 'active' AND last_seen_at < ?`,
		now.Add(-e.Cfg.AbsenceTolerance).Unix())
	if err != nil {
		return err
	}
	var errs []error
	for _, s := range stale {
		errs = append(errs, e.close(ctx, s.userID, s.id))
	}

	expired, err := e.ids(ctx, `SELECT id, user_id FROM sessions WHERE state = 'pending_confirmation' AND classified_at <= ?`,
		now.Add(-e.Cfg.AutoResolveAfter).Unix())
	if err != nil {
		return err
	}
	for _, s := range expired {
		errs = append(errs, e.resolve(ctx, s.userID, s.id, ResolutionAuto))
	}

	waiting, err := e.ids(ctx, `SELECT s.id, s.user_id FROM sessions s JOIN albums a ON a.id = s.album_id
		WHERE s.state = 'unclassified' AND (a.duration_status IN ('resolved', 'no_data', 'manual')
			OR (a.duration_status = 'failed' AND s.ended_at < ?))`, now.Add(-failedLookupGrace).Unix())
	if err != nil {
		return err
	}
	for _, s := range waiting {
		errs = append(errs, e.classify(ctx, s.userID, s.id, true))
	}
	return errors.Join(errs...)
}

func (e *Engine) RunSweeper(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := e.Sweep(ctx); err != nil && ctx.Err() == nil && e.Log != nil {
				e.Log.Error("session sweep", "err", err)
			}
		}
	}
}

// ClassifyAlbum classifies listens that were waiting for this album's length; call it once the length is settled.
func (e *Engine) ClassifyAlbum(ctx context.Context, userID, albumID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	waiting, err := e.ids(ctx, `SELECT id, user_id FROM sessions WHERE user_id = ? AND album_id = ? AND state = 'unclassified'`,
		userID, albumID)
	if err != nil {
		return err
	}
	for _, s := range waiting {
		if err := e.classify(ctx, s.userID, s.id, false); err != nil {
			return err
		}
	}
	return nil
}

// EndNow ends a listen whose sleeve is off the stand without waiting for the absence tolerance to run out. It ends when
// the sleeve was last seen, as it would have anyway.
func (e *Engine) EndNow(ctx context.Context, userID, deviceID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var id, lastSeen int64
	err := e.DB.R.QueryRowContext(ctx,
		`SELECT id, last_seen_at FROM sessions WHERE user_id = ? AND device_id = ? AND state = 'active'`, userID, deviceID).
		Scan(&id, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !e.away[deviceID] && !e.offStand(lastSeen) {
		return ErrSleeveOnStand
	}
	return e.close(ctx, userID, id)
}

// offStand tells whether the stand has stopped reporting a listen's sleeve.
func (e *Engine) offStand(lastSeen int64) bool {
	return e.Now().Sub(time.Unix(lastSeen, 0)) > staleReport
}

func (e *Engine) Resolve(ctx context.Context, userID, sessionID int64, r Resolution) error {
	switch r {
	case ResolutionForgot, ResolutionRepeated, ResolutionLonger:
	default:
		return ErrUnknownResolution
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resolve(ctx, userID, sessionID, r)
}

// sleeveRemoved tells listeners once that the sleeve left the stand and when the listen will end if it stays away.
func (e *Engine) sleeveRemoved(ctx context.Context, o presence.Observation) error {
	if e.away[o.DeviceID] {
		return nil
	}
	var uid string
	var startedAt, lastSeen int64
	err := e.DB.R.QueryRowContext(ctx, `
		SELECT t.uid, s.started_at, s.last_seen_at FROM sessions s JOIN tags t ON t.id = s.tag_id
		WHERE s.device_id = ? AND s.state = 'active'`, o.DeviceID).Scan(&uid, &startedAt, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tag, _, err := e.Tags.EnsureTag(ctx, o.UserID, uid)
	if err != nil {
		return err
	}
	if e.away == nil {
		e.away = map[int64]bool{}
	}
	e.away[o.DeviceID] = true
	started := time.Unix(startedAt, 0).UTC()
	ends := time.Unix(lastSeen, 0).Add(e.Cfg.AbsenceTolerance).UTC()
	e.publish(o.UserID, EventNowPlaying, NowPlaying{DeviceID: o.DeviceID, Active: true, UID: uid, Album: tag.Album,
		StartedAt: &started, EndsAt: &ends})
	return nil
}

func (e *Engine) close(ctx context.Context, userID, id int64) error {
	var deviceID int64
	if err := e.DB.R.QueryRowContext(ctx, `SELECT device_id FROM sessions WHERE id = ?`, id).Scan(&deviceID); err != nil {
		return err
	}
	delete(e.away, deviceID)
	if _, err := e.DB.W.ExecContext(ctx,
		`UPDATE sessions SET state = 'unclassified', ended_at = last_seen_at WHERE id = ? AND state = 'active'`, id); err != nil {
		return err
	}
	e.publish(userID, EventNowPlaying, NowPlaying{DeviceID: deviceID})
	return e.classify(ctx, userID, id, false)
}

type lengthInfo struct {
	albumID sql.NullInt64
	status  sql.NullString
	seconds sql.NullInt64
	started int64
	ended   int64
}

func (e *Engine) classify(ctx context.Context, userID, id int64, useFallback bool) error {
	var l lengthInfo
	err := e.DB.R.QueryRowContext(ctx,
		`SELECT s.album_id, a.duration_status, a.duration_sec, s.started_at, s.ended_at
		 FROM sessions s LEFT JOIN albums a ON a.id = s.album_id WHERE s.id = ?`, id).
		Scan(&l.albumID, &l.status, &l.seconds, &l.started, &l.ended)
	if err != nil {
		return err
	}
	if !l.albumID.Valid {
		return nil
	}
	if !duration.Settled(l.status.String) && !useFallback {
		if e.Durations != nil {
			e.Durations.Enqueue(userID, l.albumID.Int64)
		}
		return nil
	}

	state, plays, listened := e.judge(time.Duration(l.ended-l.started)*time.Second, l.seconds)
	confirmed := state != StatePendingConfirmation
	_, err = e.DB.W.ExecContext(ctx,
		`UPDATE sessions SET state = ?, play_count = ?, listened_sec = ?, duration_confirmed = ?, classified_at = ?
		 WHERE id = ? AND state = 'unclassified'`,
		state, plays, int64(listened/time.Second), confirmed, e.Now().Unix(), id)
	if err != nil {
		return err
	}
	if state != StateDiscarded {
		e.publish(userID, EventInboxChanged, nil)
	}
	return nil
}

// judge decides what a finished listen counts as: too short to count, one listen, or too long to count without asking.
func (e *Engine) judge(length time.Duration, albumSec sql.NullInt64) (State, int, time.Duration) {
	switch {
	case length < e.Cfg.MinDuration:
		return StateDiscarded, 0, 0
	case length > e.longThreshold(albumSec):
		return StatePendingConfirmation, 1, length
	}
	return StateCounted, 1, length
}

func (e *Engine) longThreshold(albumSec sql.NullInt64) time.Duration {
	if !albumSec.Valid || albumSec.Int64 <= 0 {
		return e.Cfg.FallbackLong
	}
	return time.Duration(float64(albumSec.Int64) * e.Cfg.LongFactor * float64(time.Second))
}

func (e *Engine) resolve(ctx context.Context, userID, id int64, r Resolution) error {
	var state string
	var l lengthInfo
	err := e.DB.R.QueryRowContext(ctx,
		`SELECT s.state, a.duration_sec, s.started_at, s.ended_at FROM sessions s
		 LEFT JOIN albums a ON a.id = s.album_id WHERE s.id = ? AND s.user_id = ?`, id, userID).
		Scan(&state, &l.seconds, &l.started, &l.ended)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if State(state) != StatePendingConfirmation {
		return ErrNotPending
	}

	plays, listened := credit(r, time.Duration(l.ended-l.started)*time.Second, l.seconds)
	_, err = e.DB.W.ExecContext(ctx,
		`UPDATE sessions SET state = 'counted', play_count = ?, listened_sec = ?, duration_confirmed = 1, resolution = ?
		 WHERE id = ?`, plays, int64(listened/time.Second), r, id)
	if err == nil {
		e.publish(userID, EventInboxChanged, nil)
	}
	return err
}

// credit is how many listens and how much time a session of the given length is worth once its resolution is known.
func credit(r Resolution, length time.Duration, albumSec sql.NullInt64) (int, time.Duration) {
	album := typicalLP
	if albumSec.Valid && albumSec.Int64 > 0 {
		album = time.Duration(albumSec.Int64) * time.Second
	}
	switch r {
	case ResolutionForgot, ResolutionAuto:
		return 1, min(album, length)
	case ResolutionRepeated:
		return repeatedPlays(length, album), length
	}
	return 1, length
}

func repeatedPlays(length, album time.Duration) int {
	return max(1, int(math.Round(float64(length)/float64(album))))
}

type ref struct{ id, userID int64 }

func (e *Engine) ids(ctx context.Context, query string, args ...any) ([]ref, error) {
	rows, err := e.DB.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.userID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (e *Engine) publish(userID int64, typ string, data any) {
	if e.Bus != nil {
		e.Bus.Publish(events.Event{Type: typ, UserID: userID, Data: data})
	}
}

// Current returns what is playing on each of the user's stands right now.
func (e *Engine) Current(ctx context.Context, userID int64, albums func(context.Context, int64, int64) (catalog.Album, error)) ([]NowPlaying, error) {
	rows, err := e.DB.R.QueryContext(ctx,
		`SELECT s.device_id, t.uid, s.album_id, s.started_at, s.last_seen_at FROM sessions s JOIN tags t ON t.id = s.tag_id
		 WHERE s.user_id = ? AND s.state = 'active' ORDER BY s.device_id`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		np      NowPlaying
		albumID sql.NullInt64
	}
	var list []row
	for rows.Next() {
		var r row
		var started, lastSeen int64
		if err := rows.Scan(&r.np.DeviceID, &r.np.UID, &r.albumID, &started, &lastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		t := time.Unix(started, 0).UTC()
		r.np.Active, r.np.StartedAt = true, &t
		if e.offStand(lastSeen) {
			ends := time.Unix(lastSeen, 0).Add(e.Cfg.AbsenceTolerance).UTC()
			r.np.EndsAt = &ends
		}
		list = append(list, r)
	}
	rows.Close()
	out := []NowPlaying{}
	for _, r := range list {
		if r.albumID.Valid {
			a, err := albums(ctx, userID, r.albumID.Int64)
			if err != nil {
				return nil, err
			}
			r.np.Album = &a
		}
		out = append(out, r.np)
	}
	return out, nil
}
