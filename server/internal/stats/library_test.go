package stats

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type fixture struct {
	st *Stats
	t  *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1), ('b', 'h', 1)`)
	d.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 's', 'h1', 1), (2, 's', 'h2', 1)`)
	return &fixture{st: &Stats{DB: d, Now: func() time.Time { return now }, Loc: time.UTC}, t: t}
}

func (f *fixture) album(user int64, title, artist string, genres, styles string, added time.Time) int64 {
	f.t.Helper()
	res, err := f.st.DB.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, genres, styles, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user, time.Now().UnixNano(), title, artist, genres, styles, added.Unix())
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	f.st.DB.W.Exec(`INSERT INTO tags (user_id, uid, album_id, created_at) VALUES (?, ?, ?, 1)`, user, title, id)
	return id
}

func (f *fixture) listen(user, album int64, state string, at time.Time, minutes, plays int) int64 {
	f.t.Helper()
	var tag int64
	f.st.DB.R.QueryRow(`SELECT id FROM tags WHERE album_id = ?`, album).Scan(&tag)
	res, err := f.st.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, user, user, tag, album, state, at.Unix(), at.Unix()+int64(minutes*60),
		at.Unix()+int64(minutes*60), plays, minutes*60)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestLibraryAggregates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	kob := f.album(1, "Kind of Blue", "Miles Davis", `["Jazz"]`, `["Modal"]`, now.AddDate(-1, 0, 0))
	dsotm := f.album(1, "The Dark Side Of The Moon", "Pink Floyd", `["Rock"]`, `["Prog Rock"]`, now.AddDate(-1, 0, 0))
	f.listen(1, kob, "counted", now.AddDate(0, 0, -3), 45, 1)
	f.listen(1, kob, "pending_confirmation", now.AddDate(0, 0, -1), 180, 1)
	f.listen(1, kob, "discarded", now, 2, 0)
	f.listen(1, dsotm, "counted", now.AddDate(0, 0, -10), 86, 2)
	f.st.DB.W.Exec(`UPDATE sessions SET rating = 9 WHERE album_id = ? AND state = 'counted'`, kob)

	items, err := f.st.Library(ctx, 1, "last_played")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].AlbumID != kob {
		t.Fatalf("items %+v", items)
	}
	k := items[0]
	if k.Plays != 2 || k.ListenedSec != 225*60 || !k.LastPlayedAt.Equal(now.AddDate(0, 0, -1)) || *k.Rating != 9 ||
		!k.AddedAt.Equal(now.AddDate(-1, 0, 0)) {
		t.Fatalf("kind of blue %+v", k)
	}
	byPlays, _ := f.st.Library(ctx, 1, "plays")
	if byPlays[0].Plays != 2 {
		t.Fatalf("by plays %+v", byPlays)
	}
	byTitle, _ := f.st.Library(ctx, 1, "title")
	if byTitle[0].Title != "Kind of Blue" {
		t.Fatalf("by title %+v", byTitle)
	}
}

func TestForgottenShelf(t *testing.T) {
	f := newFixture(t)
	old := f.album(1, "Old Favourite", "A", `[]`, `[]`, now.AddDate(-2, 0, 0))
	recent := f.album(1, "Recent", "B", `[]`, `[]`, now.AddDate(-2, 0, 0))
	never := f.album(1, "Never Played", "C", `[]`, `[]`, now.AddDate(0, -1, 0))
	discardedOnly := f.album(1, "Barely Touched", "D", `[]`, `[]`, now.AddDate(0, -2, 0))
	f.listen(1, old, "counted", now.AddDate(0, -7, 0), 40, 1)
	f.listen(1, recent, "counted", now.AddDate(0, 0, -1), 40, 1)
	f.listen(1, discardedOnly, "discarded", now.AddDate(0, 0, -1), 2, 0)

	got, err := f.st.Forgotten(context.Background(), 1, 180*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NotRecently) != 1 || got.NotRecently[0].AlbumID != old {
		t.Fatalf("not recently %+v", got.NotRecently)
	}
	if len(got.Never) != 2 || got.Never[0].AlbumID != discardedOnly || got.Never[1].AlbumID != never {
		t.Fatalf("never %+v", got.Never)
	}
}

