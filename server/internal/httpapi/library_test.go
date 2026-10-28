package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestLibraryEndpoints(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id := e.listen(t, "counted", 40)

	var items []struct {
		Title string
		Plays int
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/library?sort=plays", nil).Body).Decode(&items)
	if len(items) != 1 || items[0].Plays != 1 {
		t.Fatalf("library %+v", items)
	}

	var forgotten struct {
		NotRecently []any `json:"not_recently"`
		Never       []any
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/library/forgotten", nil).Body).Decode(&forgotten)
	if len(forgotten.NotRecently) != 1 {
		t.Fatalf("a listen at unix time 1000 should be forgotten: %+v", forgotten)
	}

	var diary []struct {
		SessionID int64 `json:"session_id"`
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/diary?limit=10", nil).Body).Decode(&diary)
	if len(diary) != 1 || diary[0].SessionID != id {
		t.Fatalf("diary %+v", diary)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/diary?before=yesterday", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad cursor: %d", resp.StatusCode)
	}

	var detail struct {
		Album   struct{ Title string }
		Stats   struct{ Plays int }
		History []any
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/albums/1", nil).Body).Decode(&detail)
	if detail.Album.Title != "Kind of Blue" || detail.Stats.Plays != 1 || len(detail.History) != 1 {
		t.Fatalf("detail %+v", detail)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/albums/99", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing album: %d", resp.StatusCode)
	}
}

func TestEditAndDeleteListen(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id := e.listen(t, "counted", 40)
	url := e.http.URL + "/api/sessions/" + strconv.FormatInt(id, 10)

	start := time.Unix(1000, 0).UTC()
	resp := do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start.Add(55 * time.Minute)})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("edit: %d", resp.StatusCode)
	}
	var listened int
	e.srv.DB.R.QueryRow(`SELECT listened_sec FROM sessions WHERE id = ?`, id).Scan(&listened)
	if listened != 55*60 {
		t.Fatalf("listened %d", listened)
	}
	if resp := do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("zero length: %d", resp.StatusCode)
	}
	if resp := do(t, c, "DELETE", url, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	var items []struct{ Plays int }
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/library", nil).Body).Decode(&items)
	if len(items) != 1 || items[0].Plays != 0 {
		t.Fatalf("library after delete %+v", items)
	}
}

func TestStatsEndpoints(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	e.listen(t, "counted", 40)
	var top struct {
		Summary struct{ Plays int }
		Albums  []struct{ Title string }
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/stats/top?period=all&measure=time", nil).Body).Decode(&top)
	if top.Summary.Plays != 1 || len(top.Albums) != 1 || top.Albums[0].Title != "Kind of Blue" {
		t.Fatalf("top %+v", top)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/stats/top?period=forever", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad period: %d", resp.StatusCode)
	}
	var weeks []struct{ Start string }
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/stats/timeline?weeks=8", nil).Body).Decode(&weeks)
	if len(weeks) != 8 {
		t.Fatalf("weeks %+v", weeks)
	}
}
