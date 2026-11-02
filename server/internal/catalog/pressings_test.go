package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestRemovingAPressingKeepsItsHistory(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	album, rel, err := s.SaveRelease(ctx, 1, 1873014)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, 1, "04A1B2C3", Assignment{ReleaseID: 1873014}); err != nil {
		t.Fatal(err)
	}
	res, _ := s.DB.W.Exec(`INSERT INTO devices (user_id, name, token_hash, created_at) VALUES (1, 'Stand', 'h', 1)`)
	device, _ := res.LastInsertId()
	tag, _ := s.TagByUID(ctx, 1, "04A1B2C3")
	if _, err := s.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, release_id, state, started_at, last_seen_at)
		VALUES (1, ?, ?, ?, ?, 'counted', 1, 2)`, device, tag.ID, album.ID, rel.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.RemovePressing(ctx, 1, rel.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Releases(ctx, 1, album.ID)
	if len(list) != 1 || list[0].RemovedAt == nil {
		t.Fatalf("the removed pressing is no longer listed as removed: %+v", list)
	}
	if tag, _ := s.TagByUID(ctx, 1, "04A1B2C3"); tag.Album == nil || tag.Album.ID != album.ID || tag.Release != nil {
		t.Fatalf("the sleeve should keep its album and drop the pressing: %+v", tag)
	}
	var kept int64
	s.DB.R.QueryRow(`SELECT release_id FROM sessions`).Scan(&kept)
	if kept != rel.ID {
		t.Fatal("the listen forgot which pressing was played")
	}
	if a, err := s.Album(ctx, 1, album.ID); err != nil || a.RemovedAt != nil {
		t.Fatalf("the album went with its pressing: %+v %v", a, err)
	}

	if err := s.RestorePressing(ctx, 1, rel.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Releases(ctx, 1, album.ID); list[0].RemovedAt != nil {
		t.Fatalf("restored: %+v", list)
	}
}

func TestPressingsOfOthersCannotBeRemoved(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	_, rel, _ := s.SaveRelease(ctx, 1, 1873014)
	if err := s.RemovePressing(ctx, 2, rel.ID); !errors.Is(err, ErrPressingNotFound) {
		t.Fatalf("remove: %v", err)
	}
	if err := s.RestorePressing(ctx, 2, rel.ID); !errors.Is(err, ErrPressingNotFound) {
		t.Fatalf("restore: %v", err)
	}
}

func TestChoosingARemovedPressingAgainBringsItBack(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	_, rel, _ := s.SaveRelease(ctx, 1, 1873014)
	s.RemovePressing(ctx, 1, rel.ID)
	if _, again, err := s.SaveRelease(ctx, 1, 1873014); err != nil || again.RemovedAt != nil {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestImportLeavesRemovedPressingsOut(t *testing.T) {
	im, s, _ := newImporter(t, myCollection)
	ctx := context.Background()
	im.run(ctx, 1, func(int, int) {})
	rel, _ := s.releaseBy(ctx, 1, 1873014)
	s.RemovePressing(ctx, 1, rel.ID)
	sum, err := im.Preview(ctx, 1)
	if err != nil || sum.NewPressings != 0 || sum.SkippedRemovedPressings != 1 || sum.AlreadyInLibrary != 2 {
		t.Fatalf("%+v %v", sum, err)
	}
}
