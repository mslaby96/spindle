package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("Spindle/test ( test@example.com )")
	c.BaseURL = srv.URL
	c.Interval = 0
	return c
}

func TestAlbumLengthPrefersVinyl(t *testing.T) {
	var queries []string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		switch {
		case r.URL.Path == "/release/":
			queries = append(queries, r.URL.Query().Get("query"))
			w.Write(fixture(t, "search_vinyl.json"))
		case strings.HasPrefix(r.URL.Path, "/release/"):
			if r.URL.Query().Get("inc") != "recordings" {
				t.Errorf("inc %q", r.URL.Query().Get("inc"))
			}
			w.Write(fixture(t, "release_vinyl.json"))
		}
	})
	d, err := c.AlbumLength(context.Background(), "Pink Floyd", "The Dark Side of the Moon", 10)
	if err != nil {
		t.Fatal(err)
	}
	if d != 2578690*time.Millisecond {
		t.Fatalf("length %v", d)
	}
	if len(queries) != 1 || !strings.Contains(queries[0], `format:"12\" Vinyl"`) || !strings.Contains(queries[0], `artist:"Pink Floyd"`) {
		t.Fatalf("queries %q", queries)
	}
}

func TestFallsBackToAnyFormat(t *testing.T) {
	searches := 0
	var fallback string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/release/":
			searches++
			fallback = r.URL.Query().Get("query")
			if searches == 1 {
				w.Write([]byte(`{"releases":[]}`))
				return
			}
			w.Write(fixture(t, "search_any.json"))
		default:
			w.Write([]byte(`{"id":"x","media":[{"format":"CD","tracks":[{"length":1000},{"length":2000}]}]}`))
		}
	})
	d, err := c.AlbumLength(context.Background(), "Pink Floyd", "The Dark Side of the Moon", 0)
	if err != nil || d != 3*time.Second || searches != 2 {
		t.Fatalf("d=%v err=%v searches=%d", d, err, searches)
	}
	if strings.Contains(fallback, "format:") {
		t.Fatalf("fallback search still restricted to vinyl: %q", fallback)
	}
}

func TestNoMatch(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[{"id":"a","score":40,"track-count":10}]}`))
	})
	d, err := c.AlbumLength(context.Background(), "Nobody", "Nothing", 0)
	if err != nil || d != 0 {
		t.Fatalf("d=%v err=%v", d, err)
	}
}

func TestMissingTrackLengthGivesZero(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release/" {
			w.Write([]byte(`{"releases":[{"id":"a","score":100,"track-count":2,"media":[{"format":"12\" Vinyl"}]}]}`))
			return
		}
		w.Write([]byte(`{"id":"a","media":[{"format":"12\" Vinyl","tracks":[{"length":1000},{"length":null}]}]}`))
	})
	d, err := c.AlbumLength(context.Background(), "A", "B", 0)
	if err != nil || d != 0 {
		t.Fatalf("d=%v err=%v", d, err)
	}
}

func TestUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		if _, err := c.AlbumLength(context.Background(), "A", "B", 0); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("status %d: got %v", status, err)
		}
	}
}

func TestClientErrorStatusIsNotUnavailable(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"releases":[]}`))
	})
	if _, err := c.AlbumLength(context.Background(), "A", "B", 0); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestErrorsAfterFirstSearchAreReported(t *testing.T) {
	for name, failing := range map[string]func(searches int, path string) bool{
		"fallback search": func(searches int, path string) bool { return searches == 2 },
		"release lookup":  func(_ int, path string) bool { return path != "/release/" },
	} {
		searches := 0
		c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/release/" {
				searches++
			}
			if failing(searches, r.URL.Path) {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if name == "fallback search" {
				w.Write([]byte(`{"releases":[]}`))
				return
			}
			w.Write(fixture(t, "search_vinyl.json"))
		})
		if _, err := c.AlbumLength(context.Background(), "A", "B", 0); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestPickPrefersVinyl(t *testing.T) {
	vinyl := []Medium{{Format: "12\" Vinyl"}}
	cd := []Medium{{Format: "CD"}}
	rs := []Release{
		{ID: "cd", Score: 100, TrackCount: 10, Media: cd},
		{ID: "lp", Score: 100, TrackCount: 10, Media: vinyl},
		{ID: "bare", Score: 100, TrackCount: 10},
	}
	if r, _ := Pick(rs, 10); r.ID != "lp" {
		t.Errorf("picked %s", r.ID)
	}
	if r, _ := Pick([]Release{rs[0], rs[2]}, 10); r.ID != "cd" {
		t.Errorf("without vinyl picked %s", r.ID)
	}
}

