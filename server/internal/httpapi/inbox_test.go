package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func (e *testEnv) listen(t *testing.T, state string, minutes int) int64 {
	t.Helper()
	db := e.srv.DB.W
	db.Exec(`INSERT OR IGNORE INTO devices (id, user_id, name, token_hash, created_at) VALUES (1, 1, 'stand', 'x', 1)`)
	db.Exec(`INSERT OR IGNORE INTO albums (id, user_id, discogs_master_id, title, artist, duration_sec, duration_status, created_at)
		VALUES (1, 1, 1, 'Kind of Blue', 'Miles Davis', 2760, 'resolved', 1)`)
	db.Exec(`INSERT OR IGNORE INTO tags (id, user_id, uid, album_id, created_at) VALUES (1, 1, '04A1B2C3', 1, 1)`)
	res, err := db.Exec(`INSERT INTO sessions (user_id, device_id, tag_id, album_id, state, started_at, last_seen_at, ended_at, play_count, listened_sec, classified_at)
		VALUES (1, 1, 1, 1, ?, 1000, ?, ?, 1, ?, 2000)`, state, 1000+minutes*60, 1000+minutes*60, minutes*60)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestInboxFlow(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	long := e.listen(t, "pending_confirmation", 180)
	normal := e.listen(t, "counted", 40)

	var cards []struct {
		Session struct {
			ID    int64
			State string
		}
		Album struct{ Title string }
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/inbox", nil).Body).Decode(&cards)
	if len(cards) != 2 || cards[0].Album.Title != "Kind of Blue" {
		t.Fatalf("cards %+v", cards)
	}

	base := e.http.URL + "/api/sessions/"
	if resp := do(t, c, "POST", base+strconv.FormatInt(long, 10)+"/resolve", map[string]string{"resolution": "repeated"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("resolve: %d", resp.StatusCode)
	}
	var plays int
	e.srv.DB.R.QueryRow(`SELECT play_count FROM sessions WHERE id = ?`, long).Scan(&plays)
	if plays != 4 {
		t.Fatalf("plays after 'repeated' %d", plays)
	}
	if resp := do(t, c, "POST", base+strconv.FormatInt(long, 10)+"/resolve", map[string]string{"resolution": "forgot"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("resolve twice: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", base+strconv.FormatInt(normal, 10)+"/resolve", map[string]string{"resolution": "bogus"}); resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bogus resolution: %d", resp.StatusCode)
	}

	if resp := do(t, c, "PUT", base+strconv.FormatInt(normal, 10)+"/review", map[string]any{"rating": 9, "note": "late night"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("review: %d", resp.StatusCode)
	}
	if resp := do(t, c, "PUT", base+strconv.FormatInt(long, 10)+"/review", map[string]any{"rating": 11}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid rating: %d", resp.StatusCode)
	}
	if resp := do(t, c, "POST", base+strconv.FormatInt(long, 10)+"/dismiss", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("dismiss: %d", resp.StatusCode)
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/inbox", nil).Body).Decode(&cards)
	if len(cards) != 0 {
		t.Fatalf("inbox after handling: %+v", cards)
	}
}

func TestInboxIsolatedBetweenUsers(t *testing.T) {
	e := newEnv(t)
	e.loggedIn(t)
	id := e.listen(t, "counted", 40)
	e.srv.DB.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('other', 'h', 1)`)
	token, _, _ := e.srv.Auth.CreateSession(t.Context(), 2, false)
	other := e.client(t)
	url := e.http.URL + "/api/sessions/" + strconv.FormatInt(id, 10) + "/dismiss"
	resp := do(t, other, "POST", url, nil, "Cookie", sessionCookie+"="+token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other user dismissed: %d", resp.StatusCode)
	}
	var cards []any
	json.NewDecoder(do(t, other, "GET", e.http.URL+"/api/inbox", nil, "Cookie", sessionCookie+"="+token).Body).Decode(&cards)
	if len(cards) != 0 {
		t.Fatalf("other user sees %+v", cards)
	}
}
