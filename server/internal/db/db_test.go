package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/mslaby96/spindle/server/internal/migrations"
)

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spindle.db")
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

func TestMigrationsCreateSchema(t *testing.T) {
	d, _ := openTemp(t)
	for _, table := range []string{"users", "login_sessions", "devices", "albums", "releases", "tags", "sessions"} {
		var n int
		err := d.R.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s missing (err=%v)", table, err)
		}
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	d, path := openTemp(t)
	if _, err := d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var n int
	if err := d2.R.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users after reopen: n=%d err=%v", n, err)
	}
}

func TestForeignKeysOnEveryConnection(t *testing.T) {
	d, _ := openTemp(t)
	ctx := context.Background()
	conns := make([]interface{ Close() error }, 0, 4)
	for i := 0; i < 4; i++ {
		c, err := d.R.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		var on int
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
			t.Errorf("read conn %d: foreign_keys=%d err=%v", i, on, err)
		}
	}
	for _, c := range conns {
		c.Close()
	}
	_, err := d.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (999, 'x', 'h', 1)`)
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
}

func TestWritesGoThroughOneConnection(t *testing.T) {
	d, _ := openTemp(t)
	ctx := context.Background()
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := d.W.ExecContext(wait, `INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second writer while a transaction is open: %v", err)
	}
}

func TestLengthsFromDiscogsTracklistsAreLookedUpAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spindle.db")
	raw, err := open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, raw, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	raw.Exec(`INSERT INTO users (id, username, password_hash, created_at) VALUES (1, 'a', 'h', 1)`)
	for i, src := range []string{"discogs_master", "discogs_release", "musicbrainz", "lastfm", "manual"} {
		status := "resolved"
		if src == "manual" {
			status = "manual"
		}
		if _, err := raw.Exec(`INSERT INTO albums (id, user_id, discogs_master_id, title, artist, duration_sec, duration_status,
			duration_source, created_at) VALUES (?, 1, ?, 'x', 'y', 1077, ?, ?, 1)`, i+1, 100+i, status, src); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	d, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	want := map[int]string{1: "pending", 2: "pending", 3: "resolved", 4: "resolved", 5: "manual"}
	for id, status := range want {
		var got string
		var sec int
		d.R.QueryRow(`SELECT duration_status, duration_sec FROM albums WHERE id = ?`, id).Scan(&got, &sec)
		if got != status || sec != 1077 {
			t.Errorf("album %d: %s %d, want %s and the old length kept", id, got, sec, status)
		}
	}
}
