package inbox

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

type env struct {
	in    *Inbox
	album int64
	tag   int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1), ('b', 'h', 1)`)
	d.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 'stand', 'h', 1)`)
	res, _ := d.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, duration_sec, duration_status, created_at)
		VALUES (1, 1, 'Kind of Blue', 'Miles Davis', 2760, 'resolved', 1)`)
	album, _ := res.LastInsertId()
	res, _ = d.W.Exec(`INSERT INTO tags (user_id, uid, album_id, created_at) VALUES (1, '04A1', ?, 1)`, album)
	tag, _ := res.LastInsertId()
	return &env{in: &Inbox{DB: d, Now: func() time.Time { return time.Unix(5000, 0) }}, album: album, tag: tag}
}

func (v *env) session(t *testing.T, state string, started int64, withAlbum bool) int64 {
	t.Helper()
	var album any
	if withAlbum {
		album = v.album
	}
	res, err := v.in.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec)
		VALUES (1, 1, ?, ?, ?, ?, ?, ?, 1, 2400)`, v.tag, album, state, started, started+2400, started+2400)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (v *env) cards(t *testing.T) []Card {
	t.Helper()
	cards, err := v.in.List(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return cards
}

func half(n int) *int { return &n }

func TestListShowsOpenListensNewestFirst(t *testing.T) {
	v := newEnv(t)
	old := v.session(t, "counted", 100, true)
	pending := v.session(t, "pending_confirmation", 200, true)
	unassigned := v.session(t, "unclassified", 300, false)
	v.session(t, "discarded", 400, true)
	v.session(t, "active", 500, true)
	v.session(t, "unclassified", 600, true)

	cards := v.cards(t)
	if len(cards) != 3 {
		t.Fatalf("cards %+v", cards)
	}
	if cards[0].Session.ID != unassigned || cards[1].Session.ID != pending || cards[2].Session.ID != old {
		t.Fatalf("order %d %d %d", cards[0].Session.ID, cards[1].Session.ID, cards[2].Session.ID)
	}
	if cards[0].Album != nil || cards[0].UID != "04A1" {
		t.Fatalf("unassigned card %+v", cards[0])
	}
	if a := cards[2].Album; a == nil || a.Title != "Kind of Blue" || *a.LengthSec != 2760 || a.Rating != nil {
		t.Fatalf("album %+v", a)
	}
}

func TestReviewClosesCardAndRatesAlbum(t *testing.T) {
	v := newEnv(t)
	id := v.session(t, "counted", 100, true)
	if err := v.in.Review(context.Background(), 1, id, half(9), "  first spin on the new cartridge "); err != nil {
		t.Fatal(err)
	}
	if len(v.cards(t)) != 0 {
		t.Fatal("reviewed card still open")
	}
	var note string
	v.in.DB.R.QueryRow(`SELECT note FROM sessions WHERE id = ?`, id).Scan(&note)
	if note != "first spin on the new cartridge" {
		t.Fatalf("note %q", note)
	}
	r, _ := v.in.AlbumRating(context.Background(), 1, v.album)
	if r == nil || *r != 9 {
		t.Fatalf("album rating %v", r)
	}
}

func TestNewestListenRatingWins(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	monday := v.session(t, "counted", 100, true)
	wednesday := v.session(t, "counted", 300, true)
	v.in.Review(ctx, 1, monday, half(8), "")
	v.in.Review(ctx, 1, wednesday, half(6), "")
	v.in.DB.W.Exec(`UPDATE sessions SET inbox_state = 'open' WHERE id = ?`, monday)
	v.in.Review(ctx, 1, monday, half(10), "")

	r, _ := v.in.AlbumRating(ctx, 1, v.album)
	if *r != 6 {
		t.Fatalf("editing an older listen changed the album rating to %d", *r)
	}
}

func TestReviewValidation(t *testing.T) {
	v := newEnv(t)
	id := v.session(t, "counted", 100, true)
	for _, bad := range []int{0, 11, -1} {
		if err := v.in.Review(context.Background(), 1, id, half(bad), ""); !errors.Is(err, ErrInvalidRating) {
			t.Errorf("rating %d: %v", bad, err)
		}
	}
	if err := v.in.Review(context.Background(), 2, id, half(5), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user: %v", err)
	}
	if err := v.in.Review(context.Background(), 1, id, nil, "no stars, just a note"); err != nil {
		t.Errorf("note only: %v", err)
	}
}

func TestDismissKeepsListen(t *testing.T) {
	v := newEnv(t)
	id := v.session(t, "counted", 100, true)
	if err := v.in.Dismiss(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	var state string
	var plays int
	v.in.DB.R.QueryRow(`SELECT state, play_count FROM sessions WHERE id = ?`, id).Scan(&state, &plays)
	if state != "counted" || plays != 1 || len(v.cards(t)) != 0 {
		t.Fatalf("state %s plays %d", state, plays)
	}
	if err := v.in.Dismiss(context.Background(), 1, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second dismiss: %v", err)
	}
}

func TestDismissUnassignedDeletesListen(t *testing.T) {
	v := newEnv(t)
	id := v.session(t, "unclassified", 100, false)
	if err := v.in.Dismiss(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	var n, tags int
	v.in.DB.R.QueryRow(`SELECT count(*) FROM sessions WHERE id = ?`, id).Scan(&n)
	v.in.DB.R.QueryRow(`SELECT count(*) FROM tags WHERE id = ?`, v.tag).Scan(&tags)
	if n != 0 || tags != 1 {
		t.Fatalf("sessions %d tags %d", n, tags)
	}
}
