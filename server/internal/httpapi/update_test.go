package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mslaby96/spindle/server/internal/devices"
)

type standEnv struct {
	*testEnv
	c     *http.Client
	token string
	id    int64
}

func newStand(t *testing.T, fw string) standEnv {
	t.Helper()
	e := newEnv(t)
	c := e.loggedIn(t)
	var created struct {
		Device devices.Device
		Token  string
	}
	json.NewDecoder(do(t, c, "POST", e.http.URL+"/api/devices", map[string]string{"name": "Stand"}).Body).Decode(&created)
	s := standEnv{e, c, created.Token, created.Device.ID}
	s.reportAs(t, fw, nil, "")
	return s
}

func (s standEnv) reportAs(t *testing.T, fw string, uid any, failed string) reportResponse {
	t.Helper()
	body := map[string]any{"uid": uid, "fw": fw, "seq": 1}
	if failed != "" {
		body["update_failed"] = failed
	}
	resp := do(t, s.client(t), "POST", s.http.URL+"/api/device/v1/report", body, "Authorization", "Bearer "+s.token)
	var out reportResponse
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func (s standEnv) url() string { return s.http.URL + "/api/devices/" + jsonNumber(s.id) + "/update" }

func (s standEnv) stand(t *testing.T) devices.Device {
	var list []devices.Device
	json.NewDecoder(do(t, s.c, "GET", s.http.URL+"/api/devices", nil).Body).Decode(&list)
	return list[0]
}

func TestStandUpdatesWhenAsked(t *testing.T) {
	s := newStand(t, "0.5.0")
	if got := s.stand(t).UpdateAvailable; got != testFirmware {
		t.Fatalf("update available: %q", got)
	}
	if out := s.reportAs(t, "0.5.0", nil, ""); out.Command != "" {
		t.Fatalf("updated without being asked: %+v", out)
	}
	if resp := do(t, s.c, "POST", s.url(), nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("request: %d", resp.StatusCode)
	}
	if out := s.reportAs(t, "0.5.0", "04A1B2C3", ""); out.Command != "" {
		t.Fatalf("told to update with a sleeve on: %+v", out)
	}
	out := s.reportAs(t, "0.5.0", nil, "")
	if out.Command != "update" || out.Version != testFirmware {
		t.Fatalf("not told to update: %+v", out)
	}
	if u := s.stand(t).Update; u == nil || u.State != devices.UpdateInstalling {
		t.Fatalf("installing: %+v", u)
	}
	s.reportAs(t, testFirmware, nil, "")
	if d := s.stand(t); d.Update != nil || d.UpdateAvailable != "" || d.FWVersion != testFirmware {
		t.Fatalf("after the update: %+v", d)
	}
}

func TestStandSaysTheUpdateFailed(t *testing.T) {
	s := newStand(t, "0.5.0")
	stream := openStream(t, s.c, s.http.URL+"/api/events")
	do(t, s.c, "POST", s.url(), nil)
	s.reportAs(t, "0.5.0", nil, "")
	s.reportAs(t, "0.5.0", nil, testFirmware)
	if u := s.stand(t).Update; u == nil || u.State != devices.UpdateFailed {
		t.Fatalf("failed: %+v", u)
	}
	seen := 0
	for {
		ev, ok := nextNamed(t, stream, devices.EventStatus)
		if !ok {
			break
		}
		seen++
		if seen == 3 && !strings.Contains(ev.data, `"state":"failed"`) {
			t.Fatalf("third status: %s", ev.data)
		}
	}
	if seen != 3 {
		t.Fatalf("%d status events, want requested, installing and failed", seen)
	}
	if resp := do(t, s.c, "DELETE", s.url(), nil); resp.StatusCode != http.StatusNoContent || s.stand(t).Update != nil {
		t.Fatalf("dismiss: %d", resp.StatusCode)
	}
}

func TestUpdateRequestsThatCannotBeMet(t *testing.T) {
	s := newStand(t, testFirmware)
	if resp := do(t, s.c, "POST", s.url(), nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("already up to date: %d", resp.StatusCode)
	}
	if resp := do(t, s.c, "POST", s.http.URL+"/api/devices/999/update", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown stand: %d", resp.StatusCode)
	}
}

func TestStandDownloadsItsFirmware(t *testing.T) {
	s := newStand(t, "0.5.0")
	get := func(path string) *http.Response {
		return do(t, s.client(t), "GET", s.http.URL+"/api/device/v1/firmware/"+path, nil, "Authorization", "Bearer "+s.token)
	}
	var manifest struct {
		Version string
		Files   []struct {
			Path, SHA256 string
			Size         int
		}
	}
	json.NewDecoder(get("").Body).Decode(&manifest)
	if manifest.Version != testFirmware || len(manifest.Files) != 2 {
		t.Fatalf("manifest %+v", manifest)
	}
	for _, f := range manifest.Files {
		resp := get(f.Path)
		data, _ := io.ReadAll(resp.Body)
		sum := sha256.Sum256(data)
		if resp.StatusCode != http.StatusOK || len(data) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Fatalf("%s: %d %q", f.Path, resp.StatusCode, data)
		}
	}
	if resp := get("config.json"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other file: %d", resp.StatusCode)
	}
	if resp := do(t, s.client(t), "GET", s.http.URL+"/api/device/v1/firmware/main.py", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", resp.StatusCode)
	}
}
