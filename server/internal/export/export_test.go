package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

var warsaw, _ = time.LoadLocation("Europe/Warsaw")

func fixture(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO users (username, password_hash, created_at) VALUES ('maciej', 'h', 1), ('other', 'h', 1)`,
		`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 'Living room', 'h1', 1), (2, 'x', 'h2', 1)`,
		`INSERT INTO albums (id, user_id, discogs_master_id, title, artist, year, genres, styles, duration_sec, duration_status, created_at)
			VALUES (1, 1, 10362, 'Kind of Blue', 'Miles Davis', 1959, '["Jazz"]', '["Modal"]', 2760, 'resolved', 1772000000)`,
		`INSERT INTO albums (id, user_id, discogs_master_id, title, artist, genres, styles, created_at, removed_at)
			VALUES (2, 1, 1, 'Sold, "Rare"', 'Someone', '[]', '[]', 1772000000, 1772500000)`,
		`INSERT INTO albums (id, user_id, discogs_master_id, title, artist, genres, styles, created_at) VALUES (3, 2, 2, 'Not mine', 'X', '[]', '[]', 1)`,
		`INSERT INTO releases (user_id, album_id, discogs_release_id, title, country, year, label, catno, created_at)
			VALUES (1, 1, 5, 'Kind of Blue', 'US', 1959, 'Columbia', 'CL 1355', 1)`,
		`INSERT INTO tags (id, user_id, uid, album_id, created_at) VALUES (1, 1, '04A1', 1, 1), (2, 1, '04B2', NULL, 1), (3, 2, '04C3', 3, 1)`,
		`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec,
			duration_confirmed, rating, note) VALUES (1, 1, 1, 1, 'counted', 1772371800, 1772374560, 1772374560, 1, 2760, 1, 9, 'rainy, evening')`,
		`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec,
			duration_confirmed) VALUES (1, 1, 1, 1, 'discarded', 1772380000, 1772380100, 1772380100, 0, 0, 1)`,
		`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec,
			duration_confirmed) VALUES (2, 2, 3, 3, 'counted', 1772371800, 1772374560, 1772374560, 1, 2760, 1)`,
	} {
		if _, err := d.W.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return d
}

func TestExportHoldsTheWholeCollectionAndHistory(t *testing.T) {
	d := fixture(t)
	now := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	e, err := Build(context.Background(), d, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.Format != "spindle-export" || e.Version != 1 || e.User != "maciej" || !e.ExportedAt.Equal(now) {
		t.Fatalf("header %+v", e)
	}
	if len(e.Albums) != 2 || len(e.Listens) != 1 {
		t.Fatalf("%d albums, %d listens", len(e.Albums), len(e.Listens))
	}
	kob := e.Albums[0]
	if kob.Title != "Kind of Blue" || *kob.Year != 1959 || *kob.LengthSec != 2760 || kob.Genres[0] != "Jazz" ||
		len(kob.Pressings) != 1 || kob.Pressings[0].CatNo != "CL 1355" || len(kob.Sleeves) != 1 || kob.Sleeves[0] != "04A1" {
		t.Fatalf("album %+v", kob)
	}
	if !kob.AddedAt.Equal(time.Unix(1772000000, 0)) {
		t.Fatalf("added %v", kob.AddedAt)
	}
	if r := e.Albums[1].RemovedAt; r == nil || !r.Equal(time.Unix(1772500000, 0)) {
		t.Fatalf("sold record not marked %+v", e.Albums[1])
	}
	l := e.Listens[0]
	if l.AlbumID != 1 || l.Stand != "Living room" || *l.Rating != 4.5 || l.Note != "rainy, evening" || !l.Confirmed ||
		l.ListenedSec != 2760 || !l.StartedAt.Equal(time.Unix(1772371800, 0)) {
		t.Fatalf("listen %+v", l)
	}
	b, _ := json.Marshal(e)
	if bytes.Contains(b, []byte("Not mine")) || bytes.Contains(b, []byte("password")) {
		t.Fatalf("leaks: %s", b)
	}
}

func TestDiaryCSV(t *testing.T) {
	d := fixture(t)
	e, _ := Build(context.Background(), d, 1, time.Now())
	var buf bytes.Buffer
	if err := WriteCSV(&buf, e, warsaw); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"date", "artist", "album", "year", "minutes", "plays", "rating", "note", "length confirmed", "stand"},
		{"2026-03-01 14:30", "Miles Davis", "Kind of Blue", "1959", "46", "1", "4.5", "rainy, evening", "yes", "Living room"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows %q", rows)
	}
	for i := range want {
		for j := range want[i] {
			if rows[i][j] != want[i][j] {
				t.Fatalf("row %d: %q, want %q", i, rows[i], want[i])
			}
		}
	}
}

func TestCSVCellsCannotRunAsFormulas(t *testing.T) {
	e := Export{
		Albums: []Album{{ID: 1, Artist: "@SUM(1+1)", Title: `=HYPERLINK("http://evil","x")`}},
		Listens: []Listen{{AlbumID: 1, Stand: "+cmd", Note: "-2+3", StartedAt: time.Now()},
			{AlbumID: 1, Stand: "\tTab", Note: "fine - really", StartedAt: time.Now()}},
	}
	var buf bytes.Buffer
	WriteCSV(&buf, e, time.UTC)
	rows, _ := csv.NewReader(&buf).ReadAll()
	for _, cell := range []string{rows[1][1], rows[1][2], rows[1][7], rows[1][9], rows[2][9]} {
		if !strings.HasPrefix(cell, "'") {
			t.Errorf("%q could run as a formula", cell)
		}
	}
	if rows[2][7] != "fine - really" {
		t.Errorf("harmless note changed: %q", rows[2][7])
	}
}
