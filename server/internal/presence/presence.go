package presence

import (
	"context"
	"sync"
	"time"

	"github.com/mslaby96/spindle/server/internal/events"
)

type LED string

const (
	LEDIdle         LED = "idle"
	LEDKnown        LED = "known"
	LEDUnknown      LED = "unknown"
	LEDCatalogReady LED = "catalog_ready"
	LEDCatalogSaved LED = "catalog_saved"
)

// Observation is what a source saw at a point in time. An empty UID means nothing is on the reader.
type Observation struct {
	UserID   int64
	DeviceID int64
	UID      string
	At       time.Time
}

type Handler interface {
	Observe(ctx context.Context, o Observation) (LED, error)
}

const EventTagSeen = "tag_seen"

type TagSeen struct {
	DeviceID int64  `json:"device_id"`
	UID      string `json:"uid"`
}

// Announcer publishes a tag_seen event whenever the tag on a device changes.
type Announcer struct {
	Bus *events.Bus

	mu   sync.Mutex
	last map[int64]string
}

func NewAnnouncer(bus *events.Bus) *Announcer {
	return &Announcer{Bus: bus, last: map[int64]string{}}
}

func (a *Announcer) Observe(_ context.Context, o Observation) (LED, error) {
	a.mu.Lock()
	changed := a.last[o.DeviceID] != o.UID
	a.last[o.DeviceID] = o.UID
	a.mu.Unlock()
	if changed {
		a.Bus.Publish(events.Event{
			Type:   EventTagSeen,
			UserID: o.UserID,
			Data:   TagSeen{DeviceID: o.DeviceID, UID: o.UID},
		})
	}
	if o.UID == "" {
		return LEDIdle, nil
	}
	return LEDUnknown, nil
}
