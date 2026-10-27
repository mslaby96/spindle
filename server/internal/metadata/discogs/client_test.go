package discogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("tok123", "Spindle/test")
	c.BaseURL = srv.URL
	return c
}

func serveFixtures(t *testing.T, seen *http.Request) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r
		}
		w.Header().Set("X-Discogs-Ratelimit", "60")
		w.Header().Set("X-Discogs-Ratelimit-Remaining", "59")
		switch {
		case r.URL.Path == "/database/search":
			w.Write(fixture(t, "search.json"))
		case r.URL.Path == "/masters/10362":
			w.Write(fixture(t, "master.json"))
		case r.URL.Path == "/masters/10362/versions":
			w.Write(fixture(t, "versions.json"))
		case r.URL.Path == "/releases/1873013":
			w.Write(fixture(t, "release.json"))
		default:
			http.NotFound(w, r)
		}
	}
}

func TestSearchFoldsReleasesIntoMasters(t *testing.T) {
	var req http.Request
	c := newTestClient(t, serveFixtures(t, &req))
	results, err := c.Search(context.Background(), "dark side moon")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Discogs token=tok123" {
		t.Errorf("authorization header %q", got)
	}
	if req.Header.Get("User-Agent") != "Spindle/test" || req.URL.Query().Get("q") != "dark side moon" {
		t.Errorf("request %v %v", req.Header, req.URL.Query())
	}
	if len(results) != 3 {
		t.Fatalf("got %d results: %+v", len(results), results)
	}
	dsotm := results[0]
	if dsotm.Kind != "master" || dsotm.ID != 10362 || dsotm.Artist != "Pink Floyd" ||
		dsotm.Title != "The Dark Side Of The Moon" || dsotm.Year != "1973" || dsotm.Thumb == "" {
		t.Errorf("master result %+v", dsotm)
	}
	standalone := results[1]
	if standalone.Kind != "release" || standalone.ID != 4455667 || standalone.Artist != "Zbigniew Seifert" ||
		standalone.Country != "Poland" || standalone.CatNo != "SX 2345" {
		t.Errorf("standalone result %+v", standalone)
	}
	if results[2].Kind != "master" || results[2].ID != 777 {
		t.Errorf("release with unseen master should become that master: %+v", results[2])
	}
}

func TestMaster(t *testing.T) {
	c := newTestClient(t, serveFixtures(t, nil))
	m, err := c.Master(context.Background(), 10362)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "The Dark Side Of The Moon" || m.Year != 1973 || ArtistNames(m.Artists) != "Pink Floyd" {
		t.Errorf("master %+v", m)
	}
	if len(m.Genres) == 0 || len(m.Styles) == 0 || len(m.Tracklist) == 0 || PrimaryImage(m.Images) == "" {
		t.Errorf("missing details %+v", m)
	}
}

func TestReleaseDetails(t *testing.T) {
	c := newTestClient(t, serveFixtures(t, nil))
	r, err := c.Release(context.Background(), 1873013)
	if err != nil {
		t.Fatal(err)
	}
	if r.MasterID != 10362 || r.Country != "UK" || len(r.Labels) == 0 || r.Labels[0].CatNo != "SHVL 804" {
		t.Errorf("release %+v", r)
	}
	if !strings.HasPrefix(r.FormatText(), "Vinyl, LP") {
		t.Errorf("format %q", r.FormatText())
	}
}

func TestVersionsFilters(t *testing.T) {
	var req http.Request
	c := newTestClient(t, serveFixtures(t, &req))
	all, err := c.Versions(context.Background(), 10362, VersionFilter{Country: "UK"})
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	if q.Get("format") != "Vinyl" || q.Get("country") != "UK" {
		t.Errorf("query %v", q)
	}
	if len(all) == 0 {
		t.Fatal("no versions")
	}
	byCat, err := c.Versions(context.Background(), 10362, VersionFilter{CatNo: "shvl-804"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCat) == 0 {
		t.Fatal("catalog number filter dropped matching versions")
	}
	none, _ := c.Versions(context.Background(), 10362, VersionFilter{CatNo: "XYZ 1"})
	if len(none) != 0 {
		t.Fatalf("unexpected match %+v", none)
	}
}

func TestNotFound(t *testing.T) {
	c := newTestClient(t, serveFixtures(t, nil))
	if _, err := c.Master(context.Background(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestRetriesOnceAfter429(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write(fixture(t, "master.json"))
	})
	if _, err := c.Master(context.Background(), 10362); err != nil {
		t.Fatalf("after retry: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestSecond429Fails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := c.Master(context.Background(), 10362); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("got %v", err)
	}
}

func TestServerErrorIsStatusError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	var se *StatusError
	if _, err := c.Master(context.Background(), 1); !errors.As(err, &se) || se.Code != 502 {
		t.Fatalf("got %v", err)
	}
}

func TestMissingTokenIsClearAndSafe(t *testing.T) {
	c := New("", "Spindle/test")
	_, err := c.Search(context.Background(), "x")
	if !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "SPINDLE_DISCOGS_TOKEN") {
		t.Fatalf("got %v", err)
	}
}

func TestRejectedTokenDoesNotLeakIt(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := c.Master(context.Background(), 1)
	if err == nil || strings.Contains(err.Error(), "tok123") {
		t.Fatalf("got %v", err)
	}
}

func TestNames(t *testing.T) {
	if got := CleanName("Nirvana (2)"); got != "Nirvana" {
		t.Errorf("clean %q", got)
	}
	got := ArtistNames([]Artist{{Name: "Simon (3)", Join: "&"}, {Name: "Garfunkel"}})
	if got != "Simon & Garfunkel" {
		t.Errorf("names %q", got)
	}
	if got := ArtistNames([]Artist{{Name: "A", ANV: "Alias", Join: ","}, {Name: "B"}}); got != "Alias, B" {
		t.Errorf("names %q", got)
	}
}

func TestLimiterSpacesRequestsWhenBudgetIsLow(t *testing.T) {
	now := time.Unix(0, 0)
	l := newLimiter()
	l.now = func() time.Time { return now }
	h := http.Header{}
	h.Set("X-Discogs-Ratelimit", "60")
	h.Set("X-Discogs-Ratelimit-Remaining", "30")
	l.update(h)
	if d := l.delay(); d != 0 {
		t.Fatalf("plenty of budget: delay %v", d)
	}
	h.Set("X-Discogs-Ratelimit-Remaining", "3")
	l.update(h)
	if d := l.delay(); d != 0 {
		t.Fatalf("first low-budget request: delay %v", d)
	}
	if d := l.delay(); d != time.Second {
		t.Fatalf("second low-budget request: delay %v", d)
	}
	h.Set("X-Discogs-Ratelimit-Remaining", "0")
	l.update(h)
	if d := l.delay(); d < 2*time.Second {
		t.Fatalf("exhausted budget: delay %v", d)
	}
}
