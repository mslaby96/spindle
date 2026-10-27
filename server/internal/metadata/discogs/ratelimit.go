package discogs

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	window   = time.Minute
	headroom = 5
)

// limiter lets requests through freely while Discogs reports spare budget and spaces them out once it runs low.
type limiter struct {
	mu        sync.Mutex
	perMinute int
	remaining int
	next      time.Time
	now       func() time.Time
}

func newLimiter() *limiter {
	return &limiter{perMinute: 60, remaining: 60, now: time.Now}
}

func (l *limiter) delay() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.remaining > headroom {
		l.remaining--
		return 0
	}
	gap := window / time.Duration(l.perMinute)
	if l.remaining <= 1 {
		gap = window / 2
	}
	start := l.next
	if start.Before(now) {
		start = now
	}
	l.next = start.Add(gap)
	return start.Sub(now)
}

func (l *limiter) wait(ctx context.Context) error {
	if d := l.delay(); d > 0 {
		return sleep(ctx, d)
	}
	return nil
}

func (l *limiter) update(h http.Header) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n, err := strconv.Atoi(h.Get("X-Discogs-Ratelimit")); err == nil && n > 0 {
		l.perMinute = n
	}
	if n, err := strconv.Atoi(h.Get("X-Discogs-Ratelimit-Remaining")); err == nil {
		l.remaining = n
	}
}
