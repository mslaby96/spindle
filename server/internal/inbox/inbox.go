package inbox

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

var (
	ErrNotFound      = errors.New("listen not found")
	ErrInvalidRating = errors.New("rating must be between half a star and five stars")
)

const maxNote = 2000

type AlbumSummary struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	HasCover   bool   `json:"has_cover"`
	CoverColor string `json:"cover_color,omitempty"`
	Rating     *int   `json:"rating,omitempty"`
	LengthSec  *int64 `json:"length_sec,omitempty"`
}

type Card struct {
	Session sessions.Session `json:"session"`
	UID     string           `json:"uid"`
	Album   *AlbumSummary    `json:"album,omitempty"`
}

type Inbox struct {
	DB  *db.DB
	Bus *events.Bus
	Now func() time.Time
}

// List returns listens that still want the listener's attention, newest first: sleeves without an album,
// listens whose length needs confirming and fresh listens that can be rated.
func (in *Inbox) List(ctx context.Context, userID int64) ([]Card, error) {
	rows, err := in.DB.R.QueryContext(ctx, `
		SELECT `+sessions.Columns("s")+`,
			t.uid, a.title, a.artist, a.cover_path IS NOT NULL, coalesce(a.cover_color, ''), a.duration_sec
		FROM sessions s
		JOIN tags t ON t.id = s.tag_id
		LEFT JOIN albums a ON a.id = s.album_id
		WHERE s.user_id = ? AND s.inbox_state = 'open'
			AND (s.state IN ('counted', 'pending_confirmation') OR (s.state = 'unclassified' AND s.album_id IS NULL))
		ORDER BY s.started_at DESC, s.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if err := in.rate(ctx, userID, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanCard(rows *sql.Rows) (Card, error) {
	var c Card
	var title, artist sql.NullString
	var hasCover sql.NullBool
	var length sql.NullInt64
	var color string
	s, err := sessions.ScanSession(rows, &c.UID, &title, &artist, &hasCover, &color, &length)
	if err != nil {
		return Card{}, err
	}
	c.Session = s
	if s.AlbumID != nil {
		c.Album = &AlbumSummary{ID: *s.AlbumID, Title: title.String, Artist: artist.String, HasCover: hasCover.Bool, CoverColor: color}
		if length.Valid {
			c.Album.LengthSec = &length.Int64
		}
	}
	return c, nil
}

func (in *Inbox) rate(ctx context.Context, userID int64, c *Card) error {
	if c.Album == nil {
		return nil
	}
	r, err := in.AlbumRating(ctx, userID, c.Album.ID)
	c.Album.Rating = r
	return err
}

// AlbumRating is the rating from the most recent listen that has one, the way Letterboxd shows a film's rating.
func (in *Inbox) AlbumRating(ctx context.Context, userID, albumID int64) (*int, error) {
	var r int
	err := in.DB.R.QueryRowContext(ctx, `
		SELECT rating FROM sessions WHERE user_id = ? AND album_id = ? AND rating IS NOT NULL
		ORDER BY started_at DESC, id DESC LIMIT 1`, userID, albumID).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Review stores the rating (in half stars, 1-10, or nil) and note for one listen and closes its card, unless the
// listen still waits for its length to be confirmed.
// CheckRating accepts no rating or one in half stars, 1 to 10.
func CheckRating(rating *int) error {
	if rating != nil && (*rating < 1 || *rating > 10) {
		return ErrInvalidRating
	}
	return nil
}

func (in *Inbox) Review(ctx context.Context, userID, sessionID int64, rating *int, note string) error {
	if err := CheckRating(rating); err != nil {
		return err
	}
	note = strings.TrimSpace(note)
	if r := []rune(note); len(r) > maxNote {
		note = string(r[:maxNote])
	}
	var ratedAt any
	if rating != nil {
		ratedAt = in.Now().Unix()
	}
	res, err := in.DB.W.ExecContext(ctx, `
		UPDATE sessions SET rating = ?, rated_at = ?, note = ?,
			inbox_state = CASE WHEN state = 'pending_confirmation' THEN 'open' ELSE 'done' END
		WHERE id = ? AND user_id = ? AND state IN ('counted', 'pending_confirmation')`,
		rating, ratedAt, nullString(note), sessionID, userID)
	return in.changed(userID, res, err)
}

// Dismiss closes a card without changing the listen. A listen of a sleeve that never got an album is deleted.
func (in *Inbox) Dismiss(ctx context.Context, userID, sessionID int64) error {
	res, err := in.DB.W.ExecContext(ctx,
		`DELETE FROM sessions WHERE id = ? AND user_id = ? AND state = 'unclassified' AND album_id IS NULL`, sessionID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		in.publish(userID)
		return nil
	}
	res, err = in.DB.W.ExecContext(ctx,
		`UPDATE sessions SET inbox_state = 'dismissed' WHERE id = ? AND user_id = ? AND inbox_state = 'open'`, sessionID, userID)
	return in.changed(userID, res, err)
}

func (in *Inbox) changed(userID int64, res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	in.publish(userID)
	return nil
}

func (in *Inbox) publish(userID int64) {
	if in.Bus != nil {
		in.Bus.Publish(events.Event{Type: sessions.EventInboxChanged, UserID: userID})
	}
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
