package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const DefaultBaseURL = "https://musicbrainz.org/ws/2"

var ErrUnavailable = errors.New("musicbrainz: service unavailable")

type Client struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
	Interval  time.Duration

	mu   sync.Mutex
	last time.Time
}

func New(userAgent string) *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		Interval:  1100 * time.Millisecond,
	}
}

// throttle keeps requests at least Interval apart; MusicBrainz blocks clients that go faster than one per second.
func (c *Client) throttle(ctx context.Context) error {
	c.mu.Lock()
	wait := c.Interval - time.Since(c.last)
	c.last = time.Now().Add(max(wait, 0))
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if err := c.throttle(ctx); err != nil {
		return err
	}
	q.Set("fmt", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode >= 500:
		return ErrUnavailable
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("musicbrainz: unexpected status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Medium struct {
	Format     string `json:"format"`
	TrackCount int    `json:"track-count"`
	Tracks     []struct {
		Length *int `json:"length"`
	} `json:"tracks"`
}

type Release struct {
	ID         string   `json:"id"`
	Score      int      `json:"score"`
	Title      string   `json:"title"`
	TrackCount int      `json:"track-count"`
	Media      []Medium `json:"media"`
}

func (r Release) isVinyl() bool {
	for _, m := range r.Media {
		if strings.Contains(m.Format, "Vinyl") {
			return true
		}
	}
	return false
}

func (c *Client) search(ctx context.Context, artist, title string, vinyl bool) ([]Release, error) {
	query := fmt.Sprintf(`release:"%s" AND artist:"%s"`, escape(title), escape(artist))
	if vinyl {
		query += ` AND format:"12\" Vinyl"`
	}
	var resp struct {
		Releases []Release `json:"releases"`
	}
	err := c.get(ctx, "/release/", url.Values{"query": {query}, "limit": {"25"}}, &resp)
	return resp.Releases, err
}

// AlbumLength finds the release that best matches the album and returns its total length.
// expectedTracks may be 0 when unknown. It returns 0 with a nil error when nothing suitable exists.
func (c *Client) AlbumLength(ctx context.Context, artist, title string, expectedTracks int) (time.Duration, error) {
	candidates, err := c.search(ctx, artist, title, true)
	if err != nil {
		return 0, err
	}
	best, ok := Pick(candidates, expectedTracks)
	if !ok {
		if candidates, err = c.search(ctx, artist, title, false); err != nil {
			return 0, err
		}
		if best, ok = Pick(candidates, expectedTracks); !ok {
			return 0, nil
		}
	}
	var full Release
	if err := c.get(ctx, "/release/"+best.ID, url.Values{"inc": {"recordings"}}, &full); err != nil {
		return 0, err
	}
	return totalLength(full), nil
}

func totalLength(r Release) time.Duration {
	var ms int
	for _, m := range r.Media {
		for _, t := range m.Tracks {
			if t.Length == nil {
				return 0
			}
			ms += *t.Length
		}
	}
	return time.Duration(ms) * time.Millisecond
}

const minScore = 90

// Pick chooses among close matches, preferring vinyl, then the expected track count, then the most common track
// count, which steers clear of deluxe box sets and singles.
func Pick(rs []Release, expectedTracks int) (Release, bool) {
	var good []Release
	for _, r := range rs {
		if r.Score >= minScore && r.TrackCount > 0 {
			good = append(good, r)
		}
	}
	if len(good) == 0 {
		return Release{}, false
	}
	var vinyl []Release
	for _, r := range good {
		if r.isVinyl() {
			vinyl = append(vinyl, r)
		}
	}
	if len(vinyl) > 0 {
		good = vinyl
	}
	if expectedTracks > 0 {
		for _, r := range good {
			if r.TrackCount == expectedTracks {
				return r, true
			}
		}
	}
	counts := map[int]int{}
	for _, r := range good {
		counts[r.TrackCount]++
	}
	mode := 0
	for n, c := range counts {
		if c > counts[mode] || (c == counts[mode] && n < mode) {
			mode = n
		}
	}
	for _, r := range good {
		if r.TrackCount == mode {
			return r, true
		}
	}
	return Release{}, false
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
