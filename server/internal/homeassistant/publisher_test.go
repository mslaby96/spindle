package homeassistant

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

func startBroker(t *testing.T) (*mqtt.Server, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	server := mqtt.New(&mqtt.Options{InlineClient: true})
	server.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	server.AddHook(new(auth.AllowHook), nil)
	if err := server.AddListener(listeners.NewTCP(listeners.Config{ID: "test", Address: addr})); err != nil {
		t.Fatal(err)
	}
	go server.Serve()
	t.Cleanup(func() { server.Close() })
	return server, "mqtt://" + addr
}

type recorder struct {
	mu   sync.Mutex
	msgs map[string][]string
	qos  map[string]byte
	cm   *autopaho.ConnectionManager
}

func (r *recorder) last(topic string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.msgs[topic]
	if len(m) == 0 {
		return "", false
	}
	return m[len(m)-1], true
}

func (r *recorder) qosOf(topic string) byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.qos[topic]
}

func (r *recorder) count(topic string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs[topic])
}

func listen(t *testing.T, ctx context.Context, broker string) *recorder {
	t.Helper()
	u, _ := url.Parse(broker)
	r := &recorder{msgs: map[string][]string{}, qos: map[string]byte{}}
	cm, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		ServerUrls: []*url.URL{u},
		KeepAlive:  30,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "#", QoS: 1}}})
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "ha-test",
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(pr paho.PublishReceived) (bool, error) {
				r.mu.Lock()
				r.msgs[pr.Packet.Topic] = append(r.msgs[pr.Packet.Topic], string(pr.Packet.Payload))
				r.qos[pr.Packet.Topic] = pr.Packet.QoS
				r.mu.Unlock()
				return true, nil
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.AwaitConnection(ctx); err != nil {
		t.Fatal(err)
	}
	r.cm = cm
	return r
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type harness struct {
	p      *Publisher
	ha     *recorder
	broker *mqtt.Server
	bus    *events.Bus
	db     *db.DB
	done   chan error
}

func startPublisher(t *testing.T, ctx context.Context) *harness {
	t.Helper()
	return startPublisherWith(t, ctx, func(context.Context, int64) ([]sessions.NowPlaying, error) { return nil, nil })
}

func startPublisherWith(t *testing.T, ctx context.Context, current CurrentFunc) *harness {
	t.Helper()
	server, broker := startBroker(t)
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`)

	h := &harness{ha: listen(t, t.Context(), broker), broker: server, bus: events.NewBus(), db: d, done: make(chan error, 1)}
	h.p = &Publisher{
		Cfg:     config.MQTT{URL: broker, Prefix: "homeassistant"},
		DB:      d,
		Bus:     h.bus,
		Covers:  &covers.Store{Dir: t.TempDir()},
		Current: current,
		Version: "test",
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() { h.done <- h.p.Run(ctx) }()
	eventually(t, "online", func() bool { s, _ := h.ha.last(AvailabilityTopic); return s == "online" })
	return h
}

func TestPublisherKeepsHomeAssistantInSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)
	ha, bus, done := h.ha, h.bus, h.done

	discovery := "homeassistant/device/spindle_1/config"
	eventually(t, "discovery", func() bool { _, ok := ha.last(discovery); return ok })
	eventually(t, "idle state", func() bool { s, _ := ha.last(StateTopic(1)); return s == Idle })

	bus.Publish(events.Event{Type: sessions.EventNowPlaying, UserID: 1, Data: sessions.NowPlaying{
		DeviceID: 1, Active: true, UID: "04A1",
		Album: &catalog.Album{ID: 1, Title: "Kind of Blue", Artist: "Miles Davis", CoverColor: "#2a9d8f"},
	}})
	eventually(t, "playing state", func() bool { s, _ := ha.last(StateTopic(1)); return s == "Miles Davis – Kind of Blue" })
	if attrs, _ := ha.last(AttributesTopic(1)); !strings.Contains(attrs, `"color_hex":"#2a9d8f"`) {
		t.Fatalf("attributes %s", attrs)
	}

	bus.Publish(events.Event{Type: sessions.EventNowPlaying, UserID: 1, Data: sessions.NowPlaying{DeviceID: 1}})
	eventually(t, "idle again", func() bool { s, _ := ha.last(StateTopic(1)); return s == Idle })

	before := ha.count(discovery)
	ha.cm.Publish(ctx, &paho.Publish{Topic: "homeassistant/status", Payload: []byte("online"), QoS: 1})
	eventually(t, "rediscovery after Home Assistant restart", func() bool { return ha.count(discovery) > before })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not stop")
	}
}

func TestPublisherDisabledWithoutBroker(t *testing.T) {
	p := &Publisher{Cfg: config.MQTT{}}
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublisherSurvivesUnreachableBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	p := &Publisher{
		Cfg: config.MQTT{URL: "mqtt://127.0.0.1:1", Prefix: "homeassistant"},
		Bus: events.NewBus(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := p.Run(ctx); err != nil {
		t.Fatalf("unreachable broker should not be fatal: %v", err)
	}
}

func TestPublisherSendsCoverAsSmallJPEG(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)

	for i, tc := range []struct{ w, h, wantW, wantH int }{
		{1200, 900, 600, 450},
		{300, 1200, 150, 600},
		{1200, 300, 600, 150},
		{500, 400, 500, 400},
	} {
		id := int64(i + 1)
		var src bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, tc.w, tc.h))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{200, 30, 30, 255}}, image.Point{}, draw.Src)
		if err := png.Encode(&src, img); err != nil {
			t.Fatal(err)
		}
		name, _, err := h.p.Covers.Save(id, src.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		h.db.W.Exec(`INSERT INTO albums (user_id, discogs_master_id, title, artist, cover_path, cover_status, created_at)
			VALUES (1, ?, 'Kind of Blue', 'Miles Davis', ?, 'resolved', 1)`, id, name)

		before := h.ha.count(CoverTopic(1))
		h.bus.Publish(events.Event{Type: sessions.EventNowPlaying, UserID: 1, Data: sessions.NowPlaying{
			DeviceID: 1, Active: true, UID: "04A1",
			Album: &catalog.Album{ID: id, Title: "Kind of Blue", Artist: "Miles Davis", HasCover: true},
		}})
		eventually(t, "cover", func() bool { return h.ha.count(CoverTopic(1)) > before })

		cover, _ := h.ha.last(CoverTopic(1))
		got, format, err := image.Decode(strings.NewReader(cover))
		if err != nil || format != "jpeg" {
			t.Fatalf("cover is %q: %v", format, err)
		}
		if b := got.Bounds(); b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("%dx%d cover sent as %dx%d", tc.w, tc.h, b.Dx(), b.Dy())
		}
	}
}

func TestPublisherLeavesOfflineWillWhenConnectionDrops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)

	cl, ok := h.broker.Clients.Get("spindle")
	if !ok {
		t.Fatal("publisher is not connected")
	}
	cl.Net.Conn.Close()
	eventually(t, "offline will", func() bool {
		h.ha.mu.Lock()
		defer h.ha.mu.Unlock()
		return slices.Contains(h.ha.msgs[AvailabilityTopic], "offline")
	})
}

func TestPublisherResyncsOnlyWhenHomeAssistantComesOnline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)
	discovery := "homeassistant/device/spindle_1/config"
	eventually(t, "discovery", func() bool { return h.ha.count(discovery) > 0 })
	before := h.ha.count(discovery)

	h.ha.cm.Publish(ctx, &paho.Publish{Topic: "homeassistant/status", Payload: []byte("offline"), QoS: 1})
	h.ha.cm.Publish(ctx, &paho.Publish{Topic: "somewhere/else", Payload: []byte("online"), QoS: 1})
	h.ha.cm.Publish(ctx, &paho.Publish{Topic: "homeassistant/status", Payload: []byte("online"), QoS: 1})
	eventually(t, "rediscovery", func() bool { return h.ha.count(discovery) > before })
	time.Sleep(300 * time.Millisecond)
	if n := h.ha.count(discovery); n != before+1 {
		t.Fatalf("discovery sent %d more times, want once", n-before)
	}
}

func TestPublisherSyncsWhatIsPlayingWhenItConnects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisherWith(t, ctx, func(context.Context, int64) ([]sessions.NowPlaying, error) {
		return []sessions.NowPlaying{{DeviceID: 1, Active: true, UID: "04A1",
			Album: &catalog.Album{ID: 1, Title: "Kind of Blue", Artist: "Miles Davis"}}}, nil
	})
	eventually(t, "playing state", func() bool { s, _ := h.ha.last(StateTopic(1)); return s == "Miles Davis – Kind of Blue" })
}

func TestPublisherDeliversAtLeastOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)
	eventually(t, "idle state", func() bool { s, _ := h.ha.last(StateTopic(1)); return s == Idle })
	for _, topic := range []string{AvailabilityTopic, StateTopic(1), AttributesTopic(1)} {
		if q := h.ha.qosOf(topic); q != 1 {
			t.Fatalf("%s delivered at QoS %d", topic, q)
		}
	}
}

func TestPublisherGoesOfflineWhenStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startPublisher(t, ctx)

	cancel()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not stop")
	}
	eventually(t, "offline", func() bool { s, _ := h.ha.last(AvailabilityTopic); return s == "offline" })
}
