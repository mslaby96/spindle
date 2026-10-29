package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCatalogFlow(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	stream := openStream(t, c, e.http.URL+"/api/events")

	var state struct{ Active bool }
	json.NewDecoder(do(t, c, "PUT", e.http.URL+"/api/catalog/mode", map[string]bool{"active": true}).Body).Decode(&state)
	if !state.Active {
		t.Fatal("mode not active")
	}
	if ev, ok := waitEvent(t, stream); !ok || ev.name != "catalog_mode" {
		t.Fatalf("mode event %+v", ev)
	}
	var current struct{ Active bool }
	if resp := do(t, c, "GET", e.http.URL+"/api/catalog/mode", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("mode: %d", resp.StatusCode)
	} else if json.NewDecoder(resp.Body).Decode(&current); !current.Active {
		t.Fatal("mode not reported active")
	}

	if _, out := e.report(t, token, nil); out.LED != "catalog_ready" {
		t.Fatalf("empty stand in catalog mode: %s", out.LED)
	}
	if _, out := e.report(t, token, "04A1B2C3D4E5F6"); out.LED != "catalog_ready" {
		t.Fatalf("new sleeve: %s", out.LED)
	}
	var tagEvent sseEvent
	for {
		ev, ok := waitEvent(t, stream)
		if !ok {
			t.Fatal("no catalog_tag event")
		}
		if ev.name == "catalog_tag" {
			tagEvent = ev
			break
		}
	}
	if !strings.Contains(tagEvent.data, `"new":true`) || !strings.Contains(tagEvent.data, "04A1B2C3D4E5F6") {
		t.Fatalf("event %s", tagEvent.data)
	}

	var unassigned []map[string]any
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/tags/unassigned", nil).Body).Decode(&unassigned)
	if len(unassigned) != 1 {
		t.Fatalf("unassigned %+v", unassigned)
	}

	resp := do(t, c, "POST", e.http.URL+"/api/tags/04A1B2C3D4E5F6/assign", map[string]int64{"master_id": 10362})
	var tag struct {
		Album struct{ Title string }
	}
	json.NewDecoder(resp.Body).Decode(&tag)
	if resp.StatusCode != http.StatusOK || tag.Album.Title != "The Dark Side Of The Moon" {
		t.Fatalf("assign: %d %+v", resp.StatusCode, tag)
	}
	var saved struct {
		Album struct{ Title string }
	}
	tagResp := do(t, c, "GET", e.http.URL+"/api/tags/04A1B2C3D4E5F6", nil)
	json.NewDecoder(tagResp.Body).Decode(&saved)
	if tagResp.StatusCode != http.StatusOK || saved.Album.Title != "The Dark Side Of The Moon" {
		t.Fatalf("tag lookup: %d %+v", tagResp.StatusCode, saved)
	}
	if _, out := e.report(t, token, "04A1B2C3D4E5F6"); out.LED != "catalog_saved" {
		t.Fatalf("after assign: %s", out.LED)
	}

	do(t, c, "PUT", e.http.URL+"/api/catalog/mode", map[string]bool{"active": false})
	drainEvents(t, stream)
	e.report(t, token, "04A1B2C3D4E5F6")
	for {
		ev, ok := waitEvent(t, stream)
		if !ok {
			break
		}
		if ev.name == "now_playing" && strings.Contains(ev.data, "04A1") {
			t.Fatalf("leftover sleeve reached listening: %s", ev.data)
		}
	}
}

func TestAssignValidation(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	if resp := do(t, c, "POST", e.http.URL+"/api/tags/zz/assign", map[string]int64{"master_id": 1}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad uid: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", e.http.URL+"/api/tags/04A1B2C3/assign", map[string]int64{}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty assignment: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", e.http.URL+"/api/tags/04A1B2C3/assign", map[string]int64{"master_id": 1}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown master: %d", resp.StatusCode)
	}
	if resp := do(t, c, "GET", e.http.URL+"/api/tags/04FFFFFF", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown tag: %d", resp.StatusCode)
	}
}

func drainEvents(t *testing.T, ch <-chan sseEvent) {
	for {
		if _, ok := waitEvent(t, ch); !ok {
			return
		}
	}
}
