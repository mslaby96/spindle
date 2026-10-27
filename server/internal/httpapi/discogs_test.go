package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

func fakeDiscogs(t *testing.T) *httptest.Server {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile("../metadata/discogs/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var cover bytes.Buffer
	png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/database/search":
			w.Write(read("search.json"))
		case "/masters/10362":
			w.Write(read("master.json"))
		case "/masters/10362/versions":
			w.Write(read("versions.json"))
		case "/cover.png":
			w.Write(cover.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscogsSearch(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	var results []discogs.Result
	resp := do(t, c, "GET", e.http.URL+"/api/discogs/search?q=dark+side", nil)
	json.NewDecoder(resp.Body).Decode(&results)
	if resp.StatusCode != http.StatusOK || len(results) == 0 || results[0].ID != 10362 {
		t.Fatalf("status %d results %+v", resp.StatusCode, results)
	}
	var empty []discogs.Result
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/discogs/search?q=d", nil).Body).Decode(&empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("short query: %+v", empty)
	}
	if resp := do(t, e.client(t), "GET", e.http.URL+"/api/discogs/search?q=dark", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous search: %d", resp.StatusCode)
	}
}

func TestDiscogsVersions(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	var versions []discogs.Version
	resp := do(t, c, "GET", e.http.URL+"/api/discogs/masters/10362/versions?country=UK", nil)
	json.NewDecoder(resp.Body).Decode(&versions)
	if resp.StatusCode != http.StatusOK || len(versions) == 0 {
		t.Fatalf("status %d versions %+v", resp.StatusCode, versions)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/discogs/masters/1/versions", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown master: %d", resp.StatusCode)
	}
}

func TestDiscogsWithoutToken(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	e.srv.Discogs.Token = ""
	resp := do(t, c, "GET", e.http.URL+"/api/discogs/search?q=dark", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestCoverIsScopedToOwner(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	ctx := context.Background()
	e.srv.DB.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('other', 'h', 1)`)
	res, _ := e.srv.DB.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, cover_url, created_at)
		VALUES (1, 10362, 'DSOTM', 'Pink Floyd', ?, 1), (2, 10362, 'DSOTM', 'Pink Floyd', ?, 1)`,
		e.srv.Discogs.BaseURL+"/cover.png", e.srv.Discogs.BaseURL+"/cover.png")
	last, _ := res.LastInsertId()
	own, other := last-1, last
	if err := e.srv.Catalog.ResolveCover(ctx, 1, own); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.Catalog.ResolveCover(ctx, 2, other); err != nil {
		t.Fatal(err)
	}
	ownURL := e.http.URL + "/api/albums/" + strconv.FormatInt(own, 10) + "/cover"
	if resp := do(t, c, "GET", ownURL, nil); resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("own cover: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	otherURL := e.http.URL + "/api/albums/" + strconv.FormatInt(other, 10) + "/cover"
	if resp := do(t, c, "GET", otherURL, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other user's cover: %d", resp.StatusCode)
	}
}
