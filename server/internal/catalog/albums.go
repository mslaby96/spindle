package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

type Discogs interface {
	Master(ctx context.Context, id int64) (discogs.Master, error)
	Release(ctx context.Context, id int64) (discogs.Release, error)
	Image(ctx context.Context, url string) ([]byte, error)
}

type Album struct {
	ID             int64    `json:"id"`
	MasterID       *int64   `json:"discogs_master_id,omitempty"`
	ReleaseID      *int64   `json:"discogs_release_id,omitempty"`
	Title          string   `json:"title"`
	Artist         string   `json:"artist"`
	Year           *int     `json:"year,omitempty"`
	Genres         []string `json:"genres"`
	Styles         []string `json:"styles"`
	CoverColor     string   `json:"cover_color,omitempty"`
	HasCover       bool     `json:"has_cover"`
	DurationSec    *int     `json:"duration_sec,omitempty"`
	DurationStatus string   `json:"duration_status"`
	// RemovedAt is set once the record left the collection; its listens stay in the diary and stats.
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

type Release struct {
	ID        int64  `json:"id"`
	AlbumID   int64  `json:"album_id"`
	DiscogsID int64  `json:"discogs_release_id"`
	Title     string `json:"title"`
	Country   string `json:"country,omitempty"`
	Year      *int   `json:"year,omitempty"`
	Label     string `json:"label,omitempty"`
	CatNo     string `json:"catno,omitempty"`
	Format    string `json:"format,omitempty"`
}

type Service struct {
	DB      *db.DB
	Discogs Discogs
	Covers  *covers.Store
	Now     func() time.Time
}

func (s *Service) now() int64 {
	if s.Now == nil {
		return time.Now().Unix()
	}
	return s.Now().Unix()
}

// SaveMaster stores the album for a Discogs master, fetching it only if it's not in the library yet.
func (s *Service) SaveMaster(ctx context.Context, userID, masterID int64) (Album, error) {
	if a, err := s.albumBy(ctx, userID, "discogs_master_id", masterID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		return s.keep(ctx, a, err)
	}
	m, err := s.Discogs.Master(ctx, masterID)
	if err != nil {
		return Album{}, err
	}
	id, err := s.insertAlbum(ctx, userID, &masterID, nil, m.Title, discogs.ArtistNames(m.Artists), m.Year,
		m.Genres, m.Styles, discogs.PrimaryImage(m.Images))
	if err != nil {
		return Album{}, err
	}
	s.ResolveCover(ctx, userID, id)
	return s.Album(ctx, userID, id)
}

// SaveRelease stores a specific pressing. Releases that belong to a master are attached to that master's album,
// releases without one become an album of their own.
func (s *Service) SaveRelease(ctx context.Context, userID, releaseID int64) (Album, Release, error) {
	if r, err := s.releaseBy(ctx, userID, releaseID); err == nil {
		a, err := s.Album(ctx, userID, r.AlbumID)
		a, err = s.keep(ctx, a, err)
		return a, r, err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Album{}, Release{}, err
	}
	rel, err := s.Discogs.Release(ctx, releaseID)
	if err != nil {
		return Album{}, Release{}, err
	}

	album, err := s.albumOf(ctx, userID, rel)
	if err != nil {
		return Album{}, Release{}, err
	}

	var label, catno string
	if len(rel.Labels) > 0 {
		label, catno = discogs.CleanName(rel.Labels[0].Name), rel.Labels[0].CatNo
	}
	_, err = s.DB.W.ExecContext(ctx,
		`INSERT INTO releases (user_id, album_id, discogs_release_id, title, country, year, label, catno, format, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, album.ID, releaseID, rel.Title, nullString(rel.Country), nullYear(rel.Year),
		nullString(label), nullString(catno), nullString(rel.FormatText()), s.now())
	if err != nil {
		return Album{}, Release{}, err
	}
	r, err := s.releaseBy(ctx, userID, releaseID)
	return album, r, err
}

// ResolveCover downloads the album's cover if it hasn't been stored yet. Failures are recorded so they can be retried.
func (s *Service) ResolveCover(ctx context.Context, userID, albumID int64) error {
	var url sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `SELECT cover_url FROM albums WHERE id = ? AND user_id = ?`, albumID, userID).Scan(&url)
	if err != nil {
		return err
	}
	if !url.Valid || url.String == "" {
		return s.setCover(ctx, albumID, "no_data", nil, nil)
	}
	data, err := s.Discogs.Image(ctx, url.String)
	if err != nil {
		s.setCover(ctx, albumID, "failed", nil, nil)
		return err
	}
	name, color, err := s.Covers.Save(albumID, data)
	if err != nil {
		s.setCover(ctx, albumID, "failed", nil, nil)
		return err
	}
	return s.setCover(ctx, albumID, "resolved", &name, &color)
}

func (s *Service) setCover(ctx context.Context, albumID int64, status string, path, color *string) error {
	_, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET cover_status = ?, cover_path = ?, cover_color = ? WHERE id = ?`,
		status, path, color, albumID)
	return err
}

func (s *Service) insertAlbum(ctx context.Context, userID int64, masterID, releaseID *int64, title, artist string,
	year int, genres, styles []string, coverURL string) (int64, error) {
	g, _ := json.Marshal(nonNil(genres))
	st, _ := json.Marshal(nonNil(styles))
	res, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO albums (user_id, discogs_master_id, discogs_release_id, title, artist, year, genres, styles, cover_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, masterID, releaseID, title, artist, nullYear(year), string(g), string(st), nullString(coverURL), s.now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const albumColumns = `id, discogs_master_id, discogs_release_id, title, artist, year, genres, styles,
	coalesce(cover_color, ''), cover_path IS NOT NULL, duration_sec, duration_status, removed_at`

func (s *Service) Album(ctx context.Context, userID, id int64) (Album, error) {
	return scanAlbum(s.DB.R.QueryRowContext(ctx,
		`SELECT `+albumColumns+` FROM albums WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Service) albumBy(ctx context.Context, userID int64, column string, value int64) (Album, error) {
	q := `SELECT ` + albumColumns + ` FROM albums WHERE user_id = ? AND ` + column + ` = ?`
	if column == "discogs_release_id" {
		q += ` AND discogs_master_id IS NULL`
	}
	return scanAlbum(s.DB.R.QueryRowContext(ctx, q, userID, value))
}

func (s *Service) releaseBy(ctx context.Context, userID, discogsID int64) (Release, error) {
	var r Release
	var country, label, catno, format sql.NullString
	var year sql.NullInt64
	err := s.DB.R.QueryRowContext(ctx,
		`SELECT id, album_id, discogs_release_id, title, country, year, label, catno, format
		 FROM releases WHERE user_id = ? AND discogs_release_id = ?`, userID, discogsID).
		Scan(&r.ID, &r.AlbumID, &r.DiscogsID, &r.Title, &country, &year, &label, &catno, &format)
	if err != nil {
		return Release{}, err
	}
	r.Country, r.Label, r.CatNo, r.Format = country.String, label.String, catno.String, format.String
	if year.Valid {
		y := int(year.Int64)
		r.Year = &y
	}
	return r, nil
}

type row interface{ Scan(...any) error }

func scanAlbum(r row) (Album, error) {
	var a Album
	var master, release, year, duration, removed sql.NullInt64
	var genres, styles string
	err := r.Scan(&a.ID, &master, &release, &a.Title, &a.Artist, &year, &genres, &styles,
		&a.CoverColor, &a.HasCover, &duration, &a.DurationStatus, &removed)
	if err != nil {
		return Album{}, err
	}
	a.MasterID, a.ReleaseID = ptr64(master), ptr64(release)
	if year.Valid {
		y := int(year.Int64)
		a.Year = &y
	}
	if duration.Valid {
		d := int(duration.Int64)
		a.DurationSec = &d
	}
	if removed.Valid {
		t := time.Unix(removed.Int64, 0).UTC()
		a.RemovedAt = &t
	}
	json.Unmarshal([]byte(genres), &a.Genres)
	json.Unmarshal([]byte(styles), &a.Styles)
	return a, nil
}

func ptr64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullYear(y int) any {
	if y <= 0 {
		return nil
	}
	return y
}

// albumOf returns the album a pressing belongs to: its master, or for a pressing without one, an album of its own.
func (s *Service) albumOf(ctx context.Context, userID int64, rel discogs.Release) (Album, error) {
	if rel.MasterID != 0 {
		return s.SaveMaster(ctx, userID, rel.MasterID)
	}
	album, err := s.albumBy(ctx, userID, "discogs_release_id", rel.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		return s.keep(ctx, album, err)
	}
	id, err := s.insertAlbum(ctx, userID, nil, &rel.ID, rel.Title, discogs.ArtistNames(rel.Artists), rel.Year,
		rel.Genres, rel.Styles, discogs.PrimaryImage(rel.Images))
	if err != nil {
		return Album{}, err
	}
	s.ResolveCover(ctx, userID, id)
	return s.Album(ctx, userID, id)
}

func (s *Service) Releases(ctx context.Context, userID, albumID int64) ([]Release, error) {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT discogs_release_id FROM releases WHERE user_id = ? AND album_id = ? ORDER BY id`, userID, albumID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Release{}
	for _, id := range ids {
		r, err := s.releaseBy(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
