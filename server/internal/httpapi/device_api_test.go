package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (e *testEnv) deviceToken(t *testing.T, c *http.Client) string {
	t.Helper()
	var created struct{ Token string }
	json.NewDecoder(do(t, c, "POST", e.http.URL+"/api/devices", map[string]string{"name": "Stand"}).Body).Decode(&created)
	return created.Token
}

func (e *testEnv) report(t *testing.T, token string, uid any) (*http.Response, reportResponse) {
	t.Helper()
	resp := do(t, e.client(t), "POST", e.http.URL+"/api/device/v1/report",
		map[string]any{"uid": uid, "fw": "0.1.0", "seq": 1}, "Authorization", "Bearer "+token)
	var out reportResponse
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

type sseEvent struct{ name, data string }

func openStream(t *testing.T, c *http.Client, url string) <-chan sseEvent {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	out := make(chan sseEvent, 8)
	ready := make(chan struct{})
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ": connected"):
				close(ready)
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			case line == "" && ev.name != "":
				out <- ev
				ev = sseEvent{}
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not connect")
	}
	return out
}

func waitEvent(t *testing.T, ch <-chan sseEvent) (sseEvent, bool) {
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(300 * time.Millisecond):
		return sseEvent{}, false
	}
}

func TestReportPublishesToStream(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	stream := openStream(t, c, e.http.URL+"/api/events")

	resp, out := e.report(t, token, "04:a1:b2:c3:d4:e5:f6")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	if out.LED != "unknown" || out.IntervalMS != 1500 {
		t.Fatalf("response %+v", out)
	}
	ev, ok := waitEvent(t, stream)
	if !ok || ev.name != "now_playing" || !strings.Contains(ev.data, `"uid":"04A1B2C3D4E5F6"`) ||
		!strings.Contains(ev.data, `"active":true`) {
		t.Fatalf("event %+v ok=%v", ev, ok)
	}

	e.report(t, token, "04A1B2C3D4E5F6")
	if ev, ok := waitEvent(t, stream); ok {
		t.Fatalf("unchanged tag published again: %+v", ev)
	}

	var current []struct {
		UID    string
		Active bool
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/now-playing", nil).Body).Decode(&current)
	if len(current) != 1 || current[0].UID != "04A1B2C3D4E5F6" || !current[0].Active {
		t.Fatalf("now playing %+v", current)
	}
}

func TestReportEmptyIsIdle(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	resp, out := e.report(t, token, nil)
	if resp.StatusCode != http.StatusOK || out.LED != "idle" {
		t.Fatalf("status %d response %+v", resp.StatusCode, out)
	}
}

func TestReportRejectsBadUID(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	stream := openStream(t, c, e.http.URL+"/api/events")
	for _, uid := range []string{"zz", "0102", "0102030405060708090A0B", "not hex"} {
		if resp, _ := e.report(t, token, uid); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("uid %q: %d", uid, resp.StatusCode)
		}
	}
	if ev, ok := waitEvent(t, stream); ok {
		t.Fatalf("event published for invalid uid: %+v", ev)
	}
}

func TestReportUpdatesLastSeen(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	e.report(t, token, nil)
	var list []struct {
		FWVersion  string     `json:"fw_version"`
		LastSeenAt *time.Time `json:"last_seen_at"`
	}
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/devices", nil).Body).Decode(&list)
	if len(list) != 1 || list[0].FWVersion != "0.1.0" || list[0].LastSeenAt == nil {
		t.Fatalf("device after report: %+v", list)
	}
}

func TestCredentialScopes(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)

	cookieOnDevice := do(t, c, "POST", e.http.URL+"/api/device/v1/report", map[string]any{"uid": nil})
	if cookieOnDevice.StatusCode != http.StatusUnauthorized {
		t.Errorf("login cookie on device endpoint: %d", cookieOnDevice.StatusCode)
	}
	tokenOnEvents := do(t, e.client(t), "GET", e.http.URL+"/api/events", nil, "Authorization", "Bearer "+token)
	if tokenOnEvents.StatusCode != http.StatusUnauthorized {
		t.Errorf("device token on events: %d", tokenOnEvents.StatusCode)
	}
	if resp, _ := e.report(t, "spd_invalid", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid device token: %d", resp.StatusCode)
	}
}

func TestNormalizeUID(t *testing.T) {
	cases := map[string]string{
		"04a1b2c3":             "04A1B2C3",
		"04:A1:B2:C3:D4:E5:F6": "04A1B2C3D4E5F6",
		" 04a1b2c3d4e5f6 ":     "04A1B2C3D4E5F6",
	}
	for in, want := range cases {
		if got, ok := normalizeUID(in); !ok || got != want {
			t.Errorf("%q: got %q ok=%v", in, got, ok)
		}
	}
}

func TestEndingAListenEarlyFromTheApp(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	e.report(t, token, "04A1B2C3")
	url := e.http.URL + "/api/now-playing/1/end"

	if resp := do(t, c, "POST", url, nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("with the sleeve on the stand: %d", resp.StatusCode)
	}
	e.report(t, token, nil)
	if resp := do(t, c, "POST", url, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("after taking it off: %d", resp.StatusCode)
	}
	var playing []any
	json.NewDecoder(do(t, c, "GET", e.http.URL+"/api/now-playing", nil).Body).Decode(&playing)
	if len(playing) != 0 {
		t.Fatalf("still playing %+v", playing)
	}
	if resp := do(t, c, "POST", url, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nothing left to end: %d", resp.StatusCode)
	}
}

func TestAnEmptyStandReportsLessOften(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	token := e.deviceToken(t, c)
	if _, out := e.report(t, token, nil); out.IntervalMS != 30000 {
		t.Fatalf("empty stand asked to report every %d ms", out.IntervalMS)
	}
	if _, out := e.report(t, token, "04A1B2C3"); out.IntervalMS != 1500 {
		t.Fatalf("stand with a sleeve asked to report every %d ms", out.IntervalMS)
	}
}
