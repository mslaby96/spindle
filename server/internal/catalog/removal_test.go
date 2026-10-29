package catalog

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
)

const dsotm, wywh = 10362, 10414

// listen records a finished listen of whatever album the tag points at.
func listen(t *testing.T, s *Service, tag Tag) {
	t.Helper()
	s.DB.W.Exec(`INSERT OR IGNORE INTO devices (id, user_id, name, token_hash, created_at) VALUES (1, 1, 'stand', 'h', 1)`)
	var album any
	if tag.Album != nil {
		album = tag.Album.ID
	}
	if _, err := s.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at,
		play_count, listened_sec, duration_confirmed) VALUES (1, 1, ?, ?, 'counted', 100, 2600, 2600, 1, 2500, 1)`, tag.ID, album); err != nil {
		t.Fatal(err)
	}
}

func sleeve(t *testing.T, s *Service, uid string, master int64) Tag {
	t.Helper()
	tag, err := s.Assign(context.Background(), 1, uid, Assignment{MasterID: master})
	if err != nil {
		t.Fatal(err)
	}
	return tag
}

func count(t *testing.T, s *Service, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRemovingFromTheCollectionKeepsTheHistoryAndFreesTheSleeve(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	tag := sleeve(t, s, "04A1", dsotm)
	listen(t, s, tag)

	if err := s.RemoveFromCollection(ctx, 1, tag.Album.ID); err != nil {
		t.Fatal(err)
	}
	album, err := s.Album(ctx, 1, tag.Album.ID)
	if err != nil || album.RemovedAt == nil {
		t.Fatalf("album %+v %v", album, err)
	}
	if again, _ := s.TagByUID(ctx, 1, "04A1"); again.Album != nil {
		t.Fatalf("sleeve still points at the album: %+v", again)
	}
	if n := count(t, s, `SELECT count(*) FROM sessions WHERE album_id = ?`, album.ID); n != 1 {
		t.Fatalf("listens kept: %d", n)
	}
}

func TestAddingARemovedAlbumAgainBringsItBackWithItsHistory(t *testing.T) {
	ctx := context.Background()
	for name, add := range map[string]func(*Service) (Album, error){
		"master": func(s *Service) (Album, error) { return s.SaveMaster(ctx, 1, dsotm) },
		"pressing of the master": func(s *Service) (Album, error) {
			a, _, err := s.SaveRelease(ctx, 1, 1873013)
			return a, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newService(t)
			tag := sleeve(t, s, "04A1", dsotm)
			listen(t, s, tag)
			s.RemoveFromCollection(ctx, 1, tag.Album.ID)

			back, err := add(s)
			if err != nil || back.ID != tag.Album.ID || back.RemovedAt != nil {
				t.Fatalf("album %+v %v", back, err)
			}
			if stored, _ := s.Album(ctx, 1, back.ID); stored.RemovedAt != nil {
				t.Fatalf("still removed in the library: %+v", stored)
			}
		})
	}
}

func TestAddingARemovedStandalonePressingBringsItBack(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	album, _, _ := s.SaveRelease(ctx, 1, 4455667)
	s.RemoveFromCollection(ctx, 1, album.ID)
	back, _, err := s.SaveRelease(ctx, 1, 4455667)
	if err != nil || back.ID != album.ID || back.RemovedAt != nil {
		t.Fatalf("album %+v %v", back, err)
	}
}

func TestRemovingSomeoneElsesAlbum(t *testing.T) {
	s, _ := newService(t)
	tag := sleeve(t, s, "04A1", dsotm)
	if err := s.RemoveFromCollection(context.Background(), 2, tag.Album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("got %v", err)
	}
}

func TestChangingTheAlbumOfASleeveWithListensAsksWhatToDoWithThem(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	tag := sleeve(t, s, "04A1", dsotm)
	listen(t, s, tag)
	listen(t, s, tag)
	fetched := f.calls["master"]

	_, err := s.Assign(ctx, 1, "04A1", Assignment{MasterID: wywh})
	var choice *ListensChoiceError
	if !errors.As(err, &choice) || choice.Listens != 2 {
		t.Fatalf("got %v", err)
	}
	if n := count(t, s, `SELECT count(*) FROM albums`); n != 1 || f.calls["master"] != fetched {
		t.Fatalf("asking changed something: %d albums, %d fetches", n, f.calls["master"]-fetched)
	}
	if again, _ := s.TagByUID(ctx, 1, "04A1"); again.Album.ID != tag.Album.ID {
		t.Fatalf("sleeve changed before answering: %+v", again)
	}
}

func TestChangingTheAlbumOfASleeveWithoutListensDoesNotAsk(t *testing.T) {
	s, _ := newService(t)
	sleeve(t, s, "04A1", dsotm)
	tag, err := s.Assign(context.Background(), 1, "04A1", Assignment{MasterID: wywh})
	if err != nil || tag.Album.Title != "Wish You Were Here" {
		t.Fatalf("%+v %v", tag, err)
	}
	if n := count(t, s, `SELECT count(*) FROM albums`); n != 1 {
		t.Fatalf("the wrong album was left behind: %d albums", n)
	}
}

func TestMovingListensToTheRightAlbum(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	wrong := sleeve(t, s, "04A1", dsotm)
	listen(t, s, wrong)

	tag, err := s.Assign(ctx, 1, "04A1", Assignment{MasterID: wywh, Listens: MoveListens})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM sessions WHERE album_id = ?`, tag.Album.ID); n != 1 {
		t.Fatalf("listens on the right album: %d", n)
	}
	if _, err := s.Album(ctx, 1, wrong.Album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the wrong album was left behind: %v", err)
	}
}

