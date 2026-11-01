package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/devices"
)

func TestStandPairsWithCodeFromTheApp(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	stream := openStream(t, c, e.http.URL+"/api/events")

	resp := do(t, c, "POST", e.http.URL+"/api/devices/pairing", map[string]string{"name": "Living room"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start pairing: %d", resp.StatusCode)
	}
	var p struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	json.NewDecoder(resp.Body).Decode(&p)
	if len(p.Code) != 7 || time.Until(p.ExpiresAt) < 9*time.Minute {
		t.Fatalf("pairing %+v", p)
	}

	resp = do(t, e.client(t), "POST", e.http.URL+"/api/device/v1/pair",
		map[string]string{"code": strings.ToLower(p.Code), "fw": "0.2.0"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair: %d", resp.StatusCode)
	}
	var paired struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&paired)
	var list []struct {
		Name      string `json:"name"`
		FWVersion string `json:"fw_version"`
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/devices", nil).Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "Living room" || list[0].FWVersion != "0.2.0" {
		t.Fatalf("devices %+v", list)
	}
	if r, _ := e.report(t, paired.Token, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("report with the new token: %d", r.StatusCode)
	}

	select {
	case ev := <-stream:
		if ev.name != devices.EventPaired || !strings.Contains(ev.data, `"name":"Living room"`) {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("app was not told the stand paired")
	}
}

func TestWrongPairingCodesAreRejectedAndRateLimited(t *testing.T) {
	e := newEnv(t)
	statuses := []int{}
	for range 5 {
		resp := do(t, e.client(t), "POST", e.http.URL+"/api/device/v1/pair", map[string]string{"code": "ABC-DEF"})
		statuses = append(statuses, resp.StatusCode)
	}
	if statuses[0] != http.StatusForbidden || statuses[len(statuses)-1] != http.StatusTooManyRequests {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestPairingNeedsAStandName(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	if resp := do(t, c, "POST", e.http.URL+"/api/devices/pairing", map[string]string{"name": " "}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
