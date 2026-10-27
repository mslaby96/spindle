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
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/release/":
			searches++
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
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := c.AlbumLength(context.Background(), "A", "B", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
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
