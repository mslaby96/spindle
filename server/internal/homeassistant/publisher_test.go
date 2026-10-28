package homeassistant

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
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

func startBroker(t *testing.T) string {
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
	return "mqtt://" + addr
}

type recorder struct {
	mu   sync.Mutex
	msgs map[string][]string
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

func (r *recorder) count(topic string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs[topic])
}

func listen(t *testing.T, ctx context.Context, broker string) *recorder {
	t.Helper()
	u, _ := url.Parse(broker)
	r := &recorder{msgs: map[string][]string{}}
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

func TestPublisherKeepsHomeAssistantInSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := startBroker(t)
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('a', 'h', 1)`)

	ha := listen(t, ctx, broker)
	bus := events.NewBus()
	p := &Publisher{
		Cfg:     config.MQTT{URL: broker, Prefix: "homeassistant"},
		DB:      d,
		Bus:     bus,
		Covers:  &covers.Store{Dir: t.TempDir()},
		Current: func(context.Context, int64) ([]sessions.NowPlaying, error) { return nil, nil },
		Version: "test",
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	discovery := "homeassistant/device/spindle_1/config"
	eventually(t, "discovery", func() bool { _, ok := ha.last(discovery); return ok })
	eventually(t, "idle state", func() bool { s, _ := ha.last(StateTopic(1)); return s == Idle })
	eventually(t, "online", func() bool { s, _ := ha.last(AvailabilityTopic); return s == "online" })

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
