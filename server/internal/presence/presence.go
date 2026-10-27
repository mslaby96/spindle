package presence

import (
	"context"
	"time"
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
