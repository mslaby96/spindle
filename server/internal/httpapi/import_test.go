package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/catalog"
)

func TestDiscogsImportFromPreviewToLibrary(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	var preview catalog.ImportSummary
	resp := do(t, c, "POST", e.http.URL+"/api/import/discogs/preview", nil)
	json.NewDecoder(resp.Body).Decode(&preview)
	if resp.StatusCode != http.StatusOK || preview.Found != 3 || preview.NewAlbums != 2 || preview.NewPressings != 3 {
		t.Fatalf("preview %d %+v", resp.StatusCode, preview)
	}
	var idle catalog.ImportProgress
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/import/discogs", nil).Body).Decode(&idle)
	if idle.State != catalog.ImportIdle {
		t.Fatalf("before: %+v", idle)
	}

	stream := openStream(t, c, e.http.URL+"/api/events")
	if resp := do(t, c, "POST", e.http.URL+"/api/import/discogs", nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	for {
		ev, ok := nextNamed(t, stream, catalog.EventImport)
		if !ok {
			t.Fatal("the import never said it was done")
		}
		if strings.Contains(ev.data, `"state":"done"`) {
			if !strings.Contains(ev.data, `"new_albums":2`) {
				t.Fatalf("done: %s", ev.data)
			}
			break
		}
	}

	var untagged []catalog.Album
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/library/untagged", nil).Body).Decode(&untagged)
	if len(untagged) != 2 {
		t.Fatalf("albums waiting for a sleeve: %+v", untagged)
	}
	var again catalog.ImportSummary
	json.NewDecoder(do(t, c, "POST", e.http.URL+"/api/import/discogs/preview", nil).Body).Decode(&again)
	if again.NewAlbums != 0 || again.AlreadyInLibrary != 3 {
		t.Fatalf("preview after the import: %+v", again)
	}
}

func TestAPressingCanBeRemovedAndRestored(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	do(t, c, "POST", e.http.URL+"/api/import/discogs", nil)
	var rel catalog.Release
	for range 200 {
		if r, err := e.srv.Catalog.ReleaseByDiscogsID(t.Context(), 1, 1873014); err == nil {
			rel = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rel.ID == 0 {
		t.Fatal("the import never added the pressing")
	}
	url := e.http.URL + "/api/pressings/" + jsonNumber(rel.ID)
	if resp := do(t, c, "POST", url+"/remove", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", resp.StatusCode)
	}
	var detail struct{ Releases []catalog.Release }
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/albums/"+jsonNumber(rel.AlbumID), nil).Body).Decode(&detail)
	removed := 0
	for _, r := range detail.Releases {
		if r.RemovedAt != nil {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("album detail %+v", detail.Releases)
	}
	if resp := do(t, c, "POST", url+"/restore", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", e.http.URL+"/api/pressings/99999/remove", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown pressing: %d", resp.StatusCode)
	}
}