func TestPickBoundaries(t *testing.T) {
	if r, ok := Pick([]Release{{ID: "edge", Score: minScore, TrackCount: 10}}, 0); !ok || r.ID != "edge" {
		t.Errorf("score at threshold: %s %v", r.ID, ok)
	}
	if r, ok := Pick([]Release{{ID: "side-long", Score: 100, TrackCount: 1}}, 0); !ok || r.ID != "side-long" {
		t.Errorf("single track release: %s %v", r.ID, ok)
	}
	rs := []Release{{ID: "one", Score: 100, TrackCount: 1}, {ID: "two-a", Score: 100, TrackCount: 2}, {ID: "two-b", Score: 100, TrackCount: 2}}
	if r, _ := Pick(rs, 1); r.ID != "one" {
		t.Errorf("expected one track: %s", r.ID)
	}
	unknown := []Release{{ID: "u1", Score: 100}, {ID: "u2", Score: 100}, {ID: "lp", Score: 100, TrackCount: 10}}
	if r, ok := Pick(unknown, 0); !ok || r.ID != "lp" {
		t.Errorf("releases without a track count: %s %v", r.ID, ok)
	}
}

func TestPickTieGoesToFewerTracks(t *testing.T) {
	rs := []Release{{ID: "ten", Score: 100, TrackCount: 10}, {ID: "nine", Score: 100, TrackCount: 9}}
	for i := 0; i < 50; i++ {
		if r, _ := Pick(rs, 0); r.ID != "nine" {
			t.Fatalf("tie picked %s", r.ID)
		}
	}
}

func TestDefaultClientRespectsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[]}`))
	}))
	t.Cleanup(srv.Close)
	c := New("Spindle/test")
	c.BaseURL = srv.URL
	if c.HTTP.Timeout != 15*time.Second {
		t.Errorf("timeout %v", c.HTTP.Timeout)
	}
	start := time.Now()
	for i := 0; i < 2; i++ {
		c.search(context.Background(), "a", "b", false)
	}
	if el := time.Since(start); el < time.Second {
		t.Fatalf("two requests took %v", el)
	}
}

func TestPickAvoidsDeluxeEditions(t *testing.T) {
	rs := []Release{
		{ID: "box", Score: 100, TrackCount: 152},
		{ID: "cd1", Score: 100, TrackCount: 9},
		{ID: "cd2", Score: 100, TrackCount: 10},
		{ID: "cd3", Score: 100, TrackCount: 10},
		{ID: "weak", Score: 50, TrackCount: 10},
	}
	if r, _ := Pick(rs, 0); r.ID != "cd2" {
		t.Errorf("mode pick: %s", r.ID)
	}
	if r, _ := Pick(rs, 9); r.ID != "cd1" {
		t.Errorf("expected count pick: %s", r.ID)
	}
	if _, ok := Pick([]Release{{ID: "weak", Score: 50, TrackCount: 10}}, 0); ok {
		t.Error("picked a weak match")
	}
}

func TestThrottleSpacesRequests(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[]}`))
	})
	c.Interval = 150 * time.Millisecond
	start := time.Now()
	for i := 0; i < 3; i++ {
		c.search(context.Background(), "a", "b", false)
	}
	if el := time.Since(start); el < 300*time.Millisecond {
		t.Fatalf("three requests took %v", el)
	}
}

func TestEscapesQuotes(t *testing.T) {
	if got := escape(`12" Single`); got != `12\" Single` {
		t.Fatalf("got %s", got)
	}
}
