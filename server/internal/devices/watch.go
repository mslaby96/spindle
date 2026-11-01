package devices

import (
	"context"
	"sync"
	"time"
)

const (
	// EventPaired tells the app that a stand finished setting itself up with a pairing code.
	EventPaired = "device_paired"
	// EventStatus tells the app that a stand went quiet or came back.
	EventStatus = "device_status"
	// EventRemoved carries a Removed.
	EventRemoved = "device_removed"
)

type Removed struct {
	ID int64 `json:"id"`
}

// Watcher notices stands going online and offline. A stand goes offline only after OfflineAfter without a report,
// but comes back with its first report, so a shaky connection doesn't make it flicker.
type Watcher struct {
	Store   *Store
	Publish func(Device)

	mu     sync.Mutex
	online map[int64]bool
}

// Run checks every interval until ctx ends.
func (w *Watcher) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check looks at every stand. Stands it hasn't seen before are only remembered, so a restart announces nothing.
func (w *Watcher) Check(ctx context.Context) error {
	all, err := w.Store.All(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	next, changed := changes(w.online, all)
	w.online = next
	w.mu.Unlock()
	for _, d := range changed {
		w.Publish(d)
	}
	return nil
}

// Seen is called with a stand that has just reported.
func (w *Watcher) Seen(d Device) {
	w.mu.Lock()
	was, known := w.online[d.ID]
	if w.online == nil {
		w.online = map[int64]bool{}
	}
	w.online[d.ID] = d.Online
	w.mu.Unlock()
	if known && was != d.Online {
		w.Publish(d)
	}
}

func changes(prev map[int64]bool, all []Device) (map[int64]bool, []Device) {
	next := make(map[int64]bool, len(all))
	var changed []Device
	for _, d := range all {
		next[d.ID] = d.Online
		if was, known := prev[d.ID]; known && was != d.Online {
			changed = append(changed, d)
		}
	}
	return next, changed
}
