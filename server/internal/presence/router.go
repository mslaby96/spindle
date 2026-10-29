package presence

import (
	"context"
	"sync"
	"time"
)

type ModeChecker interface {
	Active(ctx context.Context, userID int64) (bool, error)
}

type deviceState struct {
	lastUID     string
	inCatalog   bool
	suppressed  string
	absentSince time.Time
}

// Router sends observations to cataloguing while that mode is on and to listening sessions otherwise.
// The sleeve left on the stand when cataloguing ends is ignored until it has been gone for longer than Tolerance,
// so the last catalogued record doesn't turn into a phantom listen.
type Router struct {
	Mode      ModeChecker
	Catalog   Handler
	Sessions  Handler
	Tolerance time.Duration

	mu      sync.Mutex
	devices map[int64]*deviceState
}

// listening records an observation outside cataloguing and reports whether it is the leftover sleeve to ignore.
func (s *deviceState) listening(o Observation, tolerance time.Duration) bool {
	if s.inCatalog && o.UID != "" && o.UID == s.lastUID {
		s.suppressed = o.UID
	}
	s.inCatalog, s.lastUID = false, o.UID
	if s.suppressed != "" {
		s.followSuppressed(o, tolerance)
	}
	return s.suppressed != ""
}

// followSuppressed stops ignoring the leftover sleeve once another one shows up or it has been gone long enough.
func (s *deviceState) followSuppressed(o Observation, tolerance time.Duration) {
	switch {
	case o.UID == s.suppressed:
		s.absentSince = time.Time{}
	case o.UID == "":
		if s.absentSince.IsZero() {
			s.absentSince = o.At
		}
		if o.At.Sub(s.absentSince) > tolerance {
			s.suppressed = ""
		}
	default:
		s.suppressed = ""
	}
}

func (r *Router) state(deviceID int64) *deviceState {
	if r.devices == nil {
		r.devices = map[int64]*deviceState{}
	}
	s := r.devices[deviceID]
	if s == nil {
		s = &deviceState{}
		r.devices[deviceID] = s
	}
	return s
}

func (r *Router) Observe(ctx context.Context, o Observation) (LED, error) {
	active, err := r.Mode.Active(ctx, o.UserID)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	s := r.state(o.DeviceID)
	if active {
		s.inCatalog, s.suppressed, s.lastUID = true, "", o.UID
		r.mu.Unlock()
		return r.Catalog.Observe(ctx, o)
	}

	suppressed := s.listening(o, r.Tolerance)
	r.mu.Unlock()

	if suppressed {
		o.UID = ""
	}
	return r.Sessions.Observe(ctx, o)
}
