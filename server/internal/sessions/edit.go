package sessions

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrInvalidTimes = errors.New("the listen must end after it starts and not in the future")

// Edit corrects when a listen started and ended and credits it again the way its resolution says.
func (e *Engine) Edit(ctx context.Context, userID, id int64, started, ended time.Time) error {
	if !ended.After(started) || ended.After(e.Now().Add(time.Minute)) {
		return ErrInvalidTimes
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var resolution sql.NullString
	var albumSec sql.NullInt64
	err := e.DB.R.QueryRowContext(ctx, `
		SELECT s.resolution, a.duration_sec FROM sessions s LEFT JOIN albums a ON a.id = s.album_id
		WHERE s.id = ? AND s.user_id = ? AND s.state IN ('counted', 'pending_confirmation')`, id, userID).
		Scan(&resolution, &albumSec)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	plays, listened := credit(Resolution(resolution.String), ended.Sub(started), albumSec)
	_, err = e.DB.W.ExecContext(ctx, `
		UPDATE sessions SET started_at = ?, ended_at = ?, last_seen_at = ?, play_count = ?, listened_sec = ? WHERE id = ?`,
		started.Unix(), ended.Unix(), ended.Unix(), plays, int64(listened/time.Second), id)
	if err != nil {
		return err
	}
	e.publish(userID, EventInboxChanged, nil)
	return nil
}

func (e *Engine) Delete(ctx context.Context, userID, id int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	res, err := e.DB.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ? AND state != 'active'`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	e.publish(userID, EventInboxChanged, nil)
	return nil
}
