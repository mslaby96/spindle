package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func (e *testEnv) catalogued(t *testing.T, c *http.Client, uid string) (albumID, tagID int64) {
	t.Helper()
	var tag struct {
		ID    int64
		Album struct{ ID int64 }
	}
	resp := do(t, c, "POST", e.http.URL+"/api/tags/"+uid+"/assign", map[string]int64{"master_id": 10362})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign: %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&tag)
	return tag.Album.ID, tag.ID
}

func (e *testEnv) libraryIDs(t *testing.T, c *http.Client) []int64 {
	t.Helper()
	var items []struct {
		AlbumID int64 `json:"album_id"`
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/library", nil).Body).Decode(&items)
	ids := []int64{}
	for _, it := range items {
		ids = append(ids, it.AlbumID)
	}
	return ids
}

func TestAlbumLeavesAndReturnsToTheCollection(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id, _ := e.catalogued(t, c, "04A1B2C3")
	url := e.http.URL + "/api/albums/" + strconv.FormatInt(id, 10)

	if resp := do(t, c, "POST", url+"/remove", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", resp.StatusCode)
	}
	if ids := e.libraryIDs(t, c); len(ids) != 0 {
		t.Fatalf("library still has %v", ids)
	}
	var detail struct {
		Album struct {
			RemovedAt *string `json:"removed_at"`
		}
	}
	json.NewDecoder(do(t, c, "GET", url, nil).Body).Decode(&detail)
	if detail.Album.RemovedAt == nil {
		t.Fatal("album page doesn't say the record is gone")
	}

	if resp := do(t, c, "POST", url+"/restore", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
	if ids := e.libraryIDs(t, c); len(ids) != 1 || ids[0] != id {
		t.Fatalf("library %v", ids)
	}
}

func TestDeletingAnAlbum(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id, _ := e.catalogued(t, c, "04A1B2C3")
	url := e.http.URL + "/api/albums/" + strconv.FormatInt(id, 10)

	if resp := do(t, c, "DELETE", url, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp := do(t, c, "GET", url, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("album after delete: %d", resp.StatusCode)
	}
	for _, path := range []string{"", "/remove", "/restore"} {
		method := "POST"
		if path == "" {
			method = "DELETE"
		}
		if resp := do(t, c, method, url+path, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s on a deleted album: %d", method, path, resp.StatusCode)
		}
	}
}

func TestChangingTheAlbumOfASleeveWithListensAsksFirst(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	e.deviceToken(t, c)
	albumID, tagID := e.catalogued(t, c, "04A1B2C3")
	e.srv.DB.W.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, play_count)
		VALUES (1, 1, ?, ?, 'counted', 1, 2, 1)`, tagID, albumID)

	resp := do(t, c, "POST", e.http.URL+"/api/tags/04A1B2C3/assign", map[string]any{"master_id": 99})
	var body struct {
		Error   string
		Listens int
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusConflict || body.Listens != 1 || body.Error == "" {
		t.Fatalf("%d %+v", resp.StatusCode, body)
	}
	if resp := do(t, c, "POST", e.http.URL+"/api/tags/04A1B2C3/assign", map[string]any{"master_id": 99, "listens": "maybe"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown choice: %d", resp.StatusCode)
	}
}
