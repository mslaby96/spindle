package sessions

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidTimes = errors.New("the listen must end after it starts and not in the future")

// Edit corrects when a listen started and ended and recomputes the time listened.
func (e *Engine) Edit(ctx context.Context, userID, id int64, started, ended time.Time) error {
	if !ended.After(started) || ended.After(e.Now().Add(time.Minute)) {
		return ErrInvalidTimes
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	res, err := e.DB.W.ExecContext(ctx, `
		UPDATE sessions SET started_at = ?, ended_at = ?, last_seen_at = ?, listened_sec = ?
		WHERE id = ? AND user_id = ? AND state IN ('counted', 'pending_confirmation')`,
		started.Unix(), ended.Unix(), ended.Unix(), int64(ended.Sub(started)/time.Second), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
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