func TestKeepingListensLeavesThemWithTheOldAlbum(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	old := sleeve(t, s, "04A1", dsotm)
	listen(t, s, old)

	if _, err := s.Assign(ctx, 1, "04A1", Assignment{MasterID: wywh, Listens: KeepListens}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM sessions WHERE album_id = ?`, old.Album.ID); n != 1 {
		t.Fatalf("listens left on the old album: %d", n)
	}
}

func TestDeletingAnAlbumRemovesItsListensPressingsAndCover(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	tag, _ := s.Assign(ctx, 1, "04A1", Assignment{ReleaseID: 1873013})
	listen(t, s, tag)
	var cover string
	s.DB.R.QueryRow(`SELECT cover_path FROM albums WHERE id = ?`, tag.Album.ID).Scan(&cover)

	if err := s.DeleteAlbum(ctx, 1, tag.Album.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Album(ctx, 1, tag.Album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("album still there: %v", err)
	}
	if n := count(t, s, `SELECT count(*) FROM sessions`) + count(t, s, `SELECT count(*) FROM releases`); n != 0 {
		t.Fatalf("%d listens or pressings left", n)
	}
	if again, _ := s.TagByUID(ctx, 1, "04A1"); again.Album != nil || again.Release != nil {
		t.Fatalf("sleeve still assigned: %+v", again)
	}
	if _, err := os.Stat(s.Covers.Path(cover)); !os.IsNotExist(err) {
		t.Fatalf("cover file left behind: %v", err)
	}
	if err := s.DeleteAlbum(ctx, 1, tag.Album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestRestoringPutsTheAlbumBack(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	tag := sleeve(t, s, "04A1", dsotm)
	s.RemoveFromCollection(ctx, 1, tag.Album.ID)
	if err := s.Restore(ctx, 1, tag.Album.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Album(ctx, 1, tag.Album.ID); a.RemovedAt != nil {
		t.Fatalf("still removed: %+v", a)
	}
	if err := s.Restore(ctx, 2, tag.Album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("someone else's album: %v", err)
	}
}

func TestSleevesOfAnAlbum(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	tag := sleeve(t, s, "04A1", dsotm)
	sleeve(t, s, "04B2", dsotm)
	sleeve(t, s, "04C3", wywh)
	uids, err := s.Sleeves(ctx, 1, tag.Album.ID)
	if err != nil || len(uids) != 2 || uids[0] != "04A1" || uids[1] != "04B2" {
		t.Fatalf("%v %v", uids, err)
	}
	if other, _ := s.Sleeves(ctx, 2, tag.Album.ID); len(other) != 0 {
		t.Fatalf("someone else's sleeves: %v", other)
	}
}
