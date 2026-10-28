package stats

import (
	"context"
	"time"
)

type Week struct {
	Start       string `json:"start"`
	Plays       int64  `json:"plays"`
	ListenedSec int64  `json:"listened_sec"`
}

func startOfWeek(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}

// Timeline returns listens per week (Monday to Sunday in the configured time zone) for the last n weeks, oldest
// first, including weeks without any listens.
func (st *Stats) Timeline(ctx context.Context, userID int64, weeks int) ([]Week, error) {
	if weeks <= 0 || weeks > 520 {
		weeks = 26
	}
	loc := st.Loc
	if loc == nil {
		loc = time.Local
	}
	current := startOfWeek(st.Now().In(loc))
	first := current.AddDate(0, 0, -7*(weeks-1))

	out := make([]Week, weeks)
	index := map[string]int{}
	for i := range out {
		day := first.AddDate(0, 0, 7*i).Format(time.DateOnly)
		out[i].Start = day
		index[day] = i
	}

	rows, err := st.DB.R.QueryContext(ctx, `
		SELECT s.started_at, s.play_count, s.listened_sec FROM sessions s
		WHERE s.user_id = ? AND `+listened+` AND s.started_at >= ?`, userID, first.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var started, plays, secs int64
		if err := rows.Scan(&started, &plays, &secs); err != nil {
			return nil, err
		}
		key := startOfWeek(time.Unix(started, 0).In(loc)).Format(time.DateOnly)
		if i, ok := index[key]; ok {
			out[i].Plays += plays
			out[i].ListenedSec += secs
		}
	}
	return out, rows.Err()
}
