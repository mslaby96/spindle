package events

import "sync"

const bufferSize = 32

type Event struct {
	Type   string `json:"type"`
	UserID int64  `json:"-"`
	Data   any    `json:"data,omitempty"`
}

type Bus struct {
	mu   sync.Mutex
	subs map[int64]map[chan Event]struct{}
}

func NewBus() *Bus {
	return &Bus{subs: map[int64]map[chan Event]struct{}{}}
}

// Subscribe returns a channel of events for one user and a function that ends the subscription.
func (b *Bus) Subscribe(userID int64) (<-chan Event, func()) {
	ch := make(chan Event, bufferSize)
	b.mu.Lock()
	if b.subs[userID] == nil {
		b.subs[userID] = map[chan Event]struct{}{}
	}
	b.subs[userID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[userID], ch)
			if len(b.subs[userID]) == 0 {
				delete(b.subs, userID)
			}
			b.mu.Unlock()
			close(ch)
		})
	}
}

// Publish delivers the event to the user's subscribers, dropping it for any subscriber that is not keeping up.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[e.UserID] {
		select {
		case ch <- e:
		default:
		}
	}
}

func (b *Bus) subscribers(userID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[userID])
}
