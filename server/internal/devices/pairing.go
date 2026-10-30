package devices

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/secrets"
)

// PairingTTL is how long a pairing code shown in the app can be typed into a stand.
const PairingTTL = 10 * time.Minute

// codeAlphabet leaves out characters that are easy to misread on a phone screen: 0/O, 1/I/L.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

var ErrInvalidCode = errors.New("pairing code is wrong or has expired")

type Pairing struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// StartPairing issues a short code that a stand being set up exchanges for its token.
func (s *Store) StartPairing(ctx context.Context, userID int64, name string) (Pairing, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Pairing{}, ErrNoName
	}
	code, err := newCode()
	if err != nil {
		return Pairing{}, err
	}
	now := s.Now()
	expires := now.Add(PairingTTL).Unix()
	if _, err := s.db.W.ExecContext(ctx, `DELETE FROM device_pairings WHERE expires_at <= ?`, now.Unix()); err != nil {
		return Pairing{}, err
	}
	if _, err := s.db.W.ExecContext(ctx,
		`INSERT INTO device_pairings (code_hash, user_id, name, expires_at) VALUES (?, ?, ?, ?)`,
		secrets.Hash(code), userID, name, expires); err != nil {
		return Pairing{}, err
	}
	return Pairing{Code: code[:3] + "-" + code[3:], ExpiresAt: time.Unix(expires, 0).UTC()}, nil
}

// Pair registers the stand a code was issued for and returns its token. A code works once.
func (s *Store) Pair(ctx context.Context, typed string) (Device, string, error) {
	code, ok := normalizeCode(typed)
	if !ok {
		return Device{}, "", ErrInvalidCode
	}
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, "", err
	}
	defer tx.Rollback()
	var userID int64
	var name string
	err = tx.QueryRowContext(ctx,
		`DELETE FROM device_pairings WHERE code_hash = ? AND expires_at > ? RETURNING user_id, name`,
		secrets.Hash(code), s.Now().Unix()).Scan(&userID, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, "", ErrInvalidCode
	}
	if err != nil {
		return Device{}, "", err
	}
	dev, token, err := s.insert(ctx, tx, userID, name)
	if err != nil {
		return Device{}, "", err
	}
	return dev, token, tx.Commit()
}

func newCode() (string, error) {
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b), nil
}

// normalizeCode accepts a code typed in any case, with or without the dash and spaces.
func normalizeCode(typed string) (string, bool) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(typed))
	if len(code) != 6 {
		return "", false
	}
	for _, c := range code {
		if !strings.ContainsRune(codeAlphabet, c) {
			return "", false
		}
	}
	return code, true
}
