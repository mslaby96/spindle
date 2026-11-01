package homeassistant

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

const (
	AvailabilityTopic = "spindle/status"
	Idle              = "idle"
	maxState          = 255
)

func baseTopic(userID int64) string { return "spindle/" + strconv.FormatInt(userID, 10) }

func StateTopic(userID int64) string      { return baseTopic(userID) + "/now_playing" }
func AttributesTopic(userID int64) string { return baseTopic(userID) + "/now_playing/attributes" }
func CoverTopic(userID int64) string      { return baseTopic(userID) + "/cover" }

func StandTopic(userID, standID int64) string {
	return baseTopic(userID) + "/stand/" + strconv.FormatInt(standID, 10)
}

func StandUpdateTopic(userID, standID int64) string { return StandTopic(userID, standID) + "/update" }

// StandInstallTopic is where Home Assistant's Install button for a stand's firmware lands.
func StandInstallTopic(userID, standID int64) string { return StandTopic(userID, standID) + "/install" }

// installTarget reads the user and stand out of an install topic.
func installTarget(topic string) (userID, standID int64, ok bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "spindle" || parts[2] != "stand" || parts[4] != "install" {
		return 0, 0, false
	}
	u, err1 := strconv.ParseInt(parts[1], 10, 64)
	s, err2 := strconv.ParseInt(parts[3], 10, 64)
	return u, s, err1 == nil && err2 == nil
}

func objectID(userID int64) string { return "spindle_" + strconv.FormatInt(userID, 10) }

func standObjectID(standID int64) string { return "spindle_stand_" + strconv.FormatInt(standID, 10) }

func standDiscoveryTopic(prefix string, standID int64) string {
	return prefix + "/device/" + standObjectID(standID) + "/config"
}

// Discovery returns the device-based discovery message that makes Home Assistant create one Spindle device with a
// "Now playing" sensor and a cover image.
func Discovery(prefix string, userID int64, version string) (string, []byte) {
	id := objectID(userID)
	msg := map[string]any{
		"dev": map[string]any{
			"ids":  []string{id},
			"name": "Spindle",
			"mf":   "Spindle",
			"mdl":  "Now playing stand",
			"sw":   version,
		},
		"o": map[string]any{
			"name": "Spindle",
			"sw":   version,
			"url":  "https://github.com/mslaby96/spindle",
		},
		"avty_t": AvailabilityTopic,
		"qos":    1,
		"cmps": map[string]any{
			id + "_now_playing": map[string]any{
				"p":           "sensor",
				"name":        "Now playing",
				"uniq_id":     id + "_now_playing",
				"stat_t":      StateTopic(userID),
				"json_attr_t": AttributesTopic(userID),
				"icon":        "mdi:record-player",
			},
			id + "_cover": map[string]any{
				"p":            "image",
				"name":         "Cover",
				"uniq_id":      id + "_cover",
				"image_topic":  CoverTopic(userID),
				"content_type": "image/jpeg",
			},
		},
	}
	b, _ := json.Marshal(msg)
	return prefix + "/device/" + id + "/config", b
}

