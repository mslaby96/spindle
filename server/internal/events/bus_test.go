package events

import (
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(100 * time.Millisecond):
		return Event{}, false
	}
}

func TestPublishReachesOnlyOwner(t *testing.T) {
	b := NewBus()
	a, cancelA := b.Subscribe(1)
	defer cancelA()
	other, cancelB := b.Subscribe(2)
	defer cancelB()

	b.Publish(Event{Type: "now_playing", UserID: 1, Data: "04A1"})
	if e, ok := receive(t, a); !ok || e.Type != "now_playing" {
		t.Fatalf("owner got %+v ok=%v", e, ok)
	}
	if e, ok := receive(t, other); ok {
		t.Fatalf("other user got %+v", e)
	}
}

func TestCancelRemovesSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe(1)
	cancel()
	cancel()
	if n := subscribers(b, 1); n != 0 {
		t.Fatalf("subscribers after cancel: %d", n)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed")
	}
	b.Publish(Event{Type: "x", UserID: 1})
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewBus()
	_, cancel := b.Subscribe(1)
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < bufferSize*3; i++ {
			b.Publish(Event{Type: "x", UserID: 1})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}
}

func TestSubscribeAllSeesEveryUser(t *testing.T) {
	b := NewBus()
	ch, cancel := b.SubscribeAll()
	defer cancel()
	b.Publish(Event{Type: "a", UserID: 1})
	b.Publish(Event{Type: "b", UserID: 2})
	first, _ := receive(t, ch)
	second, _ := receive(t, ch)
	if first.UserID != 1 || second.UserID != 2 {
		t.Fatalf("got %+v %+v", first, second)
	}
}

func subscribers(b *Bus, userID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[userID])
}
