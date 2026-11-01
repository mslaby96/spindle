package devices

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/secrets"
)

const (
	tokenPrefix = "spd_"
	// OfflineAfter is three missed heartbeats of an empty stand.
	OfflineAfter = 90 * time.Second
	columns      = `id, user_id, name, coalesce(fw_version, ''), last_seen_at, created_at, rssi, reader_resets, uptime_sec,
		wifi_change_requested_at IS NOT NULL`
)

var (
	ErrNotFound     = errors.New("device not found")
	ErrInvalidToken = errors.New("invalid device token")
	ErrNoName       = errors.New("name is required")
)

type Device struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"-"`
	Name       string     `json:"name"`
	FWVersion  string     `json:"fw_version,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Online     bool       `json:"online"`
	// The diagnostics come from the last report and are missing when the stand's firmware doesn't send them.
	RSSI         *int   `json:"rssi,omitempty"`
	ReaderResets *int   `json:"reader_resets,omitempty"`
	UptimeSec    *int64 `json:"uptime_sec,omitempty"`
	// WiFiChangePending is set until the stand next reports and is told to forget its Wi-Fi.
	WiFiChangePending bool `json:"wifi_change_pending"`
}

// Report is what a stand says about itself each time it reports.
type Report struct {
	FW           string
	RSSI         *int
	ReaderResets *int
	UptimeSec    *int64
}

type Store struct {
	db  *db.DB
	Now func() time.Time
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d, Now: time.Now}
}

// Create registers a device and returns its token; only a hash of the token is stored.
func (s *Store) Create(ctx context.Context, userID int64, name string) (Device, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Device{}, "", ErrNoName
	}
	return s.insert(ctx, s.db.W, userID, name)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) insert(ctx context.Context, db execer, userID int64, name string) (Device, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Device{}, "", err
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	now := s.Now()
	res, err := db.ExecContext(ctx,
		`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		userID, name, secrets.Hash(token), now.Unix())
	if err != nil {
		return Device{}, "", err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Device{}, "", err
	}
	return Device{ID: id, UserID: userID, Name: name, CreatedAt: now.UTC()}, token, nil
}

func (s *Store) List(ctx context.Context, userID int64) ([]Device, error) {
	return s.query(ctx, `SELECT `+columns+` FROM devices WHERE user_id = ? AND revoked_at IS NULL ORDER BY id`, userID)
}

// All lists every user's stands.
func (s *Store) All(ctx context.Context) ([]Device, error) {
	return s.query(ctx, `SELECT `+columns+` FROM devices WHERE revoked_at IS NULL ORDER BY id`)
}

func (s *Store) query(ctx context.Context, query string, args ...any) ([]Device, error) {
	rows, err := s.db.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, userID, id int64) error {
	return s.update(ctx, `UPDATE devices SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		s.Now().Unix(), id, userID)
}

// RequestWiFiChange asks the stand to forget its Wi-Fi, but not its pairing, and open its setup network.
func (s *Store) RequestWiFiChange(ctx context.Context, userID, id int64) error {
	return s.update(ctx, `UPDATE devices SET wifi_change_requested_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		s.Now().Unix(), id, userID)
}

func (s *Store) CancelWiFiChange(ctx context.Context, userID, id int64) error {
	return s.update(ctx, `UPDATE devices SET wifi_change_requested_at = NULL WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		id, userID)
}

// TakeWiFiChange reports whether a change was requested and clears it, so the stand is told only once.
func (s *Store) TakeWiFiChange(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.W.ExecContext(ctx,
		`UPDATE devices SET wifi_change_requested_at = NULL WHERE id = ? AND wifi_change_requested_at IS NOT NULL`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) update(ctx context.Context, query string, args ...any) error {
	res, err := s.db.W.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Authenticate(ctx context.Context, token string) (Device, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return Device{}, ErrInvalidToken
	}
	row := s.db.R.QueryRowContext(ctx,
		`SELECT `+columns+` FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, secrets.Hash(token))
	d, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrInvalidToken
	}
	return d, err
}

// Touch records a report and returns the stand as it is now.
func (s *Store) Touch(ctx context.Context, id int64, r Report) (Device, error) {
	row := s.db.W.QueryRowContext(ctx, `UPDATE devices SET last_seen_at = ?, fw_version = ?, rssi = ?, reader_resets = ?,
		uptime_sec = ? WHERE id = ? RETURNING `+columns, s.Now().Unix(), r.FW, r.RSSI, r.ReaderResets, r.UptimeSec, id)
	return s.scan(row)
}

type scanner interface{ Scan(...any) error }

func (s *Store) scan(r scanner) (Device, error) {
	var d Device
	var lastSeen, rssi, resets, uptime sql.NullInt64
	var created int64
	if err := r.Scan(&d.ID, &d.UserID, &d.Name, &d.FWVersion, &lastSeen, &created, &rssi, &resets, &uptime,
		&d.WiFiChangePending); err != nil {
		return Device{}, err
	}
	d.CreatedAt = time.Unix(created, 0).UTC()
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0).UTC()
		d.LastSeenAt = &t
		d.Online = s.Now().Sub(t) < OfflineAfter
	}
	d.RSSI, d.ReaderResets, d.UptimeSec = intPtr(rssi), intPtr(resets), int64Ptr(uptime)
	return d, nil
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
