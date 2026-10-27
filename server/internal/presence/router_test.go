package presence

import (
	"context"
	"testing"
	"time"
)

type switchMode struct{ on bool }

func (m *switchMode) Active(context.Context, int64) (bool, error) { return m.on, nil }

type recorder struct {
	led  LED
	uids []string
}

func (r *recorder) Observe(_ context.Context, o Observation) (LED, error) {
	r.uids = append(r.uids, o.UID)
	return r.led, nil
}

func newRouter() (*Router, *switchMode, *recorder, *recorder) {
	m := &switchMode{}
	cat := &recorder{led: LEDCatalogReady}
	ses := &recorder{led: LEDKnown}
	return &Router{Mode: m, Catalog: cat, Sessions: ses, Tolerance: 90 * time.Second}, m, cat, ses
}

func TestRoutesByMode(t *testing.T) {
	r, mode, cat, ses := newRouter()
	at := time.Unix(1000, 0)
	mode.on = true
	if led, _ := r.Observe(context.Background(), Observation{DeviceID: 1, UID: "A", At: at}); led != LEDCatalogReady {
		t.Fatalf("catalog led %s", led)
	}
	mode.on = false
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "", At: at})
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "B", At: at})
	if len(cat.uids) != 1 || len(ses.uids) != 2 || ses.uids[1] != "B" {
		t.Fatalf("catalog %v sessions %v", cat.uids, ses.uids)
	}
}

func TestSleeveLeftAfterCataloguingIsIgnored(t *testing.T) {
	r, mode, _, ses := newRouter()
	at := time.Unix(1000, 0)
	obs := func(uid string, offset time.Duration) {
		r.Observe(context.Background(), Observation{DeviceID: 1, UID: uid, At: at.Add(offset)})
	}
	mode.on = true
	obs("A", 0)
	mode.on = false
	for i := 1; i <= 100; i++ {
		obs("A", time.Duration(i)*time.Minute)
	}
	for _, uid := range ses.uids {
		if uid != "" {
			t.Fatalf("sessions saw %q while the leftover sleeve should be ignored", uid)
		}
	}

	obs("", 101*time.Minute)
	obs("A", 101*time.Minute+30*time.Second)
	if ses.uids[len(ses.uids)-1] != "" {
		t.Fatal("brief removal should keep ignoring the sleeve")
	}
	obs("", 102*time.Minute)
	obs("", 104*time.Minute)
	obs("A", 105*time.Minute)
	if ses.uids[len(ses.uids)-1] != "A" {
		t.Fatal("sleeve put back after a real break should start a listen")
	}
}

func TestDifferentSleeveEndsSuppression(t *testing.T) {
	r, mode, _, ses := newRouter()
	at := time.Unix(1000, 0)
	mode.on = true
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "A", At: at})
	mode.on = false
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "A", At: at.Add(time.Minute)})
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "B", At: at.Add(2 * time.Minute)})
	if got := ses.uids[len(ses.uids)-1]; got != "B" {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyStandWhenModeEnds(t *testing.T) {
	r, mode, _, ses := newRouter()
	at := time.Unix(1000, 0)
	mode.on = true
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "", At: at})
	mode.on = false
	r.Observe(context.Background(), Observation{DeviceID: 1, UID: "A", At: at.Add(time.Minute)})
	if ses.uids[0] != "A" {
		t.Fatalf("got %v", ses.uids)
	}
}
