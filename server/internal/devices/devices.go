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

const tokenPrefix = "spd_"

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
	rows, err := s.db.R.QueryContext(ctx,
		`SELECT id, user_id, name, coalesce(fw_version, ''), last_seen_at, created_at
		 FROM devices WHERE user_id = ? AND revoked_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, userID, id int64) error {
	res, err := s.db.W.ExecContext(ctx,
		`UPDATE devices SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		s.Now().Unix(), id, userID)
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
		`SELECT id, user_id, name, coalesce(fw_version, ''), last_seen_at, created_at
		 FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, secrets.Hash(token))
	d, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrInvalidToken
	}
	return d, err
}

func (s *Store) Touch(ctx context.Context, id int64, fwVersion string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET last_seen_at = ?, fw_version = ? WHERE id = ?`,
		s.Now().Unix(), fwVersion, id)
	return err
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Device, error) {
	var d Device
	var lastSeen sql.NullInt64
	var created int64
	if err := r.Scan(&d.ID, &d.UserID, &d.Name, &d.FWVersion, &lastSeen, &created); err != nil {
		return Device{}, err
	}
	d.CreatedAt = time.Unix(created, 0).UTC()
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0).UTC()
		d.LastSeenAt = &t
	}
	return d, nil
}
