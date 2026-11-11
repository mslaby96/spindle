package sessions

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxAlbumLength is longer than any record a person would leave on; a bigger answer is a typo.
const maxAlbumLength = 6 * time.Hour

var ErrInvalidLength = errors.New("the album's length must be more than zero and at most six hours")

// CorrectLength answers a confirmation with "I heard the whole album, its length is wrong": the album gets the given
// length, this listen counts as one full play, and the album's other listens waiting for a confirmation are judged
// again against the new length, so the ones that fit now count without being asked about.
func (e *Engine) CorrectLength(ctx context.Context, userID, id int64, length time.Duration) error {
	if length <= 0 || length > maxAlbumLength {
		return ErrInvalidLength
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	albumID, err := e.pendingAlbum(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := e.setLength(ctx, userID, albumID, id, length); err != nil {
		return err
	}
	if err := e.rejudge(ctx, userID, albumID); err != nil {
		return err
	}
	e.publish(userID, EventInboxChanged, nil)
	return nil
}

func (e *Engine) pendingAlbum(ctx context.Context, userID, id int64) (int64, error) {
	var state string
	var albumID sql.NullInt64
	err := e.DB.R.QueryRowContext(ctx, `SELECT state, album_id FROM sessions WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&state, &albumID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if State(state) != StatePendingConfirmation || !albumID.Valid {
		return 0, ErrNotPending
	}
	return albumID.Int64, nil
}

// setLength stores the album's length, set by hand, and counts the answered listen as one play, both or neither.
// The listen is recorded as "longer", which credits a whole listen the same way.
func (e *Engine) setLength(ctx context.Context, userID, albumID, id int64, length time.Duration) error {
	tx, err := e.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE albums SET duration_status = 'manual', duration_sec = ?, duration_source = 'manual' WHERE id = ? AND user_id = ?`,
		int64(length/time.Second), albumID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET state = 'counted', play_count = 1, listened_sec = ended_at - started_at,
		duration_confirmed = 1, resolution = ? WHERE id = ?`, ResolutionLonger, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) rejudge(ctx context.Context, userID, albumID int64) error {
	rows, err := e.DB.W.QueryContext(ctx, `UPDATE sessions SET state = 'unclassified', classified_at = NULL
		WHERE user_id = ? AND album_id = ? AND state = 'pending_confirmation' RETURNING id`, userID, albumID)
	if err != nil {
		return err
	}
	var waiting []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		waiting = append(waiting, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range waiting {
		if err := e.classify(ctx, userID, id, false); err != nil {
			return err
		}
	}
	return nil
}
