package homeassistant

import (
	"bytes"
	"cmp"
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
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

const coverSide = 600

type CurrentFunc func(ctx context.Context, userID int64) ([]sessions.NowPlaying, error)

type StandsFunc func(ctx context.Context, userID int64) ([]devices.Device, error)

// InstallFunc asks a stand to update its firmware.
type InstallFunc func(ctx context.Context, userID, standID int64) error

type Publisher struct {
	Cfg     config.MQTT
	DB      *db.DB
	Bus     *events.Bus
	Covers  *covers.Store
	Current CurrentFunc
	Stands  StandsFunc
	Install InstallFunc
	// StandsEvery is how often the stands' diagnostics are sent again, a minute unless set.
	StandsEvery time.Duration
	Version     string
	Log         *slog.Logger
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

	p.serve(ctx, cm, resync)
	return nil
}

// serve passes what happens in Spindle on to Home Assistant until ctx ends, then says goodbye.
func (p *Publisher) serve(ctx context.Context, cm *autopaho.ConnectionManager, resync signal) {
	evs, cancel := p.Bus.SubscribeAll()
	defer cancel()
	refresh := time.NewTicker(cmp.Or(p.StandsEvery, time.Minute))
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			p.goOffline(cm)
			return
		case <-resync:
			p.resync(ctx, cm)
		case <-refresh.C:
			if err := p.syncStands(ctx, cm); err != nil {
				p.Log.Warn("Home Assistant stands", "err", err)
			}
		case e := <-evs:
			p.handle(ctx, cm, e)
		}
	}
}

func (p *Publisher) handle(ctx context.Context, cm *autopaho.ConnectionManager, e events.Event) {
	switch data := e.Data.(type) {
	case sessions.NowPlaying:
		if e.Type == sessions.EventNowPlaying {
			p.publishNowPlaying(ctx, cm, e.UserID, data)
		}
	case devices.Device:
		p.publishStand(ctx, cm, e.UserID, data)
	case devices.Removed:
		// empty retained messages make Home Assistant drop the device and forget its last state
		p.publish(ctx, cm, standDiscoveryTopic(p.Cfg.Prefix, data.ID), nil)
		p.publish(ctx, cm, StandTopic(e.UserID, data.ID), nil)
		p.publish(ctx, cm, StandUpdateTopic(e.UserID, data.ID), nil)
	}
}

func (p *Publisher) publishStand(ctx context.Context, cm *autopaho.ConnectionManager, userID int64, d devices.Device) {
	topic, payload := StandDiscovery(p.Cfg.Prefix, userID, d)
	p.publish(ctx, cm, topic, payload)
	p.publish(ctx, cm, StandTopic(userID, d.ID), StandState(d))
	p.publish(ctx, cm, StandUpdateTopic(userID, d.ID), StandUpdateState(d))
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
				if _, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{
					{Topic: haStatus, QoS: 1}, {Topic: "spindle/+/stand/+/install", QoS: 1},
				}}); err != nil {
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
					if user, stand, ok := installRequest(pr.Packet); ok {
						go p.install(ctx, user, stand)
					}
					return true, nil
				},
			},
		},
	}
}

// installRequest tells a press of Install in Home Assistant from other messages. A retained one would come back on every
// reconnect, so it doesn't count.
func installRequest(p *paho.Publish) (userID, standID int64, ok bool) {
	if p.Retain || string(p.Payload) != "install" {
		return 0, 0, false
	}
	return installTarget(p.Topic)
}

func (p *Publisher) install(ctx context.Context, userID, standID int64) {
	if err := p.Install(ctx, userID, standID); err != nil {
		p.Log.Warn("firmware update from Home Assistant", "stand", standID, "err", err)
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

func (p *Publisher) users(ctx context.Context) ([]int64, error) {
	rows, err := p.DB.R.QueryContext(ctx, `SELECT id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			users = append(users, id)
		}
	}
	return users, rows.Err()
}

func (p *Publisher) syncStands(ctx context.Context, cm *autopaho.ConnectionManager) error {
	users, err := p.users(ctx)
	if err != nil {
		return err
	}
	for _, id := range users {
		stands, err := p.Stands(ctx, id)
		if err != nil {
			return err
		}
		for _, d := range stands {
			p.publishStand(ctx, cm, id, d)
		}
	}
	return nil
}

func (p *Publisher) syncAll(ctx context.Context, cm *autopaho.ConnectionManager) error {
	users, err := p.users(ctx)
	if err != nil {
		return err
	}
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
	return p.syncStands(ctx, cm)
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
