package stats

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"
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
	// ArtistKey and TitleKey are what the library is filed under, so the app groups records the way they are sorted.
	ArtistKey string `json:"artist_key"`
	TitleKey  string `json:"title_key"`
}

// filingKey is how a record shelf files a name: case doesn't matter and a leading "The" is skipped, so The Beatles
// sit under B.
func filingKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if rest, ok := strings.CutPrefix(key, "the "); ok {
		key = strings.TrimSpace(rest)
	}
	return key
}

// filed orders by filing keys, which SQL can't compute, for the sorts that a reader expects alphabetically.
var filed = map[string]func(a, b Item) int{
	"title": func(a, b Item) int {
		return cmp.Or(strings.Compare(a.TitleKey, b.TitleKey), strings.Compare(a.ArtistKey, b.ArtistKey))
	},
	"artist": func(a, b Item) int {
		return cmp.Or(strings.Compare(a.ArtistKey, b.ArtistKey), cmp.Compare(yearOf(a), yearOf(b)),
			strings.Compare(a.TitleKey, b.TitleKey))
	},
}

func yearOf(it Item) int {
	if it.Year == nil {
		return 0
	}
	return *it.Year
}

var sorts = map[string]string{
	"last_played": "last_played IS NULL, last_played DESC, a.title",
	"plays":       "plays DESC, last_played DESC, a.title",
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

// onShelf keeps records that were sold or lost out of the library and the forgotten shelf; their listens still count.
const onShelf = ` AND a.removed_at IS NULL`

func (st *Stats) Library(ctx context.Context, userID int64, sort string) ([]Item, error) {
	if byKey, ok := filed[sort]; ok {
		items, err := st.items(ctx, itemQuery+onShelf+` GROUP BY a.id ORDER BY a.id`, userID)
		slices.SortStableFunc(items, byKey)
		return items, err
	}
	order, ok := sorts[sort]
	if !ok {
		order = sorts["last_played"]
	}
	return st.items(ctx, itemQuery+onShelf+` GROUP BY a.id ORDER BY `+order, userID)
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
	f.NotRecently, err = st.items(ctx, itemQuery+onShelf+` GROUP BY a.id HAVING last_played < ? ORDER BY last_played`, userID, cutoff)
	if err != nil {
		return f, err
	}
	f.Never, err = st.items(ctx, itemQuery+onShelf+` GROUP BY a.id HAVING last_played IS NULL ORDER BY a.created_at`, userID)
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
		it.ArtistKey, it.TitleKey = filingKey(it.Artist), filingKey(it.Title)
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
	// Before and BeforeID are the started_at and session id of the last entry already shown.
	Before   *time.Time
	BeforeID int64
	Limit    int
}

// Diary returns listens newest first. Pass the started_at of the last entry seen as Before to get the next page.
func (st *Stats) Diary(ctx context.Context, userID int64, f DiaryFilter) ([]DiaryEntry, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cursor, cursorID := int64(1<<62), int64(1<<62)
	if f.Before != nil {
		cursor = f.Before.Unix()
		if f.BeforeID > 0 {
			cursorID = f.BeforeID
		}
	}
	rows, err := st.DB.R.QueryContext(ctx, `
		SELECT s.id, a.id, a.title, a.artist, a.cover_path IS NOT NULL, coalesce(a.cover_color, ''), s.started_at,
			s.play_count, s.listened_sec, s.duration_confirmed, s.rating, coalesce(s.note, '')
		FROM sessions s JOIN albums a ON a.id = s.album_id
		WHERE s.user_id = ? AND `+listened+` AND (s.started_at < ? OR (s.started_at = ? AND s.id < ?))
			AND (? = 0 OR s.album_id = ?)
		ORDER BY s.started_at DESC, s.id DESC LIMIT ?`, userID, cursor, cursor, cursorID, f.AlbumID, f.AlbumID, limit)
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
