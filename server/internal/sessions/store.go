package sessions

import (
	"database/sql"
	"strings"
	"time"
)

type Session struct {
	ID                int64      `json:"id"`
	DeviceID          int64      `json:"device_id"`
	TagID             int64      `json:"tag_id"`
	AlbumID           *int64     `json:"album_id,omitempty"`
	ReleaseID         *int64     `json:"release_id,omitempty"`
	State             State      `json:"state"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	PlayCount         int        `json:"play_count"`
	ListenedSec       int64      `json:"listened_sec"`
	DurationConfirmed bool       `json:"duration_confirmed"`
	Resolution        Resolution `json:"resolution,omitempty"`
	Rating            *int       `json:"rating,omitempty"`
	Note              string     `json:"note,omitempty"`
	InboxState        string     `json:"inbox_state"`
}

var columns = []string{"id", "device_id", "tag_id", "album_id", "release_id", "state", "started_at", "ended_at",
	"play_count", "listened_sec", "duration_confirmed", "coalesce(%s.resolution, '')", "rating", "coalesce(%s.note, '')",
	"inbox_state"}

// Columns returns the session columns in the order ScanSession expects, qualified with the given table alias.
func Columns(alias string) string {
	out := make([]string, len(columns))
	for i, c := range columns {
		if strings.Contains(c, "%s") {
			out[i] = strings.ReplaceAll(c, "%s", alias)
		} else {
			out[i] = alias + "." + c
		}
	}
	return strings.Join(out, ", ")
}

type scanner interface{ Scan(...any) error }

// ScanSession reads the session columns in the order of a session SELECT, followed by any extra columns.
func ScanSession(r scanner, extra ...any) (Session, error) {
	var s Session
	var album, release, ended, rating sql.NullInt64
	var started int64
	dest := append([]any{&s.ID, &s.DeviceID, &s.TagID, &album, &release, &s.State, &started, &ended, &s.PlayCount,
		&s.ListenedSec, &s.DurationConfirmed, &s.Resolution, &rating, &s.Note, &s.InboxState}, extra...)
	err := r.Scan(dest...)
	if err != nil {
		return Session{}, err
	}
	s.StartedAt = time.Unix(started, 0).UTC()
	if album.Valid {
		s.AlbumID = &album.Int64
	}
	if release.Valid {
		s.ReleaseID = &release.Int64
	}
	if ended.Valid {
		t := time.Unix(ended.Int64, 0).UTC()
		s.EndedAt = &t
	}
	if rating.Valid {
		v := int(rating.Int64)
		s.Rating = &v
	}
	return s, nil
}
