package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

const (
	SessionIdle       = 7 * 24 * time.Hour
	RememberIdle      = 365 * 24 * time.Hour
	touchGranularity  = time.Minute
	setupCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSetupDone          = errors.New("setup already completed")
	ErrInvalidSetupCode   = errors.New("invalid setup code")
	ErrNoSession          = errors.New("no valid session")
)

type Service struct {
	db  *db.DB
	Now func() time.Time

	mu        sync.Mutex
	setupHash []byte
}

func New(d *db.DB) *Service {
	return &Service{db: d, Now: time.Now}
}

func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n == 0, err
}

// PrepareSetup returns a fresh one-time setup code when no account exists yet.
func (s *Service) PrepareSetup(ctx context.Context) (string, error) {
	needed, err := s.SetupNeeded(ctx)
	if err != nil || !needed {
		return "", err
	}
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, len(b))
	for i, v := range b {
		code[i] = setupCodeAlphabet[int(v)%len(setupCodeAlphabet)]
	}
	h := sha256.Sum256(code)
	s.mu.Lock()
	s.setupHash = h[:]
	s.mu.Unlock()
	return string(code), nil
}

func (s *Service) CompleteSetup(ctx context.Context, code, username, password string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupHash == nil {
		return 0, ErrSetupDone
	}
	h := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	if subtle.ConstantTimeCompare(h[:], s.setupHash) != 1 {
		return 0, ErrInvalidSetupCode
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, errors.New("username is required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return 0, err
	}
	id, err := s.createFirstUser(ctx, username, hash)
	if err == nil || errors.Is(err, ErrSetupDone) {
		s.setupHash = nil
	}
	return id, err
}

func (s *Service) createFirstUser(ctx context.Context, username, hash string) (int64, error) {
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return 0, ErrSetupDone
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, hash, s.Now().Unix())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.R.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE username = ?`,
		strings.TrimSpace(username)).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		VerifyPassword(dummyHash, password)
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}

type Session struct {
	UserID     int64
	Persistent bool
	ExpiresAt  time.Time
}

func (s *Service) CreateSession(ctx context.Context, userID int64, persistent bool) (string, Session, error) {
	token, id, err := newToken()
	if err != nil {
		return "", Session{}, err
	}
	now := s.Now()
	exp := now.Add(idleFor(persistent))
	_, err = s.db.W.ExecContext(ctx,
		`INSERT INTO login_sessions (id, user_id, persistent, created_at, last_used_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, persistent, now.Unix(), now.Unix(), exp.Unix())
	if err != nil {
		return "", Session{}, err
	}
	return token, Session{UserID: userID, Persistent: persistent, ExpiresAt: exp}, nil
}

// Lookup validates a session token and slides its expiry forward.
func (s *Service) Lookup(ctx context.Context, token string) (Session, error) {
	id := tokenID(token)
	var sess Session
	var lastUsed, expires int64
	err := s.db.R.QueryRowContext(ctx,
		`SELECT user_id, persistent, last_used_at, expires_at FROM login_sessions WHERE id = ?`, id).
		Scan(&sess.UserID, &sess.Persistent, &lastUsed, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	now := s.Now()
	if now.Unix() >= expires {
		s.db.W.ExecContext(ctx, `DELETE FROM login_sessions WHERE id = ?`, id)
		return Session{}, ErrNoSession
	}
	sess.ExpiresAt = time.Unix(expires, 0)
	if now.Sub(time.Unix(lastUsed, 0)) >= touchGranularity {
		sess.ExpiresAt = now.Add(idleFor(sess.Persistent))
		_, err = s.db.W.ExecContext(ctx, `UPDATE login_sessions SET last_used_at = ?, expires_at = ? WHERE id = ?`,
			now.Unix(), sess.ExpiresAt.Unix(), id)
		if err != nil {
			return Session{}, err
		}
	}
	return sess, nil
}

func (s *Service) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM login_sessions WHERE id = ?`, tokenID(token))
	return err
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, keepToken string) error {
	var hash string
	if err := s.db.R.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(hash, current)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	return s.setPassword(ctx, userID, next, tokenID(keepToken))
}

func (s *Service) ResetPassword(ctx context.Context, username, next string) error {
	var id int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no such user")
	}
	if err != nil {
		return err
	}
	return s.setPassword(ctx, id, next, "")
}

func (s *Service) setPassword(ctx context.Context, userID int64, pw, keepID string) error {
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_sessions WHERE user_id = ? AND id != ?`, userID, keepID); err != nil {
		return err
	}
	return tx.Commit()
}

func idleFor(persistent bool) time.Duration {
	if persistent {
		return RememberIdle
	}
	return SessionIdle
}

func newToken() (token, id string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, tokenID(token), nil
}

func tokenID(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

var dummyHash, _ = HashPassword("timing-equalizer")
