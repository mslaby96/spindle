package devices

import (
	"context"
	"database/sql"
	"errors"
)

const (
	UpdateRequested  = "requested"
	UpdateInstalling = "installing"
	UpdateFailed     = "failed"
)

var (
	ErrNoFirmware = errors.New("this server has no firmware to give out")
	ErrUpToDate   = errors.New("the stand already runs this firmware")
)

// Update is a firmware update asked for in the app: requested until the stand next reports while empty, installing
// once it was told, and failed if the stand says the new version didn't take. A stand reporting the new version ends it.
type Update struct {
	Version string `json:"version"`
	State   string `json:"state"`
}

func (s *Store) Get(ctx context.Context, userID, id int64) (Device, error) {
	d, err := s.scan(s.db.R.QueryRowContext(ctx,
		`SELECT `+columns+` FROM devices WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

func (s *Store) RequestUpdate(ctx context.Context, userID, id int64) error {
	if s.Firmware == "" {
		return ErrNoFirmware
	}
	d, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if d.FWVersion == s.Firmware {
		return ErrUpToDate
	}
	if _, err := s.db.W.ExecContext(ctx, `UPDATE devices SET update_target = ?, update_state = ? WHERE id = ?`,
		s.Firmware, UpdateRequested, id); err != nil {
		return err
	}
	s.notify(ctx, id)
	return nil
}

// CancelUpdate drops a request the stand hasn't taken yet, or the note that an update failed. One being installed
// carries on.
func (s *Store) CancelUpdate(ctx context.Context, userID, id int64) error {
	err := s.update(ctx, `UPDATE devices
		SET update_target = CASE WHEN update_state = 'installing' THEN update_target END,
			update_state = CASE WHEN update_state = 'installing' THEN update_state END
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID)
	if err == nil {
		s.notify(ctx, id)
	}
	return err
}

// TakeUpdate returns the version to install if one was requested, and marks it as being installed.
func (s *Store) TakeUpdate(ctx context.Context, id int64) (string, bool, error) {
	var version string
	err := s.db.W.QueryRowContext(ctx, `UPDATE devices SET update_state = ? WHERE id = ? AND update_state = ?
		RETURNING update_target`, UpdateInstalling, id, UpdateRequested).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	s.notify(ctx, id)
	return version, true, nil
}

// Settle ends an update when the stand reports the new version, or marks it failed when the stand says so.
func (s *Store) Settle(ctx context.Context, id int64, fw, failed string) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE devices
		SET update_state = CASE WHEN update_target = ?1 THEN NULL ELSE 'failed' END,
			update_target = CASE WHEN update_target = ?1 THEN NULL ELSE update_target END
		WHERE id = ?3 AND ((update_target = ?1 AND update_state IN ('requested', 'installing'))
			OR (update_target = ?2 AND update_state = 'installing'))`, fw, failed, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.notify(ctx, id)
	}
	return nil
}

func (s *Store) notify(ctx context.Context, id int64) {
	if s.Notify == nil {
		return
	}
	if d, err := s.scan(s.db.R.QueryRowContext(ctx, `SELECT `+columns+` FROM devices WHERE id = ?`, id)); err == nil {
		s.Notify(d)
	}
}
