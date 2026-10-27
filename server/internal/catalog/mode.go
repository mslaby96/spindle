package catalog

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/presence"
)

const (
	EventCatalogTag  = "catalog_tag"
	EventCatalogMode = "catalog_mode"
)

type CatalogTag struct {
	DeviceID int64 `json:"device_id"`
	Tag      Tag   `json:"tag"`
	New      bool  `json:"new"`
}

type ModeState struct {
	Active bool       `json:"active"`
	Until  *time.Time `json:"until,omitempty"`
}

// Mode tracks the cataloguing switch per account. While it's on, sleeves placed on a stand open the assign flow
// instead of starting a listen, and every new sleeve keeps the mode alive for another Idle period.
type Mode struct {
	Service *Service
	Bus     *events.Bus
	Idle    time.Duration

	mu   sync.Mutex
	last map[int64]string
}

func (m *Mode) now() time.Time { return time.Unix(m.Service.now(), 0) }

func (m *Mode) State(ctx context.Context, userID int64) (ModeState, error) {
	var until sql.NullInt64
	err := m.Service.DB.R.QueryRowContext(ctx, `SELECT catalog_mode_until FROM users WHERE id = ?`, userID).Scan(&until)
	if err != nil || !until.Valid || until.Int64 <= m.now().Unix() {
		return ModeState{}, err
	}
	t := time.Unix(until.Int64, 0).UTC()
	return ModeState{Active: true, Until: &t}, nil
}

func (m *Mode) Active(ctx context.Context, userID int64) (bool, error) {
	s, err := m.State(ctx, userID)
	return s.Active, err
}

func (m *Mode) Set(ctx context.Context, userID int64, on bool) (ModeState, error) {
	var until any
	if on {
		until = m.now().Add(m.Idle).Unix()
	}
	if _, err := m.Service.DB.W.ExecContext(ctx, `UPDATE users SET catalog_mode_until = ? WHERE id = ?`, until, userID); err != nil {
		return ModeState{}, err
	}
	if !on {
		m.mu.Lock()
		m.last = nil
		m.mu.Unlock()
	}
	state, err := m.State(ctx, userID)
	if err == nil {
		m.Bus.Publish(events.Event{Type: EventCatalogMode, UserID: userID, Data: state})
	}
	return state, err
}

func (m *Mode) Observe(ctx context.Context, o presence.Observation) (presence.LED, error) {
	m.mu.Lock()
	if m.last == nil {
		m.last = map[int64]string{}
	}
	changed := m.last[o.DeviceID] != o.UID
	m.last[o.DeviceID] = o.UID
	m.mu.Unlock()

	if o.UID == "" {
		return presence.LEDCatalogReady, nil
	}
	tag, created, err := m.Service.EnsureTag(ctx, o.UserID, o.UID)
	if err != nil {
		return "", err
	}
	if changed {
		if _, err := m.Set(ctx, o.UserID, true); err != nil {
			return "", err
		}
		m.Bus.Publish(events.Event{Type: EventCatalogTag, UserID: o.UserID,
			Data: CatalogTag{DeviceID: o.DeviceID, Tag: tag, New: created}})
	}
	if tag.Album != nil {
		return presence.LEDCatalogSaved, nil
	}
	return presence.LEDCatalogReady, nil
}
