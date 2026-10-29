package homeassistant

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/jpeg"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"golang.org/x/image/draw"

	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

const coverSide = 600

type CurrentFunc func(ctx context.Context, userID int64) ([]sessions.NowPlaying, error)

type Publisher struct {
	Cfg     config.MQTT
	DB      *db.DB
	Bus     *events.Bus
	Covers  *covers.Store
	Current CurrentFunc
	Version string
	Log     *slog.Logger
}

// Run keeps Home Assistant in sync with what is playing until ctx ends. Without an MQTT URL it does nothing.
func (p *Publisher) Run(ctx context.Context) error {
	if !p.Cfg.Enabled() {
		return nil
	}
	u, err := url.Parse(p.Cfg.URL)
	if err != nil {
		return err
	}
	// The connection outlives ctx just long enough to tell Home Assistant that Spindle is going away.
	conn, closeConn := context.WithCancel(context.WithoutCancel(ctx))
	defer closeConn()
	resync := make(signal, 1)
	cm, err := autopaho.NewConnection(conn, p.clientConfig(ctx, u, resync.raise))
	if err != nil {
		return err
	}

	evs, cancel := p.Bus.SubscribeAll()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			p.goOffline(cm)
			return nil
		case <-resync:
			p.resync(ctx, cm)
		case e := <-evs:
			p.handle(ctx, cm, e)
		}
	}
}

func (p *Publisher) handle(ctx context.Context, cm *autopaho.ConnectionManager, e events.Event) {
	if np, ok := e.Data.(sessions.NowPlaying); ok && e.Type == sessions.EventNowPlaying {
		p.publishNowPlaying(ctx, cm, e.UserID, np)
	}
}

// signal coalesces requests: raising it while one is already pending does nothing.
type signal chan struct{}

func (s signal) raise() {
	select {
	case s <- struct{}{}:
	default:
	}
}

// clientConfig asks for a full resync whenever the broker connection comes up or Home Assistant restarts.
func (p *Publisher) clientConfig(ctx context.Context, u *url.URL, resync func()) autopaho.ClientConfig {
	haStatus := p.Cfg.Prefix + "/status"
	return autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ReconnectBackoff:              autopaho.NewConstantBackoff(10 * time.Second),
		ConnectUsername:               p.Cfg.Username,
		ConnectPassword:               []byte(p.Cfg.Password),
		WillMessage:                   &paho.WillMessage{Topic: AvailabilityTopic, Payload: []byte("offline"), QoS: 1, Retain: true},
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			p.Log.Info("connected to MQTT broker")
			go func() {
				if _, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: haStatus, QoS: 1}}}); err != nil {
					p.Log.Warn("subscribe to Home Assistant status", "err", err)
				}
			}()
			resync()
		},
		OnConnectError: func(err error) { p.Log.Warn("MQTT connection failed", "err", err) },
		ClientConfig: paho.ClientConfig{
			ClientID: "spindle",
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					if pr.Packet.Topic == haStatus && string(pr.Packet.Payload) == "online" {
						resync()
					}
					return true, nil
				},
			},
		},
	}
}

func (p *Publisher) resync(ctx context.Context, cm *autopaho.ConnectionManager) {
	actx, stop := context.WithTimeout(ctx, 10*time.Second)
	err := cm.AwaitConnection(actx)
	stop()
	if err != nil {
		return
	}
	p.publish(ctx, cm, AvailabilityTopic, []byte("online"))
	if err := p.syncAll(ctx, cm); err != nil {
		p.Log.Warn("Home Assistant sync", "err", err)
	}
}

func (p *Publisher) goOffline(cm *autopaho.ConnectionManager) {
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	p.publish(ctx, cm, AvailabilityTopic, []byte("offline"))
	cm.Disconnect(ctx)
}

func (p *Publisher) syncAll(ctx context.Context, cm *autopaho.ConnectionManager) error {
	rows, err := p.DB.R.QueryContext(ctx, `SELECT id FROM users`)
	if err != nil {
		return err
	}
	var users []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			users = append(users, id)
		}
	}
	rows.Close()
	for _, id := range users {
		topic, payload := Discovery(p.Cfg.Prefix, id, p.Version)
		p.publish(ctx, cm, topic, payload)
		current, err := p.Current(ctx, id)
		if err != nil {
			return err
		}
		np := sessions.NowPlaying{}
		if len(current) > 0 {
			np = current[0]
		}
		p.publishNowPlaying(ctx, cm, id, np)
	}
	return nil
}

func (p *Publisher) publishNowPlaying(ctx context.Context, cm *autopaho.ConnectionManager, userID int64, np sessions.NowPlaying) {
	state, attrs := State(np)
	p.publish(ctx, cm, AttributesTopic(userID), attrs)
	p.publish(ctx, cm, StateTopic(userID), []byte(state))
	if np.Album == nil || !np.Album.HasCover {
		return
	}
	img, err := p.coverJPEG(ctx, userID, np.Album.ID)
	if err != nil {
		p.Log.Warn("cover for Home Assistant", "album", np.Album.ID, "err", err)
		return
	}
	p.publish(ctx, cm, CoverTopic(userID), img)
}

func (p *Publisher) publish(ctx context.Context, cm *autopaho.ConnectionManager, topic string, payload []byte) {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := cm.Publish(pctx, &paho.Publish{Topic: topic, Payload: payload, QoS: 1, Retain: true}); err != nil &&
		!errors.Is(err, context.Canceled) {
		p.Log.Warn("MQTT publish", "topic", topic, "err", err)
	}
}

// coverJPEG re-encodes the stored cover as a modest JPEG, which is what the Home Assistant image entity expects.
func (p *Publisher) coverJPEG(ctx context.Context, userID, albumID int64) ([]byte, error) {
	var path sql.NullString
	if err := p.DB.R.QueryRowContext(ctx, `SELECT cover_path FROM albums WHERE id = ? AND user_id = ?`, albumID, userID).
		Scan(&path); err != nil {
		return nil, err
	}
	if !path.Valid {
		return nil, errors.New("no cover stored")
	}
	f, err := os.Open(p.Covers.Path(path.String))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() > coverSide || b.Dy() > coverSide {
		scale := float64(coverSide) / float64(max(b.Dx(), b.Dy()))
		dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		src = dst
	}
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85})
	return buf.Bytes(), err
}
