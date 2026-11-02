package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