// StandDiscovery makes a stand its own Home Assistant device, linked to the user's Spindle, with its connection and
// diagnostics. Diagnostics a stand doesn't send render as None, which Home Assistant shows as unknown.
func StandDiscovery(prefix string, userID int64, d devices.Device) (string, []byte) {
	id := standObjectID(d.ID)
	state := StandTopic(userID, d.ID)
	diagnostic := func(name, key string, extra map[string]any) map[string]any {
		c := map[string]any{
			"p":       "sensor",
			"name":    name,
			"uniq_id": id + "_" + key,
			"stat_t":  state,
			"val_tpl": "{{ value_json.get('" + key + "') }}",
			"ent_cat": "diagnostic",
		}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	msg := map[string]any{
		"dev": map[string]any{
			"ids":        []string{id},
			"name":       d.Name,
			"mf":         "Spindle",
			"mdl":        "Spindle stand",
			"sw":         d.FWVersion,
			"via_device": objectID(userID),
		},
		"o":      map[string]any{"name": "Spindle", "url": "https://github.com/mslaby96/spindle"},
		"avty_t": AvailabilityTopic,
		"qos":    1,
		"cmps": map[string]any{
			id + "_connectivity": map[string]any{
				"p":       "binary_sensor",
				"name":    "Connection",
				"uniq_id": id + "_connectivity",
				"dev_cla": "connectivity",
				"stat_t":  state,
				"val_tpl": "{{ value_json.connectivity }}",
				"ent_cat": "diagnostic",
			},
			id + "_firmware": map[string]any{
				"p":       "update",
				"name":    "Firmware",
				"uniq_id": id + "_firmware",
				"dev_cla": "firmware",
				"stat_t":  StandUpdateTopic(userID, d.ID),
				"cmd_t":   StandInstallTopic(userID, d.ID),
				"pl_inst": "install",
			},
			id + "_rssi": diagnostic("Wi-Fi signal", "rssi", map[string]any{
				"dev_cla": "signal_strength", "unit_of_meas": "dBm", "stat_cla": "measurement",
			}),
			id + "_reader_resets": diagnostic("Reader resets", "reader_resets", map[string]any{
				"stat_cla": "total_increasing", "icon": "mdi:restart",
			}),
			id + "_uptime_sec": diagnostic("Uptime", "uptime_sec", map[string]any{
				"dev_cla": "duration", "unit_of_meas": "s",
			}),
		},
	}
	b, _ := json.Marshal(msg)
	return standDiscoveryTopic(prefix, d.ID), b
}

type standState struct {
	Connectivity string `json:"connectivity"`
	RSSI         *int   `json:"rssi,omitempty"`
	ReaderResets *int   `json:"reader_resets,omitempty"`
	UptimeSec    *int64 `json:"uptime_sec,omitempty"`
}

func StandState(d devices.Device) []byte {
	s := standState{Connectivity: "OFF", RSSI: d.RSSI, ReaderResets: d.ReaderResets, UptimeSec: d.UptimeSec}
	if d.Online {
		s.Connectivity = "ON"
	}
	b, _ := json.Marshal(s)
	return b
}

type updateState struct {
	Installed  string `json:"installed_version"`
	Latest     string `json:"latest_version"`
	InProgress bool   `json:"in_progress"`
}

// StandUpdateState is what Home Assistant's update entity expects: the installed and latest firmware.
func StandUpdateState(d devices.Device) []byte {
	u := updateState{Installed: d.FWVersion, Latest: cmp.Or(d.UpdateAvailable, d.FWVersion)}
	u.InProgress = d.Update != nil && d.Update.State != devices.UpdateFailed
	b, _ := json.Marshal(u)
	return b
}

type Attributes struct {
	Artist    string   `json:"artist,omitempty"`
	Album     string   `json:"album,omitempty"`
	Year      *int     `json:"year,omitempty"`
	Genres    []string `json:"genres,omitempty"`
	Styles    []string `json:"styles,omitempty"`
	Color     string   `json:"color_hex,omitempty"`
	RGB       []int    `json:"color_rgb,omitempty"`
	UID       string   `json:"tag_uid,omitempty"`
	StartedAt string   `json:"started_at,omitempty"`
}

// State turns what is playing into the sensor state and its JSON attributes.
func State(np sessions.NowPlaying) (string, []byte) {
	if !np.Active {
		return Idle, []byte("{}")
	}
	a := Attributes{UID: np.UID}
	if np.StartedAt != nil {
		a.StartedAt = np.StartedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	state := "Unknown record"
	if al := np.Album; al != nil {
		state = al.Artist + " – " + al.Title
		a.Artist, a.Album, a.Year, a.Genres, a.Styles = al.Artist, al.Title, al.Year, al.Genres, al.Styles
		if rgb, ok := parseHex(al.CoverColor); ok {
			a.Color, a.RGB = al.CoverColor, rgb
		}
	}
	b, _ := json.Marshal(a)
	return truncate(state, maxState), b
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func parseHex(s string) ([]int, bool) {
	var r, g, b int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil || len(s) != 7 {
		return nil, false
	}
	return []int{r, g, b}, true
}
