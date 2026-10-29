package stats

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var Periods = map[string]time.Duration{
	"30d":  30 * 24 * time.Hour,
	"90d":  90 * 24 * time.Hour,
	"year": 365 * 24 * time.Hour,
	"all":  0,
}

var ErrBadQuery = errors.New("period must be 30d, 90d, year or all and measure plays or time")

type Ranked struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type RankedAlbum struct {
	Item
	Value int64 `json:"value"`
}

type Summary struct {
	Plays          int64 `json:"plays"`
	ListenedSec    int64 `json:"listened_sec"`
	UnconfirmedSec int64 `json:"unconfirmed_sec"`
	Albums         int64 `json:"albums"`
}

type Top struct {
	Summary Summary       `json:"summary"`
	Albums  []RankedAlbum `json:"albums"`
	Genres  []Ranked      `json:"genres"`
	Styles  []Ranked      `json:"styles"`
}

// window is the part of the listening history one Top ranking looks at.
type window struct {
	userID int64
	since  int64
	value  string
	limit  int
}

const inWindow = `s.user_id = ? AND ` + listened + ` AND s.started_at >= ?`

func (st *Stats) window(userID int64, period, measure string, limit int) (window, error) {
	length, ok := Periods[period]
	if !ok || (measure != "plays" && measure != "time") {
		return window{}, ErrBadQuery
	}
	w := window{userID: userID, value: "s.play_count", limit: limit}
	if measure == "time" {
		w.value = "s.listened_sec"
	}
	if limit <= 0 || limit > 50 {
		w.limit = 10
	}
	if length > 0 {
		w.since = st.Now().Add(-length).Unix()
	}
	return w, nil
}

// Top ranks albums, genres and styles by number of listens or by time listened. An album with several styles
// counts towards each of them.
func (st *Stats) Top(ctx context.Context, userID int64, period, measure string, limit int) (Top, error) {
	w, err := st.window(userID, period, measure, limit)
	if err != nil {
		return Top{}, err
	}
	var t Top
	if t.Summary, err = st.summary(ctx, w); err != nil {
		return Top{}, err
	}
	if t.Albums, err = st.topAlbums(ctx, w); err != nil {
		return Top{}, err
	}
	if t.Genres, err = st.topTags(ctx, w, "genres"); err != nil {
		return Top{}, err
	}
	if t.Styles, err = st.topTags(ctx, w, "styles"); err != nil {
		return Top{}, err
	}
	return t, nil
}

func (st *Stats) summary(ctx context.Context, w window) (Summary, error) {
	var s Summary
	err := st.DB.R.QueryRowContext(ctx, `
		SELECT coalesce(sum(s.play_count), 0), coalesce(sum(s.listened_sec), 0),
			coalesce(sum(CASE WHEN s.duration_confirmed = 0 THEN s.listened_sec END), 0), count(DISTINCT s.album_id)
		FROM sessions s WHERE `+inWindow, w.userID, w.since).
		Scan(&s.Plays, &s.ListenedSec, &s.UnconfirmedSec, &s.Albums)
	return s, err
}

func (st *Stats) topAlbums(ctx context.Context, w window) ([]RankedAlbum, error) {
	top, err := st.ranked(ctx, `
		SELECT s.album_id, sum(`+w.value+`) AS v FROM sessions s
		WHERE `+inWindow+` AND s.album_id IS NOT NULL GROUP BY s.album_id ORDER BY v DESC, max(s.started_at) DESC LIMIT ?`,
		w.userID, w.since, w.limit)
	if err != nil {
		return nil, err
	}
	out := []RankedAlbum{}
	for _, r := range top {
		id, err := strconv.ParseInt(r.Name, 10, 64)
		if err != nil {
			return nil, err
		}
		item, err := st.AlbumItem(ctx, w.userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, RankedAlbum{Item: item, Value: r.Value})
	}
	return out, nil
}

// topTags ranks the values of a JSON list column on albums, such as genres or styles.
func (st *Stats) topTags(ctx context.Context, w window, column string) ([]Ranked, error) {
	return st.ranked(ctx, `
		SELECT j.value, sum(`+w.value+`) AS v FROM sessions s JOIN albums a ON a.id = s.album_id, json_each(a.`+column+`) j
		WHERE `+inWindow+` GROUP BY j.value ORDER BY v DESC, j.value LIMIT ?`, w.userID, w.since, w.limit)
}

func (st *Stats) ranked(ctx context.Context, query string, args ...any) ([]Ranked, error) {
	rows, err := st.DB.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ranked{}
	for rows.Next() {
		var r Ranked
		if err := rows.Scan(&r.Name, &r.Value); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
