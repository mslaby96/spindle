package catalog

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNothingToAssign = errors.New("pick an album or a pressing")

type Tag struct {
	ID        int64    `json:"id"`
	UID       string   `json:"uid"`
	Album     *Album   `json:"album,omitempty"`
	Release   *Release `json:"release,omitempty"`
	CreatedAt int64    `json:"created_at"`
}

// EnsureTag returns the user's tag with this UID, creating an unassigned one the first time it's seen.
func (s *Service) EnsureTag(ctx context.Context, userID int64, uid string) (Tag, bool, error) {
	t, err := s.TagByUID(ctx, userID, uid)
	if err == nil {
		return t, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Tag{}, false, err
	}
	_, err = s.DB.W.ExecContext(ctx,
		`INSERT INTO tags (user_id, uid, created_at) VALUES (?, ?, ?) ON CONFLICT (user_id, uid) DO NOTHING`,
		userID, uid, s.now())
	if err != nil {
		return Tag{}, false, err
	}
	t, err = s.TagByUID(ctx, userID, uid)
	return t, true, err
}

func (s *Service) TagByUID(ctx context.Context, userID int64, uid string) (Tag, error) {
	var t Tag
	var albumID, releaseID sql.NullInt64
	err := s.DB.R.QueryRowContext(ctx,
		`SELECT id, uid, album_id, release_id, created_at FROM tags WHERE user_id = ? AND uid = ?`, userID, uid).
		Scan(&t.ID, &t.UID, &albumID, &releaseID, &t.CreatedAt)
	if err != nil {
		return Tag{}, err
	}
	return s.fillTag(ctx, userID, t, albumID, releaseID)
}

func (s *Service) UnassignedTags(ctx context.Context, userID int64) ([]Tag, error) {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT id, uid, created_at FROM tags WHERE user_id = ? AND album_id IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.UID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type Assignment struct {
	MasterID  int64 `json:"master_id"`
	ReleaseID int64 `json:"release_id"`
}

// Assign links a tag to an album, and optionally a pressing. Listens already recorded for this tag but not yet tied
// to an album are attributed to it; listens credited to a previous album keep that album.
func (s *Service) Assign(ctx context.Context, userID int64, uid string, a Assignment) (Tag, error) {
	tag, _, err := s.EnsureTag(ctx, userID, uid)
	if err != nil {
		return Tag{}, err
	}
	var album Album
	var releaseID *int64
	switch {
	case a.ReleaseID != 0:
		var rel Release
		album, rel, err = s.SaveRelease(ctx, userID, a.ReleaseID)
		releaseID = &rel.ID
	case a.MasterID != 0:
		album, err = s.SaveMaster(ctx, userID, a.MasterID)
	default:
		return Tag{}, ErrNothingToAssign
	}
	if err != nil {
		return Tag{}, err
	}

	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return Tag{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE tags SET album_id = ?, release_id = ?, assigned_at = ? WHERE id = ?`,
		album.ID, releaseID, s.now(), tag.ID); err != nil {
		return Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET album_id = ?, release_id = ? WHERE tag_id = ? AND album_id IS NULL`,
		album.ID, releaseID, tag.ID); err != nil {
		return Tag{}, err
	}
	if err := tx.Commit(); err != nil {
		return Tag{}, err
	}
	return s.TagByUID(ctx, userID, uid)
}

func (s *Service) fillTag(ctx context.Context, userID int64, t Tag, albumID, releaseID sql.NullInt64) (Tag, error) {
	if albumID.Valid {
		a, err := s.Album(ctx, userID, albumID.Int64)
		if err != nil {
			return Tag{}, err
		}
		t.Album = &a
	}
	if releaseID.Valid {
		var discogsID int64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT discogs_release_id FROM releases WHERE id = ?`, releaseID.Int64).
			Scan(&discogsID); err != nil {
			return Tag{}, err
		}
		r, err := s.releaseBy(ctx, userID, discogsID)
		if err != nil {
			return Tag{}, err
		}
		t.Release = &r
	}
	return t, nil
}
