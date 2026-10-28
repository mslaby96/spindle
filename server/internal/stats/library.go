package stats

import (
	"context"
	"database/sql"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

// listened is the condition for a session that counts as a listen: counted, or awaiting confirmation of its length.
const listened = `s.state IN ('counted', 'pending_confirmation')`

type Stats struct {
	DB  *db.DB
	Now func() time.Time
	Loc *time.Location
}

type Item struct {
	AlbumID      int64      `json:"album_id"`
	Title        string     `json:"title"`
	Artist       string     `json:"artist"`
	Year         *int       `json:"year,omitempty"`
	HasCover     bool       `json:"has_cover"`
	CoverColor   string     `json:"cover_color,omitempty"`
	Plays        int        `json:"plays"`
	ListenedSec  int64      `json:"listened_sec"`
	LastPlayedAt *time.Time `json:"last_played_at,omitempty"`
	Rating       *int       `json:"rating,omitempty"`
	AddedAt      time.Time  `json:"added_at"`
}

var sorts = map[string]string{
	"last_played": "last_played IS NULL, last_played DESC, a.title",
	"plays":       "plays DESC, last_played DESC, a.title",
	"title":       "a.title COLLATE NOCASE, a.artist COLLATE NOCASE",
	"artist":      "a.artist COLLATE NOCASE, a.year, a.title COLLATE NOCASE",
	"added":       "a.created_at DESC, a.id DESC",
}

const itemQuery = `
	SELECT a.id, a.title, a.artist, a.year, a.cover_path IS NOT NULL, coalesce(a.cover_color, ''), a.created_at,
		coalesce(sum(CASE WHEN ` + listened + ` THEN s.play_count END), 0) AS plays,
		coalesce(sum(CASE WHEN ` + listened + ` THEN s.listened_sec END), 0),
		max(CASE WHEN ` + listened + ` THEN s.started_at END) AS last_played,
		(SELECT r.rating FROM sessions r WHERE r.album_id = a.id AND r.rating IS NOT NULL
			ORDER BY r.started_at DESC, r.id DESC LIMIT 1)
	FROM albums a
	LEFT JOIN sessions s ON s.album_id = a.id
	WHERE a.user_id = ?`

func (st *Stats) Library(ctx context.Context, userID int64, sort string) ([]Item, error) {
	order, ok := sorts[sort]
	if !ok {
		order = sorts["last_played"]
	}
	return st.items(ctx, itemQuery+` GROUP BY a.id ORDER BY `+order, userID)
}

type Forgotten struct {
	NotRecently []Item `json:"not_recently"`
	Never       []Item `json:"never"`
}

// Forgotten lists albums not played for longer than after, oldest listen first, and albums never played since
// they were catalogued, oldest addition first.
func (st *Stats) Forgotten(ctx context.Context, userID int64, after time.Duration) (Forgotten, error) {
	cutoff := st.Now().Add(-after).Unix()
	var f Forgotten
	var err error
	f.NotRecently, err = st.items(ctx, itemQuery+` GROUP BY a.id HAVING last_played < ? ORDER BY last_played`, userID, cutoff)
	if err != nil {
		return f, err
	}
	f.Never, err = st.items(ctx, itemQuery+` GROUP BY a.id HAVING last_played IS NULL ORDER BY a.created_at`, userID)
	return f, err
}

func (st *Stats) items(ctx context.Context, query string, args ...any) ([]Item, error) {
	rows, err := st.DB.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var year, last, rating sql.NullInt64
		var added int64
		if err := rows.Scan(&it.AlbumID, &it.Title, &it.Artist, &year, &it.HasCover, &it.CoverColor, &added,
			&it.Plays, &it.ListenedSec, &last, &rating); err != nil {
			return nil, err
		}
		it.AddedAt = time.Unix(added, 0).UTC()
		if year.Valid {
			y := int(year.Int64)
			it.Year = &y
		}
		if last.Valid {
			t := time.Unix(last.Int64, 0).UTC()
			it.LastPlayedAt = &t
		}
		if rating.Valid {
			r := int(rating.Int64)
			it.Rating = &r
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

type DiaryEntry struct {
	SessionID   int64     `json:"session_id"`
	AlbumID     int64     `json:"album_id"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	HasCover    bool      `json:"has_cover"`
	CoverColor  string    `json:"cover_color,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	Plays       int       `json:"plays"`
	ListenedSec int64     `json:"listened_sec"`
	Confirmed   bool      `json:"duration_confirmed"`
	Rating      *int      `json:"rating,omitempty"`
	Note        string    `json:"note,omitempty"`
}

type DiaryFilter struct {
	AlbumID int64
	Before  *time.Time
	Limit   int
}

// Diary returns listens newest first. Pass the started_at of the last entry seen as Before to get the next page.
func (st *Stats) Diary(ctx context.Context, userID int64, f DiaryFilter) ([]DiaryEntry, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cursor := int64(1 << 62)
	if f.Before != nil {
		cursor = f.Before.Unix()
	}
	rows, err := st.DB.R.QueryContext(ctx, `
		SELECT s.id, a.id, a.title, a.artist, a.cover_path IS NOT NULL, coalesce(a.cover_color, ''), s.started_at,
			s.play_count, s.listened_sec, s.duration_confirmed, s.rating, coalesce(s.note, '')
		FROM sessions s JOIN albums a ON a.id = s.album_id
		WHERE s.user_id = ? AND `+listened+` AND s.started_at < ? AND (? = 0 OR s.album_id = ?)
		ORDER BY s.started_at DESC, s.id DESC LIMIT ?`, userID, cursor, f.AlbumID, f.AlbumID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiaryEntry{}
	for rows.Next() {
		var e DiaryEntry
		var started int64
		var rating sql.NullInt64
		if err := rows.Scan(&e.SessionID, &e.AlbumID, &e.Title, &e.Artist, &e.HasCover, &e.CoverColor, &started,
			&e.Plays, &e.ListenedSec, &e.Confirmed, &rating, &e.Note); err != nil {
			return nil, err
		}
		e.StartedAt = time.Unix(started, 0).UTC()
		if rating.Valid {
			r := int(rating.Int64)
			e.Rating = &r
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (st *Stats) AlbumItem(ctx context.Context, userID, albumID int64) (Item, error) {
	items, err := st.items(ctx, itemQuery+` AND a.id = ? GROUP BY a.id`, userID, albumID)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, sql.ErrNoRows
	}
	return items[0], nil
}
