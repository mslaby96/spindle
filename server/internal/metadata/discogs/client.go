package discogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxResponse caps what is read from the API; real answers are far smaller.
const maxResponse = 4 << 20

const (
	DefaultBaseURL = "https://api.discogs.com"
	maxImageBytes  = 8 << 20
)

var (
	ErrNotFound    = errors.New("discogs: not found")
	ErrRateLimited = errors.New("discogs: rate limited")
	ErrNoToken     = errors.New("discogs: no personal access token configured (SPINDLE_DISCOGS_TOKEN)")
)

// StatusError is returned for unexpected HTTP responses so callers can tell transient failures apart.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("discogs: unexpected status %d", e.Code) }

type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
	limiter   *limiter
}

func New(token, userAgent string) *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		Token:     token,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		limiter:   newLimiter(),
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.Token == "" {
		return ErrNoToken
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	wait, err := c.fetch(ctx, u, out)
	if !errors.Is(err, ErrRateLimited) {
		return err
	}
	if err := sleep(ctx, wait); err != nil {
		return err
	}
	_, err = c.fetch(ctx, u, out)
	return err
}

// fetch makes one request and decodes the response into out. When Discogs asks to slow down it returns
// ErrRateLimited and how long to wait before trying again.
func (c *Client) fetch(ctx context.Context, u string, out any) (time.Duration, error) {
	if err := c.limiter.wait(ctx); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Authorization", "Discogs token="+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	c.limiter.update(resp.Header)
	if err := statusError(resp.StatusCode); err != nil {
		return retryAfter(resp.Header), err
	}
	return 0, json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(out)
}

func statusError(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized:
		return errors.New("discogs: token rejected, check SPINDLE_DISCOGS_TOKEN")
	}
	return &StatusError{Code: code}
}

func (c *Client) Image(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, errors.New("discogs: image too large")
	}
	return data, nil
}

func retryAfter(h http.Header) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 && s < 120 {
		return time.Duration(s) * time.Second
	}
	return 10 * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var disambiguation = regexp.MustCompile(`\s+\(\d+\)$`)

// CleanName removes the numeric suffix Discogs uses to tell same-named artists apart, e.g. "Nirvana (2)".
func CleanName(s string) string {
	return disambiguation.ReplaceAllString(strings.TrimSpace(s), "")
}

type Artist struct {
	Name string `json:"name"`
	ANV  string `json:"anv"`
	Join string `json:"join"`
}

func ArtistNames(artists []Artist) string {
	var b strings.Builder
	for i, a := range artists {
		name := a.ANV
		if name == "" {
			name = a.Name
		}
		b.WriteString(CleanName(name))
		if i < len(artists)-1 {
			join := strings.TrimSpace(a.Join)
			switch join {
			case "", ",":
				b.WriteString(", ")
			default:
				b.WriteString(" " + join + " ")
			}
		}
	}
	return b.String()
}
