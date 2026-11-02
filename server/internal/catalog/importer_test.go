package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

func item(release, master int64, title string) discogs.CollectionItem {
	return discogs.CollectionItem{ReleaseID: release, MasterID: master, Title: title, Artist: "Someone"}
}

func TestPlanGroupsPressingsOfOneAlbum(t *testing.T) {
	p := plan([]discogs.CollectionItem{item(1, 100, "A"), item(2, 100, "A"), item(3, 0, "Solo")}, library{})
	if len(p.newAlbums) != 2 || len(p.newAlbums[0].items) != 2 || p.newAlbums[0].masterID != 100 ||
		p.newAlbums[1].masterID != 0 || p.newAlbums[1].items[0].ReleaseID != 3 {
		t.Fatalf("%+v", p.newAlbums)
	}
	if s := p.summary(); s.Found != 3 || s.NewAlbums != 2 || s.NewPressings != 3 || s.AlreadyInLibrary != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestPlanAddsNewPressingsToAlbumsYouHave(t *testing.T) {
	lib := library{
		masters:  map[int64]libAlbum{100: {id: 7, title: "A"}},
		releases: map[int64]bool{1: true},
	}
	p := plan([]discogs.CollectionItem{item(1, 100, "A"), item(2, 100, "A")}, lib)
	if len(p.newAlbums) != 0 || len(p.pressings) != 1 || p.pressings[0].albumID != 7 || p.pressings[0].item.ReleaseID != 2 {
		t.Fatalf("%+v", p)
	}
	if s := p.summary(); s.NewAlbums != 0 || s.NewPressings != 1 || s.AlreadyInLibrary != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestPlanLeavesOutAlbumsYouRemoved(t *testing.T) {
	lib := library{
		masters:    map[int64]libAlbum{100: {id: 7, title: "Gone", removed: true}},
		standalone: map[int64]libAlbum{3: {id: 8, title: "Also gone", removed: true}},
	}
	p := plan([]discogs.CollectionItem{item(1, 100, "Gone"), item(2, 100, "Gone"), item(3, 0, "Also gone")}, lib)
	if len(p.newAlbums) != 0 || len(p.pressings) != 0 {
		t.Fatalf("%+v", p)
	}
	if s := p.summary(); len(s.SkippedRemoved) != 2 || s.SkippedRemoved[0] != "Gone" || s.SkippedRemoved[1] != "Also gone" {
		t.Fatalf("%+v", s)
	}
}

func TestPlanCountsACopyYouOwnTwiceOnce(t *testing.T) {
	p := plan([]discogs.CollectionItem{item(1, 100, "A"), item(1, 100, "A")}, library{})
	if len(p.newAlbums) != 1 || len(p.newAlbums[0].items) != 1 {
		t.Fatalf("%+v", p.newAlbums)
	}
}

func TestPlanKnowsAStandaloneAlbumYouHave(t *testing.T) {
	lib := library{standalone: map[int64]libAlbum{3: {id: 8, title: "Solo"}}, releases: map[int64]bool{3: true}}
	p := plan([]discogs.CollectionItem{item(3, 0, "Solo")}, lib)
	if len(p.newAlbums) != 0 || len(p.pressings) != 0 || p.summary().AlreadyInLibrary != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestPlanGivesAStandaloneAlbumWithoutItsPressingThePressing(t *testing.T) {
	lib := library{standalone: map[int64]libAlbum{3: {id: 8, title: "Solo"}}}
	p := plan([]discogs.CollectionItem{item(3, 0, "Solo")}, lib)
	if len(p.pressings) != 1 || p.pressings[0].albumID != 8 {
		t.Fatalf("%+v", p)
	}
}

type fakeCollection struct {
	items []discogs.CollectionItem
	err   error
}

func (f *fakeCollection) Identity(context.Context) (string, error) { return "maciej_spins", f.err }

func (f *fakeCollection) Collection(_ context.Context, username string) ([]discogs.CollectionItem, error) {
	if username != "maciej_spins" {
		return nil, fmt.Errorf("asked for %q", username)
	}
	return f.items, f.err
}

var myCollection = []discogs.CollectionItem{
	{ReleaseID: 1873014, MasterID: 10362, Title: "The Dark Side Of The Moon", Artist: "Pink Floyd", Year: 2016,
		Label: "Pink Floyd Records", CatNo: "PFRLP8", Format: "Vinyl, LP, Reissue", CoverURL: "https://img/reissue.jpg"},
	{ReleaseID: 1873013, MasterID: 10362, Title: "The Dark Side Of The Moon", Artist: "Pink Floyd", Year: 1973,
		Label: "Harvest", CatNo: "SHVL 804", Format: "Vinyl, LP, Album"},
	{ReleaseID: 4455667, Title: "Solo Violin", Artist: "Zbigniew Seifert", Year: 1983, Genres: []string{"Jazz"},
		Label: "Polskie Nagrania Muza", CatNo: "SX 2345", CoverURL: "https://img/solo.jpg"},
}

func newImporter(t *testing.T, items []discogs.CollectionItem) (*Importer, *Service, *[]int64) {
	t.Helper()
	s, _ := newService(t)
	var queued []int64
	im := &Importer{
		Service: s, Source: &fakeCollection{items: items},
		Enqueue: func(_, albumID int64) { queued = append(queued, albumID) },
	}
	return im, s, &queued
}

func TestImportAddsTheCollectionAsAlbumsWithTheirPressings(t *testing.T) {
	im, s, queued := newImporter(t, myCollection)
	ctx := context.Background()
	sum, err := im.run(ctx, 1, func(done, total int) {})
	if err != nil {
		t.Fatal(err)
	}
	if sum.NewAlbums != 2 || sum.NewPressings != 3 || sum.Found != 3 {
		t.Fatalf("%+v", sum)
	}
	dsotm, err := s.albumBy(ctx, 1, "discogs_master_id", 10362)
	// the album takes the master's original year and cover, not the reissue's
	if err != nil || dsotm.Year == nil || *dsotm.Year != 1973 || dsotm.Title != "The Dark Side Of The Moon" {
		t.Fatalf("%+v %v", dsotm, err)
	}
	var cover string
	s.DB.R.QueryRow(`SELECT cover_url FROM albums WHERE id = ?`, dsotm.ID).Scan(&cover)
	if cover != "https://img/dsotm.png" {
		t.Fatalf("cover %q", cover)
	}
	r, err := s.releaseBy(ctx, 1, 1873014)
	if err != nil || r.AlbumID != dsotm.ID || r.Label != "Pink Floyd Records" || r.CatNo != "PFRLP8" || *r.Year != 2016 {
		t.Fatalf("pressing %+v %v", r, err)
	}
	solo, err := s.albumBy(ctx, 1, "discogs_release_id", 4455667)
	if err != nil || solo.Artist != "Zbigniew Seifert" || *solo.Year != 1983 {
		t.Fatalf("standalone %+v %v", solo, err)
	}
	if len(*queued) != 2 {
		t.Fatalf("covers and lengths queued for %v", *queued)
	}
	if _, err := s.releaseBy(ctx, 2, 1873014); err == nil {
		t.Fatal("imported into another user's library")
	}
}

func TestImportingAgainAddsNothing(t *testing.T) {
	im, s, _ := newImporter(t, myCollection)
	ctx := context.Background()
	im.run(ctx, 1, func(int, int) {})
	sum, err := im.run(ctx, 1, func(int, int) {})
	if err != nil || sum.NewAlbums != 0 || sum.NewPressings != 0 || sum.AlreadyInLibrary != 3 {
		t.Fatalf("%+v %v", sum, err)
	}
	var n int
	s.DB.R.QueryRow(`SELECT count(*) FROM albums WHERE user_id = 1`).Scan(&n)
	if n != 2 {
		t.Fatalf("%d albums", n)
	}
}

func TestImportKeepsRemovedAlbumsOut(t *testing.T) {
	im, s, _ := newImporter(t, myCollection)
	ctx := context.Background()
	a, _ := s.SaveMaster(ctx, 1, 10362)
	if err := s.RemoveFromCollection(ctx, 1, a.ID); err != nil {
		t.Fatal(err)
	}
	sum, _ := im.run(ctx, 1, func(int, int) {})
	if sum.NewAlbums != 1 || len(sum.SkippedRemoved) != 1 || sum.SkippedRemoved[0] != "The Dark Side Of The Moon" {
		t.Fatalf("%+v", sum)
	}
	if again, _ := s.Album(ctx, 1, a.ID); again.RemovedAt == nil {
		t.Fatal("the removed album came back")
	}
}

func TestImportFallsBackToTheCollectionWhenTheMasterIsGone(t *testing.T) {
	gone := []discogs.CollectionItem{{ReleaseID: 99, MasterID: 777, Title: "Lost Master", Artist: "Nobody", Year: 2001,
		CoverURL: "https://img/lost.jpg"}}
	im, s, _ := newImporter(t, gone)
	if _, err := im.run(context.Background(), 1, func(int, int) {}); err != nil {
		t.Fatal(err)
	}
	a, err := s.albumBy(context.Background(), 1, "discogs_master_id", 777)
	if err != nil || a.Title != "Lost Master" || *a.Year != 2001 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestImportTellsHowFarItGot(t *testing.T) {
	im, _, _ := newImporter(t, myCollection)
	var steps [][2]int
	im.run(context.Background(), 1, func(done, total int) { steps = append(steps, [2]int{done, total}) })
	if len(steps) != 3 || steps[0] != [2]int{0, 2} || steps[2] != [2]int{2, 2} {
		t.Fatalf("%v", steps)
	}
}

func TestImportStopsWhenDiscogsFails(t *testing.T) {
	s, _ := newService(t)
	im := &Importer{Service: s, Source: &fakeCollection{err: errors.New("down")}, Enqueue: func(_, _ int64) {}}
	if _, err := im.run(context.Background(), 1, func(int, int) {}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := im.Preview(context.Background(), 1); err == nil {
		t.Fatal("preview: expected an error")
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	im, s, _ := newImporter(t, myCollection)
	sum, err := im.Preview(context.Background(), 1)
	if err != nil || sum.NewAlbums != 2 {
		t.Fatalf("%+v %v", sum, err)
	}
	var n int
	s.DB.R.QueryRow(`SELECT count(*) FROM albums`).Scan(&n)
	if n != 0 {
		t.Fatalf("preview added %d albums", n)
	}
}

func TestStartRunsTheImportInTheBackgroundAndTellsHowItGoes(t *testing.T) {
	im, _, _ := newImporter(t, myCollection)
	updates := make(chan ImportProgress, 16)
	backedUp := false
	im.Backup = func(context.Context) error { backedUp = true; return nil }
	im.Publish = func(userID int64, p ImportProgress) {
		if userID == 1 {
			updates <- p
		}
	}
	if err := im.Start(1); err != nil {
		t.Fatal(err)
	}
	var last ImportProgress
	for last.State != ImportDone && last.State != ImportFailed {
		select {
		case last = <-updates:
		case <-time.After(5 * time.Second):
			t.Fatal("import never finished")
		}
	}
	if last.State != ImportDone || last.Summary == nil || last.Summary.NewAlbums != 2 || !backedUp {
		t.Fatalf("%+v backed up %v", last, backedUp)
	}
	if got := im.Status(1); got.State != ImportDone {
		t.Fatalf("status %+v", got)
	}
	if got := im.Status(2); got.State != ImportIdle {
		t.Fatalf("someone else's status %+v", got)
	}
}

func TestOnlyOneImportRunsAtATime(t *testing.T) {
	im, _, _ := newImporter(t, myCollection)
	release := make(chan struct{})
	im.Backup = func(context.Context) error { <-release; return nil }
	if err := im.Start(1); err != nil {
		t.Fatal(err)
	}
	if err := im.Start(1); !errors.Is(err, ErrImportRunning) {
		t.Fatalf("second start: %v", err)
	}
	close(release)
}

func TestAFailedBackupStopsTheImport(t *testing.T) {
	im, s, _ := newImporter(t, myCollection)
	done := make(chan ImportProgress, 16)
	im.Backup = func(context.Context) error { return errors.New("disk full") }
	im.Publish = func(_ int64, p ImportProgress) { done <- p }
	im.Start(1)
	var last ImportProgress
	for last.State != ImportFailed && last.State != ImportDone {
		last = <-done
	}
	var n int
	s.DB.R.QueryRow(`SELECT count(*) FROM albums`).Scan(&n)
	if last.State != ImportFailed || !strings.Contains(last.Error, "disk full") || n != 0 {
		t.Fatalf("%+v, %d albums", last, n)
	}
}
