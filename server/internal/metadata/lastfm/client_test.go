package lastfm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func newClient(t *testing.T, body string, status int) (*Client, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New("key123", "Spindle/test")
	c.BaseURL = srv.URL + "/"
	return c, &seen
}

func TestAlbumLength(t *testing.T) {
	b, _ := os.ReadFile("testdata/album.json")
	c, req := newClient(t, string(b), 200)
	d, err := c.AlbumLength(context.Background(), "Pink Floyd", "The Dark Side of the Moon")
	if err != nil {
		t.Fatal(err)
	}
	if d != 2550*time.Second {
		t.Fatalf("length %v", d)
	}
	q := req.URL.Query()
	if q.Get("method") != "album.getInfo" || q.Get("api_key") != "key123" || q.Get("album") != "The Dark Side of the Moon" {
		t.Fatalf("query %v", q)
	}
}

func TestSingleTrackObject(t *testing.T) {
	c, _ := newClient(t, `{"album":{"tracks":{"track":{"name":"Side Long","duration":1200}}}}`, 200)
	if d, err := c.AlbumLength(context.Background(), "a", "b"); err != nil || d != 20*time.Minute {
		t.Fatalf("d=%v err=%v", d, err)
	}
}

func TestIncompleteDurations(t *testing.T) {
	for _, body := range []string{
		`{"album":{"tracks":{"track":[{"duration":100},{"duration":null}]}}}`,
		`{"album":{"tracks":{"track":[{"duration":100},{"duration":0}]}}}`,
		`{"album":{"tracks":{"track":[]}}}`,
		`{"album":{"tracks":""}}`,
		`{"album":{"tracks":{}}}`,
		`{"album":{"name":"b"}}`,
		`{}`,
		`{"error":6,"message":"Album not found"}`,
	} {
		c, _ := newClient(t, body, 200)
		if d, err := c.AlbumLength(context.Background(), "a", "b"); err != nil || d != 0 {
			t.Errorf("%s: d=%v err=%v", body, d, err)
		}
	}
}

func TestServerError(t *testing.T) {
	for _, status := range []int{500, 503} {
		c, _ := newClient(t, `{"album":{"tracks":{"track":[{"duration":100}]}}}`, status)
		if _, err := c.AlbumLength(context.Background(), "a", "b"); err == nil {
			t.Fatalf("status %d: expected error", status)
		}
	}
}

func TestOneSecondTrackCounts(t *testing.T) {
	c, _ := newClient(t, `{"album":{"tracks":{"track":[{"duration":1},{"duration":100}]}}}`, 200)
	if d, err := c.AlbumLength(context.Background(), "a", "b"); err != nil || d != 101*time.Second {
		t.Fatalf("d=%v err=%v", d, err)
	}
}

func TestUnreadableResponseIsError(t *testing.T) {
	c, _ := newClient(t, `<html>`, 200)
	if _, err := c.AlbumLength(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewClientTimesOut(t *testing.T) {
	if got := New("k", "x").HTTP.Timeout; got != 15*time.Second {
		t.Fatalf("timeout %v", got)
	}
}

func TestNoKey(t *testing.T) {
	if _, err := New("", "x").AlbumLength(context.Background(), "a", "b"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("got %v", err)
	}
}
