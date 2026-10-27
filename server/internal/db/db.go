package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/mslaby96/spindle/server/internal/migrations"
)

type DB struct {
	W *sql.DB
	R *sql.DB
}

func Open(ctx context.Context, path string) (*DB, error) {
	w, err := open(path, 1)
	if err != nil {
		return nil, err
	}
	r, err := open(path, 4)
	if err != nil {
		w.Close()
		return nil, err
	}
	d := &DB{W: w, R: r}
	if err := d.migrate(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	rerr := d.R.Close()
	if err := d.W.Close(); err != nil {
		return err
	}
	return rerr
}

func open(path string, maxConns int) (*sql.DB, error) {
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(maxConns)
	return db, nil
}

func (d *DB) migrate(ctx context.Context) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, d.W, migrations.FS)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}
