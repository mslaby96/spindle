package auth

import (
	"sync"
	"time"
)

const (
	freeAttempts = 3
	maxBackoff   = 5 * time.Minute
	// forgetAfter clears a client's failures after this long without an attempt, so typos don't add up for ever and
	// clients that went away don't take up memory.
	forgetAfter = 15 * time.Minute
)

type Limiter struct {
	Now func() time.Time
	// Free is how many attempts go through before the backoff starts.
	Free int

	mu      sync.Mutex
	clients map[string]*attempts
	pruned  time.Time
}

type attempts struct {
	failures  int
	blockedTo time.Time
	last      time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{Now: time.Now, Free: freeAttempts, clients: map[string]*attempts{}}
}

func (a *attempts) stale(now time.Time) bool {
	return now.Sub(a.last) >= forgetAfter && !now.Before(a.blockedTo)
}

// prune drops clients with nothing left to remember, at most once a minute.
func (l *Limiter) prune(now time.Time) {
	if now.Sub(l.pruned) < time.Minute {
		return
	}
	l.pruned = now
	for k, a := range l.clients {
		if a.stale(now) {
			delete(l.clients, k)
		}
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// Begin counts an attempt before it is checked, so parallel requests can't slip past the backoff. It reports whether
// the client may try now and, if not, how long it has to wait. Call Reset after a successful attempt.
func (l *Limiter) Begin(client string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	l.prune(now)
	a := l.clients[client]
	if a == nil || a.stale(now) {
		a = &attempts{}
		l.clients[client] = a
	}
	if wait := a.blockedTo.Sub(now); wait > 0 {
		return false, wait
	}
	a.failures++
	a.last = now
	if a.failures > l.Free {
		backoff := time.Second << (a.failures - l.Free - 1)
		if backoff > maxBackoff || backoff <= 0 {
			backoff = maxBackoff
		}
		a.blockedTo = now.Add(backoff)
	}
	return true, 0
}

// Done takes back the attempt Begin just counted, because it succeeded. Failures before it still count, so succeeding
// on one account doesn't buy more guesses at another.
func (l *Limiter) Done(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.clients[client]
	if a == nil {
		return
	}
	a.failures = max(0, a.failures-1)
	if a.failures <= l.Free {
		a.blockedTo = time.Time{}
	}
}

func (l *Limiter) Reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, client)
}
