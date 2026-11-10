package duration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

func min(m, s int) time.Duration { return time.Duration(m)*time.Minute + time.Duration(s)*time.Second }

func TestTracklistCountsSuitesAsTracks(t *testing.T) {
	part := func(d string) discogs.Track { return discogs.Track{Type: "track", Duration: d} }
	cases := []struct {
		name     string
		tracks   []discogs.Track
		want     time.Duration
		count    int
		complete bool
	}{
		{"plain tracks", []discogs.Track{part("5:00"), part("4:30")}, min(9, 30), 2, true},
		{"a suite with its own length", []discogs.Track{
			{Type: "index", Duration: "13:19", SubTracks: []discogs.Track{part(""), part("")}}, part("7:32"),
		}, min(20, 51), 2, true},
		{"a suite timed only by its parts", []discogs.Track{
			{Type: "index", SubTracks: []discogs.Track{part("3:00"), part("2:30")}}, part("1:00"),
		}, min(6, 30), 2, true},
		{"a suite with a part missing its length", []discogs.Track{
			{Type: "index", SubTracks: []discogs.Track{part("3:00"), part("")}}, part("1:00"),
		}, min(1, 0), 2, false},
		{"a suite with no lengths at all", []discogs.Track{{Type: "index"}, part("1:00")}, min(1, 0), 2, false},
		{"headings are not tracks", []discogs.Track{{Type: "heading", Title: "Side A"}, part("5:00")}, min(5, 0), 1, true},
		{"nothing", nil, 0, 0, false},
	}
	for _, c := range cases {
		d, n, ok := Tracklist(c.tracks)
		if d != c.want || n != c.count || ok != c.complete {
			t.Errorf("%s: %v %d %v, want %v %d %v", c.name, d, n, ok, c.want, c.count, c.complete)
		}
	}
}

// Wish You Were Here, release 1892728 on Discogs: both halves of Shine On You Crazy Diamond are indexes.
const wishYouWereHere = `{"tracklist": [
	{"position": "", "type_": "index", "title": "Shine On You Crazy Diamond (1-5)", "duration": "13:19",
	 "sub_tracks": [{"position": "A1.1", "type_": "track", "title": "Part I", "duration": ""},
	                {"position": "A1.2", "type_": "track", "title": "Part II", "duration": ""}]},
	{"position": "A2", "type_": "track", "title": "Welcome To The Machine", "duration": "7:32"},
	{"position": "B1", "type_": "track", "title": "Have A Cigar", "duration": "5:10"},
	{"position": "B2", "type_": "track", "title": "Wish You Were Here", "duration": "5:15"},
	{"position": "", "type_": "index", "title": "Shine On You Crazy Diamond (6-9)", "duration": "12:31",
	 "sub_tracks": [{"position": "B3.1", "type_": "track", "title": "Part VI", "duration": ""}]}
]}`

func TestWishYouWereHereIsNotEighteenMinutes(t *testing.T) {
	var rel discogs.Release
	if err := json.Unmarshal([]byte(wishYouWereHere), &rel); err != nil {
		t.Fatal(err)
	}
	d, n, ok := Tracklist(rel.Tracklist)
	if d != min(43, 47) || n != 5 || !ok {
		t.Fatalf("%v %d %v", d, n, ok)
	}
}
