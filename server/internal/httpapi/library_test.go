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

func TestOneEditChangesTimesRatingAndNote(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id := e.listen(t, "counted", 40)
	url := e.http.URL + "/api/sessions/" + strconv.FormatInt(id, 10)
	start := time.Unix(1000, 0).UTC()
	read := func() (listened int, rating *int, note *string) {
		e.srv.DB.R.QueryRow(`SELECT listened_sec, rating, note FROM sessions WHERE id = ?`, id).Scan(&listened, &rating, &note)
		return
	}

	resp := do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start.Add(50 * time.Minute),
		"review": map[string]any{"rating": 8, "note": "side B twice"}})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("edit: %d", resp.StatusCode)
	}
	if listened, rating, note := read(); listened != 50*60 || rating == nil || *rating != 8 || note == nil || *note != "side B twice" {
		t.Fatalf("after edit: %d %v %v", listened, rating, note)
	}

	// a bad rating saves nothing, not even the times
	resp = do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start.Add(30 * time.Minute),
		"review": map[string]any{"rating": 11, "note": "x"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad rating: %d", resp.StatusCode)
	}
	// and bad times save nothing, not even the review
	resp = do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start,
		"review": map[string]any{"rating": 2, "note": "y"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad times: %d", resp.StatusCode)
	}
	if listened, rating, note := read(); listened != 50*60 || *rating != 8 || *note != "side B twice" {
		t.Fatalf("a refused edit changed something: %d %v %v", listened, *rating, *note)
	}

	// without a review only the times change, as before
	do(t, c, "PUT", url, map[string]any{"started_at": start, "ended_at": start.Add(45 * time.Minute)})
	if listened, rating, _ := read(); listened != 45*60 || *rating != 8 {
		t.Fatalf("times only: %d %v", listened, *rating)
	}
}

func TestEditingOnlyTheReviewLeavesTheTimesAlone(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	id := e.listen(t, "counted", 40)
	e.srv.DB.W.Exec(`UPDATE sessions SET listened_sec = 2435, ended_at = 3435 WHERE id = ?`, id)
	url := e.http.URL + "/api/sessions/" + strconv.FormatInt(id, 10)
	if resp := do(t, c, "PUT", url, map[string]any{"review": map[string]any{"rating": 6, "note": "warm"}}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("review only: %d", resp.StatusCode)
	}
	var listened, ended int
	var rating int
	e.srv.DB.R.QueryRow(`SELECT listened_sec, ended_at, rating FROM sessions WHERE id = ?`, id).Scan(&listened, &ended, &rating)
	if listened != 2435 || ended != 3435 || rating != 6 {
		t.Fatalf("listened %d, ended %d, rating %d", listened, ended, rating)
	}
	if resp := do(t, c, "PUT", url, map[string]any{"started_at": time.Unix(1000, 0)}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a start without an end: %d", resp.StatusCode)
	}
	if resp := do(t, c, "PUT", url, map[string]any{}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("nothing to change: %d", resp.StatusCode)
	}
}