func TestDiary(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	kob := f.album(1, "Kind of Blue", "Miles Davis", `[]`, `[]`, now)
	other := f.album(1, "Other", "X", `[]`, `[]`, now)
	var ids []int64
	for i := 0; i < 5; i++ {
		ids = append(ids, f.listen(1, kob, "counted", now.Add(-time.Duration(i)*time.Hour), 40, 1))
	}
	f.listen(1, kob, "discarded", now.Add(time.Hour), 2, 0)
	f.listen(1, other, "counted", now.Add(-30*time.Minute), 40, 1)
	f.st.DB.W.Exec(`UPDATE sessions SET note = 'late night', rating = 7 WHERE id = ?`, ids[0])

	page, err := f.st.Diary(ctx, 1, DiaryFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 || page[0].SessionID != ids[0] || page[0].Note != "late night" || *page[0].Rating != 7 ||
		!page[0].StartedAt.Equal(now) {
		t.Fatalf("first page %+v", page)
	}
	next, _ := f.st.Diary(ctx, 1, DiaryFilter{Limit: 3, Before: &page[2].StartedAt})
	if len(next) != 3 {
		t.Fatalf("second page %+v", next)
	}
	onlyKob, _ := f.st.Diary(ctx, 1, DiaryFilter{AlbumID: kob})
	if len(onlyKob) != 5 {
		t.Fatalf("album filter %d", len(onlyKob))
	}
}

func TestIsolatedBetweenUsers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	theirs := f.album(2, "Theirs", "B", `[]`, `[]`, now)
	f.listen(2, theirs, "counted", now, 40, 1)

	items, _ := f.st.Library(ctx, 1, "")
	diary, _ := f.st.Diary(ctx, 1, DiaryFilter{})
	forgotten, _ := f.st.Forgotten(ctx, 1, time.Hour)
	if len(items)+len(diary)+len(forgotten.Never)+len(forgotten.NotRecently) != 0 {
		t.Fatalf("user 1 sees user 2's data: %v %v %v", items, diary, forgotten)
	}
	if _, err := f.st.AlbumItem(ctx, 1, theirs); err == nil {
		t.Fatal("user 1 read user 2's album")
	}
}

func TestDiaryPagesThroughListensInTheSameSecond(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	for i := 0; i < 5; i++ {
		f.listen(1, a, "counted", now, 40, 1)
	}
	seen := map[int64]bool{}
	filter := DiaryFilter{Limit: 2}
	for page := 0; page < 5; page++ {
		entries, err := f.st.Diary(context.Background(), 1, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			seen[e.SessionID] = true
		}
		last := entries[len(entries)-1]
		filter.Before, filter.BeforeID = &last.StartedAt, last.SessionID
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d of 5 listens", len(seen))
	}
}

func TestDiaryLimitFallsBackToFiftyOutsideOneToTwoHundred(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	for i := 0; i < 60; i++ {
		f.listen(1, a, "counted", now.Add(-time.Duration(i)*time.Minute), 40, 1)
	}
	for limit, want := range map[int]int{-1: 50, 0: 50, 1: 1, 200: 60, 201: 50} {
		got, err := f.st.Diary(context.Background(), 1, DiaryFilter{Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Fatalf("limit %d returned %d entries, want %d", limit, len(got), want)
		}
	}
}

func TestDiaryBeforeWithoutSessionIDKeepsListensFromThatSecond(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", now, 40, 1)
	f.listen(1, a, "counted", now, 40, 1)
	f.listen(1, a, "counted", now.Add(-time.Hour), 40, 1)
	got, err := f.st.Diary(context.Background(), 1, DiaryFilter{Before: &now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("entries %+v", got)
	}
}

func TestDiaryBeforeFirstSessionIDSkipsTheWholeSecond(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	first := f.listen(1, a, "counted", now, 40, 1)
	f.listen(1, a, "counted", now, 40, 1)
	older := f.listen(1, a, "counted", now.Add(-time.Hour), 40, 1)
	if first != 1 {
		t.Fatalf("first session id %d", first)
	}
	got, err := f.st.Diary(context.Background(), 1, DiaryFilter{Before: &now, BeforeID: first})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != older {
		t.Fatalf("entries %+v", got)
	}
}

func TestRemovedAlbumsLeaveTheShelvesButKeepTheirHistory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sold := f.album(1, "Sold", "A", `["Jazz"]`, `[]`, now.AddDate(-2, 0, 0))
	neverPlayed := f.album(1, "Lost", "B", `[]`, `[]`, now.AddDate(-1, 0, 0))
	f.listen(1, sold, "counted", now.AddDate(0, -7, 0), 40, 1)
	f.st.DB.W.Exec(`UPDATE albums SET removed_at = ? WHERE id IN (?, ?)`, now.Unix(), sold, neverPlayed)

	library, _ := f.st.Library(ctx, 1, "title")
	forgotten, _ := f.st.Forgotten(ctx, 1, 180*24*time.Hour)
	if len(library) != 0 || len(forgotten.NotRecently)+len(forgotten.Never) != 0 {
		t.Fatalf("removed albums still on the shelves: %+v %+v", library, forgotten)
	}

	diary, _ := f.st.Diary(ctx, 1, DiaryFilter{})
	top, _ := f.st.Top(ctx, 1, "all", "plays", 10)
	if len(diary) != 1 || len(top.Albums) != 1 || top.Albums[0].AlbumID != sold || top.Genres[0].Name != "Jazz" {
		t.Fatalf("history lost: diary %+v top %+v", diary, top)
	}
	if item, err := f.st.AlbumItem(ctx, 1, sold); err != nil || item.Plays != 1 {
		t.Fatalf("album page: %+v %v", item, err)
	}
}
