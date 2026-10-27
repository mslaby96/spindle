package duration

import (
	"strconv"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

// ParseTrack parses Discogs track durations such as "5:00" or "1:02:03".
func ParseTrack(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var total int
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return time.Duration(total) * time.Second, total > 0
}

// Tracklist sums a Discogs tracklist. It reports the number of tracks and whether every one had a duration.
func Tracklist(tracks []discogs.Track) (time.Duration, int, bool) {
	var total time.Duration
	count := 0
	complete := true
	for _, t := range tracks {
		if t.Type != "" && t.Type != "track" {
			continue
		}
		count++
		d, ok := ParseTrack(t.Duration)
		if !ok {
			complete = false
			continue
		}
		total += d
	}
	return total, count, complete && count > 0
}
