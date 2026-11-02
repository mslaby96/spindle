package catalog

import (
	"context"
	"errors"
)

var ErrPressingNotFound = errors.New("pressing not found")

// RemovePressing takes a pressing out of the collection. Its album stays, listens of it keep saying which pressing
// they were, and a sleeve pointing at it keeps its album without the pressing. An import leaves it out from then on.
func (s *Service) RemovePressing(ctx context.Context, userID, id int64) error {
	res, err := s.DB.W.ExecContext(ctx,
		`UPDATE releases SET removed_at = coalesce(removed_at, ?) WHERE id = ? AND user_id = ?`, s.now(), id, userID)
	if err := changedOne(res, err); err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE tags SET release_id = NULL WHERE release_id = ? AND user_id = ?`, id, userID)
	return err
}

func (s *Service) RestorePressing(ctx context.Context, userID, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `UPDATE releases SET removed_at = NULL WHERE id = ? AND user_id = ?`, id, userID)
	return changedOne(res, err)
}

type rowsAffected interface{ RowsAffected() (int64, error) }

func changedOne(res rowsAffected, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return errors.Join(err, ErrPressingNotFound)
	}
	return nil
}

// ReleaseByDiscogsID finds a pressing in the user's library by its Discogs id.
func (s *Service) ReleaseByDiscogsID(ctx context.Context, userID, discogsID int64) (Release, error) {
	return s.releaseBy(ctx, userID, discogsID)
}
