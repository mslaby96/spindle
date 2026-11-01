package homeassistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

func TestDiscovery(t *testing.T) {
	topic, payload := Discovery("homeassistant", 1, "0.1.0")
	if topic != "homeassistant/device/spindle_1/config" {
		t.Fatalf("topic %s", topic)
	}
	var msg struct {
		Dev  struct{ IDs []string }
		O    struct{ Name string }
		Avty string `json:"avty_t"`
		QoS  int
		Cmps map[string]struct {
			P      string
			UniqID string `json:"uniq_id"`
			StatT  string `json:"stat_t"`
			Image  string `json:"image_topic"`
		}
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Dev.IDs) != 1 || msg.O.Name != "Spindle" || msg.Avty != AvailabilityTopic || msg.QoS != 1 {
		t.Fatalf("device %+v", msg)
	}
	sensor, image := msg.Cmps["spindle_1_now_playing"], msg.Cmps["spindle_1_cover"]
	if sensor.P != "sensor" || sensor.StatT != StateTopic(1) || image.P != "image" || image.Image != CoverTopic(1) {
		t.Fatalf("components %+v", msg.Cmps)
	}
	if sensor.UniqID == image.UniqID {
		t.Fatal("components share a unique id")
	}
	_, other := Discovery("homeassistant", 2, "0.1.0")
	if strings.Contains(string(other), "spindle_1") {
		t.Fatal("second user reuses ids")
	}
}

func TestPlayingState(t *testing.T) {
	year := 1973
	started := time.Date(2026, 3, 14, 20, 0, 0, 0, time.UTC)
	state, attrs := State(sessions.NowPlaying{
		Active: true, UID: "04A1", StartedAt: &started,
		Album: &catalog.Album{Title: "The Dark Side Of The Moon", Artist: "Pink Floyd", Year: &year,
			Genres: []string{"Rock"}, Styles: []string{"Prog Rock"}, CoverColor: "#1d3557"},
	})
	if state != "Pink Floyd – The Dark Side Of The Moon" {
		t.Fatalf("state %q", state)
	}
	var a Attributes
	json.Unmarshal(attrs, &a)
	if a.Album != "The Dark Side Of The Moon" || a.Color != "#1d3557" || len(a.RGB) != 3 || a.RGB[0] != 0x1d ||
		a.StartedAt != "2026-03-14T20:00:00Z" || a.Styles[0] != "Prog Rock" {
		t.Fatalf("attributes %+v", a)
	}
}

func TestIdleClearsAttributes(t *testing.T) {
	state, attrs := State(sessions.NowPlaying{DeviceID: 1})
	if state != Idle || string(attrs) != "{}" {
		t.Fatalf("%q %s", state, attrs)
	}
}

func TestUnknownRecord(t *testing.T) {
	state, attrs := State(sessions.NowPlaying{Active: true, UID: "04FF"})
	if state != "Unknown record" || !strings.Contains(string(attrs), "04FF") {
		t.Fatalf("%q %s", state, attrs)
	}
}

func TestLongStateIsTruncated(t *testing.T) {
	title := strings.Repeat("Симфония ", 40)
	state, attrs := State(sessions.NowPlaying{Active: true, Album: &catalog.Album{Title: title, Artist: "Orchestra"}})
	if n := utf8.RuneCountInString(state); n != maxState {
		t.Fatalf("state has %d characters", n)
	}
	if !strings.Contains(string(attrs), strings.TrimSpace(title)) {
		t.Fatal("full title missing from attributes")
	}
}

func TestStateOfExactlyMaxLengthIsKept(t *testing.T) {
	title := strings.Repeat("я", maxState-utf8.RuneCountInString("A – "))
	state, _ := State(sessions.NowPlaying{Active: true, Album: &catalog.Album{Title: title, Artist: "A"}})
	if state != "A – "+title {
		t.Fatalf("state %q", state)
	}
}

func TestInvalidCoverColorIsLeftOut(t *testing.T) {
	for _, c := range []string{"#zzzzzz", "#1d3557ff", "1d3557"} {
		_, attrs := State(sessions.NowPlaying{Active: true, Album: &catalog.Album{Title: "T", Artist: "A", CoverColor: c}})
		var a Attributes
		json.Unmarshal(attrs, &a)
		if a.Color != "" || a.RGB != nil {
			t.Fatalf("%q gave %s", c, attrs)
		}
	}
}

func TestStandDiscovery(t *testing.T) {
	topic, payload := StandDiscovery("homeassistant", 1, devices.Device{ID: 7, Name: "Living room", FWVersion: "0.4.0"})
	if topic != "homeassistant/device/spindle_stand_7/config" {
		t.Fatalf("topic %s", topic)
	}
	var msg struct {
		Dev struct {
			IDs      []string
			Name, SW string
			Via      string `json:"via_device"`
		}
		Avty string `json:"avty_t"`
		Cmps map[string]struct {
			P, Dev string
			DevCla string `json:"dev_cla"`
			EntCat string `json:"ent_cat"`
			StatT  string `json:"stat_t"`
			UniqID string `json:"uniq_id"`
		}
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Dev.IDs[0] != "spindle_stand_7" || msg.Dev.Name != "Living room" || msg.Dev.SW != "0.4.0" ||
		msg.Dev.Via != "spindle_1" || msg.Avty != AvailabilityTopic {
		t.Fatalf("device %+v", msg)
	}
	conn := msg.Cmps["spindle_stand_7_connectivity"]
	if conn.P != "binary_sensor" || conn.DevCla != "connectivity" || conn.StatT != StandTopic(1, 7) {
		t.Fatalf("connectivity %+v", conn)
	}
	for _, key := range []string{"spindle_stand_7_rssi", "spindle_stand_7_reader_resets", "spindle_stand_7_uptime_sec"} {
		c, ok := msg.Cmps[key]
		if !ok || c.P != "sensor" || c.EntCat != "diagnostic" || c.UniqID != key || c.StatT != StandTopic(1, 7) {
			t.Errorf("%s: %+v", key, c)
		}
	}
}

func TestStandStateLeavesOutWhatIsUnknown(t *testing.T) {
	resets := 0
	if got := string(StandState(devices.Device{Online: true, ReaderResets: &resets})); got != `{"connectivity":"ON","reader_resets":0}` {
		t.Fatalf("online %s", got)
	}
	if got := string(StandState(devices.Device{})); got != `{"connectivity":"OFF"}` {
		t.Fatalf("offline %s", got)
	}
}
