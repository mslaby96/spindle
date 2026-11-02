package lastfm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultBaseURL = "https://ws.audioscrobbler.com/2.0/"

var ErrNoKey = errors.New("lastfm: no API key configured")

type Client struct {
	BaseURL   string
	Key       string
	UserAgent string
	HTTP      *http.Client
}

func New(key, userAgent string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Key: key, UserAgent: userAgent, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// AlbumLength returns the summed track durations, or 0 when Last.fm doesn't know all of them.
func (c *Client) AlbumLength(ctx context.Context, artist, album string) (time.Duration, error) {
	if c.Key == "" {
		return 0, ErrNoKey
	}
	q := url.Values{
		"method":      {"album.getInfo"},
		"api_key":     {c.Key},
		"artist":      {artist},
		"album":       {album},
		"autocorrect": {"1"},
		"format":      {"json"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if uerr, ok := errors.AsType[*url.Error](err); ok {
		// the URL carries the API key, so only the cause goes into the error and on to the logs
		err = fmt.Errorf("lastfm: %s request failed: %w", uerr.Op, uerr.Err)
	}
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return 0, fmt.Errorf("lastfm: unexpected status %d", resp.StatusCode)
	}

	return albumLength(resp.Body)
}

// albumLength reads an album.getInfo response. Anything short of a full tracklist with every length is "don't know".
func albumLength(r io.Reader) (time.Duration, error) {
	var body struct {
		Error int `json:"error"`
		Album struct {
			Tracks json.RawMessage `json:"tracks"`
		} `json:"album"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return 0, err
	}
	var list struct {
		Track json.RawMessage `json:"track"`
	}
	if t := bytes.TrimSpace(body.Album.Tracks); body.Error != 0 || len(t) == 0 || t[0] != '{' || json.Unmarshal(t, &list) != nil {
		return 0, nil
	}
	tracks, err := decodeTracks(list.Track)
	if err != nil {
		return 0, err
	}
	var total int
	for _, t := range tracks {
		secs, ok := t.seconds()
		if !ok {
			return 0, nil
		}
		total += secs
	}
	return time.Duration(total) * time.Second, nil
}

type track struct {
	Duration json.RawMessage `json:"duration"`
}

func (t track) seconds() (int, bool) {
	raw := bytes.Trim(t.Duration, `"`)
	n, err := strconv.Atoi(string(raw))
	return n, err == nil && n > 0
}

// decodeTracks accepts both a list of tracks and the single object Last.fm returns for one-track albums.
func decodeTracks(raw json.RawMessage) ([]track, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '{' {
		var t track
		err := json.Unmarshal(raw, &t)
		return []track{t}, err
	}
	var ts []track
	err := json.Unmarshal(raw, &ts)
	return ts, err
}
