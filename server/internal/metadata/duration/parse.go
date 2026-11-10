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

// Tracklist sums a Discogs tracklist. It reports the number of tracks and whether every one had a duration. A suite,
// which Discogs lists as an "index" with its parts below it, is one track; headings such as "Side A" are not tracks.
func Tracklist(tracks []discogs.Track) (time.Duration, int, bool) {
	var total time.Duration
	count := 0
	complete := true
	for _, t := range tracks {
		d, ok, isTrack := trackLength(t)
		if !isTrack {
			continue
		}
		count++
		if !ok {
			complete = false
			continue
		}
		total += d
	}
	return total, count, complete && count > 0
}

func trackLength(t discogs.Track) (d time.Duration, ok, isTrack bool) {
	switch t.Type {
	case "", "track":
		d, ok = ParseTrack(t.Duration)
		return d, ok, true
	case "index":
		d, ok = indexLength(t)
		return d, ok, true
	}
	return 0, false, false
}

// indexLength takes a suite's own length, or else the sum of its parts when every part has one.
func indexLength(t discogs.Track) (time.Duration, bool) {
	if d, ok := ParseTrack(t.Duration); ok {
		return d, true
	}
	var total time.Duration
	for _, part := range t.SubTracks {
		d, ok := ParseTrack(part.Duration)
		if !ok {
			return 0, false
		}
		total += d
	}
	return total, len(t.SubTracks) > 0
}
