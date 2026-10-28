package auth

import (
	"sync"
	"time"
)

const (
	freeAttempts = 3
	maxBackoff   = 5 * time.Minute
)

type Limiter struct {
	Now func() time.Time

	mu      sync.Mutex
	clients map[string]*attempts
}

type attempts struct {
	failures  int
	blockedTo time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{Now: time.Now, clients: map[string]*attempts{}}
}

// Begin counts an attempt before it is checked, so parallel requests can't slip past the backoff. It reports whether
// the client may try now and, if not, how long it has to wait. Call Reset after a successful attempt.
func (l *Limiter) Begin(client string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.clients[client]
	if a == nil {
		a = &attempts{}
		l.clients[client] = a
	}
	if wait := a.blockedTo.Sub(l.Now()); wait > 0 {
		return false, wait
	}
	a.failures++
	if a.failures > freeAttempts {
		backoff := time.Second << (a.failures - freeAttempts - 1)
		if backoff > maxBackoff || backoff <= 0 {
			backoff = maxBackoff
		}
		a.blockedTo = l.Now().Add(backoff)
	}
	return true, 0
}

func (l *Limiter) Reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, client)
}
