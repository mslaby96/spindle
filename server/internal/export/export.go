// Package export gives a user all of their Spindle data in open formats they can keep or take elsewhere.
package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

// Export is version 1 of the export format. New fields may be added; existing ones keep their meaning.
type Export struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	User       string    `json:"user"`
	Albums     []Album   `json:"albums"`
	Listens    []Listen  `json:"listens"`
}

type Album struct {
	ID               int64      `json:"id"`
	DiscogsMasterID  *int64     `json:"discogs_master_id,omitempty"`
	DiscogsReleaseID *int64     `json:"discogs_release_id,omitempty"`
	Title            string     `json:"title"`
	Artist           string     `json:"artist"`
	Year             *int       `json:"year,omitempty"`
	Genres           []string   `json:"genres"`
	Styles           []string   `json:"styles"`
	LengthSec        *int       `json:"length_sec,omitempty"`
	AddedAt          time.Time  `json:"added_at"`
	RemovedAt        *time.Time `json:"removed_from_collection_at,omitempty"`
	Pressings        []Pressing `json:"pressings"`
	Sleeves          []string   `json:"sleeves"`
}

type Pressing struct {
	DiscogsReleaseID int64  `json:"discogs_release_id"`
	Title            string `json:"title"`
	Country          string `json:"country,omitempty"`
	Year             *int   `json:"year,omitempty"`
	Label            string `json:"label,omitempty"`
	CatNo            string `json:"catno,omitempty"`
	Format           string `json:"format,omitempty"`
}

type Listen struct {
	ID          int64      `json:"id"`
	AlbumID     int64      `json:"album_id"`
	Stand       string     `json:"stand"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	Plays       int        `json:"plays"`
	ListenedSec int64      `json:"listened_sec"`
	Confirmed   bool       `json:"length_confirmed"`
	Rating      *float64   `json:"rating,omitempty"` // in stars, 0.5 to 5
	Note        string     `json:"note,omitempty"`
}

// Build gathers one user's albums, as albums they own or used to, and every listen that counts.
func Build(ctx context.Context, d *db.DB, userID int64, now time.Time) (Export, error) {
	e := Export{Format: "spindle-export", Version: 1, ExportedAt: now, Albums: []Album{}, Listens: []Listen{}}
	if err := d.R.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, userID).Scan(&e.User); err != nil {
		return Export{}, err
	}
	var err error
	if e.Albums, err = albums(ctx, d, userID); err != nil {
		return Export{}, err
	}
	if e.Listens, err = listens(ctx, d, userID); err != nil {
		return Export{}, err
	}
	return e, nil
}

func albums(ctx context.Context, d *db.DB, userID int64) ([]Album, error) {
	rows, err := d.R.QueryContext(ctx, `SELECT id, discogs_master_id, discogs_release_id, title, artist, year, genres, styles,
		duration_sec, created_at, removed_at FROM albums WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		var a Album
		var master, release, year, length, removed sql.NullInt64
		var genres, styles string
		var added int64
		if err := rows.Scan(&a.ID, &master, &release, &a.Title, &a.Artist, &year, &genres, &styles, &length, &added, &removed); err != nil {
			return nil, err
		}
		a.DiscogsMasterID, a.DiscogsReleaseID = ptr(master), ptr(release)
		a.Year, a.LengthSec = intPtr(year), intPtr(length)
		a.AddedAt, a.RemovedAt = time.Unix(added, 0).UTC(), timePtr(removed)
		a.Genres, a.Styles = list(genres), list(styles)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if err := details(ctx, d, userID, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func details(ctx context.Context, d *db.DB, userID int64, a *Album) error {
	rows, err := d.R.QueryContext(ctx, `SELECT discogs_release_id, title, coalesce(country, ''), year, coalesce(label, ''),
		coalesce(catno, ''), coalesce(format, '') FROM releases WHERE user_id = ? AND album_id = ? ORDER BY id`, userID, a.ID)
	if err != nil {
		return err
	}
	a.Pressings = []Pressing{}
	for rows.Next() {
		var p Pressing
		var year sql.NullInt64
		if err := rows.Scan(&p.DiscogsReleaseID, &p.Title, &p.Country, &year, &p.Label, &p.CatNo, &p.Format); err != nil {
			rows.Close()
			return err
		}
		p.Year = intPtr(year)
		a.Pressings = append(a.Pressings, p)
	}
	rows.Close()
	a.Sleeves, err = texts(ctx, d, `SELECT uid FROM tags WHERE user_id = ? AND album_id = ? ORDER BY uid`, userID, a.ID)
	return err
}

func listens(ctx context.Context, d *db.DB, userID int64) ([]Listen, error) {
	rows, err := d.R.QueryContext(ctx, `SELECT s.id, s.album_id, dv.name, s.started_at, s.ended_at, s.play_count, s.listened_sec,
		s.duration_confirmed, s.rating, coalesce(s.note, '') FROM sessions s JOIN devices dv ON dv.id = s.device_id
		WHERE s.user_id = ? AND s.album_id IS NOT NULL AND s.state IN ('counted', 'pending_confirmation')
		ORDER BY s.started_at, s.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Listen{}
	for rows.Next() {
		var l Listen
		var started int64
		var ended, rating sql.NullInt64
		if err := rows.Scan(&l.ID, &l.AlbumID, &l.Stand, &started, &ended, &l.Plays, &l.ListenedSec, &l.Confirmed, &rating, &l.Note); err != nil {
			return nil, err
		}
		l.StartedAt, l.EndedAt = time.Unix(started, 0).UTC(), timePtr(ended)
		if rating.Valid {
			stars := float64(rating.Int64) / 2
			l.Rating = &stars
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// WriteCSV writes the diary, one listen per row, with times in loc so it reads naturally in a spreadsheet.
func WriteCSV(w io.Writer, e Export, loc *time.Location) error {
	byID := map[int64]Album{}
	for _, a := range e.Albums {
		byID[a.ID] = a
	}
	cw := csv.NewWriter(w)
	cw.Write([]string{"date", "artist", "album", "year", "minutes", "plays", "rating", "note", "length confirmed", "stand"})
	for _, l := range e.Listens {
		a := byID[l.AlbumID]
		cw.Write([]string{
			l.StartedAt.In(loc).Format("2006-01-02 15:04"), a.Artist, a.Title, optional(a.Year),
			strconv.FormatInt((l.ListenedSec+30)/60, 10), strconv.Itoa(l.Plays), stars(l.Rating), l.Note, yesNo(l.Confirmed), l.Stand,
		})
	}
	cw.Flush()
	return cw.Error()
}

func texts(ctx context.Context, d *db.DB, query string, args ...any) ([]string, error) {
	rows, err := d.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func list(raw string) []string {
	out := []string{}
	json.Unmarshal([]byte(raw), &out)
	return out
}

func ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func timePtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0).UTC()
	return &t
}

func optional(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

func stars(r *float64) string {
	if r == nil {
		return ""
	}
	return strconv.FormatFloat(*r, 'f', -1, 64)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
