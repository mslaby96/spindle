package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// keep puts an album that is being added again back into the collection, with the history it had.
func (s *Service) keep(ctx context.Context, a Album, err error) (Album, error) {
	if err != nil || a.RemovedAt == nil {
		return a, err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET removed_at = NULL WHERE id = ?`, a.ID); err != nil {
		return Album{}, err
	}
	a.RemovedAt = nil
	return a, nil
}

// RemoveFromCollection is for a record that was sold or lost. Its listens stay in the diary and stats, and its sleeves
// are freed so a sticker that turns up again counts as a new sleeve.
func (s *Service) RemoveFromCollection(ctx context.Context, userID, albumID int64) error {
	if _, err := s.Album(ctx, userID, albumID); err != nil {
		return err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE albums SET removed_at = coalesce(removed_at, ?) WHERE id = ?`, s.now(), albumID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tags SET album_id = NULL, release_id = NULL WHERE album_id = ?`, albumID); err != nil {
		return err
	}
	return tx.Commit()
}

// Restore puts a removed album back into the collection. Its sleeves stay free until assigned again.
func (s *Service) Restore(ctx context.Context, userID, albumID int64) error {
	res, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET removed_at = NULL WHERE id = ? AND user_id = ?`, albumID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteAlbum is for an album that should never have been added: it goes with its listens, pressings and cover.
func (s *Service) DeleteAlbum(ctx context.Context, userID, albumID int64) error {
	var cover sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `SELECT cover_path FROM albums WHERE id = ? AND user_id = ?`, albumID, userID).Scan(&cover)
	if err != nil {
		return err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE album_id = ?`, albumID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, albumID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if cover.Valid {
		return s.Covers.Remove(cover.String)
	}
	return nil
}

// dropIfUnused deletes an album nothing points at any more, like the wrong album a sleeve was first given.
func (s *Service) dropIfUnused(ctx context.Context, userID, albumID int64) error {
	var used bool
	err := s.DB.R.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tags WHERE album_id = ?)
		OR EXISTS (SELECT 1 FROM sessions WHERE album_id = ?)`, albumID, albumID).Scan(&used)
	if err != nil || used {
		return err
	}
	return s.DeleteAlbum(ctx, userID, albumID)
}

// What to do with the listens of a sleeve that is given a different album.
const (
	MoveListens = "move"
	KeepListens = "keep"
)

// ListensChoiceError means the sleeve already has listens of its current album and the caller has to say whether they
// move to the new one.
type ListensChoiceError struct{ Listens int }

func (e *ListensChoiceError) Error() string {
	return fmt.Sprintf("this sleeve has %d listens of its current album; say whether they move to the new one", e.Listens)
}

var ErrUnknownListensChoice = errors.New(`listens must be "move" or "keep"`)

func (s *Service) listensOf(ctx context.Context, tag Tag) (int, error) {
	if tag.Album == nil {
		return 0, nil
	}
	var n int
	err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE tag_id = ? AND album_id = ?
		AND state IN ('counted', 'pending_confirmation')`, tag.ID, tag.Album.ID).Scan(&n)
	return n, err
}

// Sleeves lists the tag UIDs that point at an album.
func (s *Service) Sleeves(ctx context.Context, userID, albumID int64) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT uid FROM tags WHERE user_id = ? AND album_id = ? ORDER BY uid`, userID, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
