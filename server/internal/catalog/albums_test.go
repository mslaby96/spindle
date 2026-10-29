package catalog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

type fakeDiscogs struct {
	masters  map[int64]discogs.Master
	releases map[int64]discogs.Release
	images   map[string][]byte
	calls    map[string]int
	imageErr error
}

func (f *fakeDiscogs) Master(_ context.Context, id int64) (discogs.Master, error) {
	return lookup(f, "master", f.masters, id)
}

func (f *fakeDiscogs) Release(_ context.Context, id int64) (discogs.Release, error) {
	return lookup(f, "release", f.releases, id)
}

func lookup[T any](f *fakeDiscogs, kind string, known map[int64]T, id int64) (T, error) {
	f.calls[kind]++
	v, ok := known[id]
	if !ok {
		return v, discogs.ErrNotFound
	}
	return v, nil
}

func (f *fakeDiscogs) Image(_ context.Context, url string) ([]byte, error) {
	f.calls["image"]++
	if f.imageErr != nil {
		return nil, f.imageErr
	}
	return f.images[url], nil
}

func redPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{180, 20, 20, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func newService(t *testing.T) (*Service, *fakeDiscogs) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1), ('b', 'h', 1)`)
	f := &fakeDiscogs{
		masters: map[int64]discogs.Master{
			10362: {
				ID: 10362, Title: "The Dark Side Of The Moon", Year: 1973,
				Artists: []discogs.Artist{{Name: "Pink Floyd"}},
				Genres:  []string{"Rock"}, Styles: []string{"Prog Rock", "Psychedelic Rock"},
				Images: []discogs.Image{{Type: "primary", URI: "https://img/dsotm.png"}},
			},
			10414: {
				ID: 10414, Title: "Wish You Were Here", Year: 1975,
				Artists: []discogs.Artist{{Name: "Pink Floyd"}}, Genres: []string{"Rock"},
			},
		},
		releases: map[int64]discogs.Release{
			1873013: {
				ID: 1873013, MasterID: 10362, Title: "The Dark Side Of The Moon", Year: 1973, Country: "UK",
				Labels:  []discogs.Label{{Name: "Harvest", CatNo: "SHVL 804"}},
				Formats: []discogs.Format{{Name: "Vinyl", Descriptions: []string{"LP", "Album"}}},
			},
			1873014: {
				ID: 1873014, MasterID: 10362, Title: "The Dark Side Of The Moon", Year: 2016, Country: "Europe",
				Labels: []discogs.Label{{Name: "Pink Floyd Records", CatNo: "PFRLP8"}},
			},
			4455667: {
				ID: 4455667, Title: "Solo Violin", Year: 1983, Country: "Poland",
				Artists: []discogs.Artist{{Name: "Zbigniew Seifert (2)"}}, Genres: []string{"Jazz"},
				Labels: []discogs.Label{{Name: "Polskie Nagrania Muza", CatNo: "SX 2345"}},
			},
		},
		images: map[string][]byte{"https://img/dsotm.png": redPNG(t)},
		calls:  map[string]int{},
	}
	return &Service{DB: d, Discogs: f, Covers: &covers.Store{Dir: t.TempDir()}}, f
}

func TestSaveMaster(t *testing.T) {
	s, _ := newService(t)
	a, err := s.SaveMaster(context.Background(), 1, 10362)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "The Dark Side Of The Moon" || a.Artist != "Pink Floyd" || *a.Year != 1973 ||
		len(a.Styles) != 2 || *a.MasterID != 10362 {
		t.Fatalf("album %+v", a)
	}
	if !a.HasCover || a.CoverColor != "#b41414" {
		t.Fatalf("cover not stored: %+v", a)
	}
	if a.DurationStatus != "pending" {
		t.Fatalf("duration status %q", a.DurationStatus)
	}
}

func TestSaveMasterIsIdempotent(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	first, _ := s.SaveMaster(ctx, 1, 10362)
	second, err := s.SaveMaster(ctx, 1, 10362)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second save: %+v %v", second, err)
	}
	if f.calls["master"] != 1 {
		t.Fatalf("master fetched %d times", f.calls["master"])
	}
	var n int
	s.DB.R.QueryRow(`SELECT count(*) FROM albums`).Scan(&n)
	if n != 1 {
		t.Fatalf("albums %d", n)
	}
}

func TestSameMasterForDifferentUsers(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a, _ := s.SaveMaster(ctx, 1, 10362)
	b, err := s.SaveMaster(ctx, 2, 10362)
	if err != nil || a.ID == b.ID {
		t.Fatalf("users share an album row: %d %d %v", a.ID, b.ID, err)
	}
	if _, err := s.Album(ctx, 2, a.ID); err == nil {
		t.Fatal("user 2 can read user 1's album")
	}
}

func TestSaveReleaseAttachesToMaster(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	album, rel, err := s.SaveRelease(ctx, 1, 1873013)
	if err != nil {
		t.Fatal(err)
	}
	if album.MasterID == nil || *album.MasterID != 10362 || rel.AlbumID != album.ID {
		t.Fatalf("album %+v release %+v", album, rel)
	}
	if rel.Country != "UK" || rel.Label != "Harvest" || rel.CatNo != "SHVL 804" || rel.Format != "Vinyl, LP, Album" {
		t.Fatalf("release %+v", rel)
	}
	again, rel2, err := s.SaveRelease(ctx, 1, 1873013)
	if err != nil || again.ID != album.ID || rel2.ID != rel.ID {
		t.Fatalf("second save: %+v %+v %v", again, rel2, err)
	}
}

func TestSaveStandaloneRelease(t *testing.T) {
	s, _ := newService(t)
	album, rel, err := s.SaveRelease(context.Background(), 1, 4455667)
	if err != nil {
		t.Fatal(err)
	}
	if album.MasterID != nil || album.ReleaseID == nil || *album.ReleaseID != 4455667 {
		t.Fatalf("album %+v", album)
	}
	if album.Artist != "Zbigniew Seifert" || rel.AlbumID != album.ID {
		t.Fatalf("album %+v release %+v", album, rel)
	}
	if album.HasCover {
		t.Fatal("cover without image")
	}
	var status string
	s.DB.R.QueryRow(`SELECT cover_status FROM albums WHERE id = ?`, album.ID).Scan(&status)
	if status != "no_data" {
		t.Fatalf("cover status %q", status)
	}
}

func TestCoverFailureIsRecorded(t *testing.T) {
	s, f := newService(t)
	f.imageErr = errors.New("network down")
	a, err := s.SaveMaster(context.Background(), 1, 10362)
	if err != nil {
		t.Fatalf("album should still be saved: %v", err)
	}
	var status string
	s.DB.R.QueryRow(`SELECT cover_status FROM albums WHERE id = ?`, a.ID).Scan(&status)
	if status != "failed" || a.HasCover {
		t.Fatalf("status %q album %+v", status, a)
	}
	f.imageErr = nil
	if err := s.ResolveCover(context.Background(), 1, a.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ = s.Album(context.Background(), 1, a.ID); !a.HasCover {
		t.Fatal("retry did not store cover")
	}
}

func TestUnknownMaster(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.SaveMaster(context.Background(), 1, 1); !errors.Is(err, discogs.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestReleasesListsSavedPressings(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	album, _, err := s.SaveRelease(ctx, 1, 1873013)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveRelease(ctx, 1, 1873014); err != nil {
		t.Fatal(err)
	}
	list, err := s.Releases(ctx, 1, album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].CatNo != "SHVL 804" || list[1].CatNo != "PFRLP8" || list[1].Year == nil || *list[1].Year != 2016 {
		t.Fatalf("releases %+v", list)
	}
	if other, err := s.Releases(ctx, 2, album.ID); err != nil || len(other) != 0 {
		t.Fatalf("another user's view: %+v %v", other, err)
	}
}

func TestReleaseWithoutLabelOrYear(t *testing.T) {
	s, f := newService(t)
	f.releases[5550001] = discogs.Release{ID: 5550001, Title: "White Label", Artists: []discogs.Artist{{Name: "Unknown"}}}
	album, rel, err := s.SaveRelease(context.Background(), 1, 5550001)
	if err != nil {
		t.Fatal(err)
	}
	if album.Year != nil || rel.Year != nil || rel.Label != "" || rel.CatNo != "" {
		t.Fatalf("album %+v release %+v", album, rel)
	}
}

func TestEarliestPositiveYearIsKept(t *testing.T) {
	s, f := newService(t)
	f.releases[5550002] = discogs.Release{ID: 5550002, Title: "Ancient", Year: 1}
	album, rel, err := s.SaveRelease(context.Background(), 1, 5550002)
	if err != nil {
		t.Fatal(err)
	}
	if album.Year == nil || *album.Year != 1 || rel.Year == nil || *rel.Year != 1 {
		t.Fatalf("album %+v release %+v", album, rel)
	}
}

func TestSaveMasterDoesNotFetchWhenLibraryLookupFails(t *testing.T) {
	s, f := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SaveMaster(ctx, 1, 10362); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if f.calls["master"] != 0 {
		t.Fatalf("master fetched %d times", f.calls["master"])
	}
}

func TestSaveMasterForUnknownUser(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.SaveMaster(context.Background(), 99, 10362); err == nil {
		t.Fatal("album saved for a user that does not exist")
	}
}
