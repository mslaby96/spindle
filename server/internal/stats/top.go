package stats

import (
	"context"
	"errors"
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

// Top ranks albums, genres and styles by number of listens or by time listened. An album with several styles
// counts towards each of them.
func (st *Stats) Top(ctx context.Context, userID int64, period, measure string, limit int) (Top, error) {
	window, ok := Periods[period]
	if !ok || (measure != "plays" && measure != "time") {
		return Top{}, ErrBadQuery
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	since := int64(0)
	if window > 0 {
		since = st.Now().Add(-window).Unix()
	}
	value := "s.play_count"
	if measure == "time" {
		value = "s.listened_sec"
	}
	where := `s.user_id = ? AND ` + listened + ` AND s.started_at >= ?`

	var t Top
	err := st.DB.R.QueryRowContext(ctx, `
		SELECT coalesce(sum(s.play_count), 0), coalesce(sum(s.listened_sec), 0),
			coalesce(sum(CASE WHEN s.duration_confirmed = 0 THEN s.listened_sec END), 0), count(DISTINCT s.album_id)
		FROM sessions s WHERE `+where, userID, since).
		Scan(&t.Summary.Plays, &t.Summary.ListenedSec, &t.Summary.UnconfirmedSec, &t.Summary.Albums)
	if err != nil {
		return Top{}, err
	}

	rows, err := st.DB.R.QueryContext(ctx, `
		SELECT s.album_id, sum(`+value+`) AS v FROM sessions s
		WHERE `+where+` AND s.album_id IS NOT NULL GROUP BY s.album_id ORDER BY v DESC, max(s.started_at) DESC LIMIT ?`,
		userID, since, limit)
	if err != nil {
		return Top{}, err
	}
	type pair struct{ id, v int64 }
	var top []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.v); err != nil {
			rows.Close()
			return Top{}, err
		}
		top = append(top, p)
	}
	rows.Close()
	t.Albums = []RankedAlbum{}
	for _, p := range top {
		item, err := st.AlbumItem(ctx, userID, p.id)
		if err != nil {
			return Top{}, err
		}
		t.Albums = append(t.Albums, RankedAlbum{Item: item, Value: p.v})
	}

	for _, field := range []struct {
		column string
		out    *[]Ranked
	}{{"genres", &t.Genres}, {"styles", &t.Styles}} {
		*field.out, err = st.ranked(ctx, `
			SELECT j.value, sum(`+value+`) AS v FROM sessions s JOIN albums a ON a.id = s.album_id, json_each(a.`+field.column+`) j
			WHERE `+where+` GROUP BY j.value ORDER BY v DESC, j.value LIMIT ?`, userID, since, limit)
		if err != nil {
			return Top{}, err
		}
	}
	return t, nil
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
