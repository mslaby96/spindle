package homeassistant

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

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

func objectID(userID int64) string { return "spindle_" + strconv.FormatInt(userID, 10) }

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
