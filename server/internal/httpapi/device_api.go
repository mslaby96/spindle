package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/presence"
)

const (
	// reportInterval keeps a sleeve on the stand fresh well inside the five seconds after which it counts as taken off.
	reportInterval = 1500 * time.Millisecond
	// idleInterval is for an empty stand: putting a sleeve on is reported at once anyway, so this is only a heartbeat
	// and lets the stand keep its radio quiet.
	idleInterval = 30 * time.Second
)

type deviceCtxKey struct{}

func (s *Server) device(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing device token")
			return
		}
		dev, err := s.Devices.Authenticate(r.Context(), strings.TrimSpace(token))
		if errors.Is(err, devices.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid device token")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), deviceCtxKey{}, dev)))
	})
}

type reportRequest struct {
	UID          *string `json:"uid"`
	FW           string  `json:"fw"`
	Seq          int64   `json:"seq"`
	RSSI         *int    `json:"rssi"`
	ReaderResets *int    `json:"reader_resets"`
	UptimeSec    *int64  `json:"uptime_sec"`
}

// commandChangeWiFi tells the stand to forget its Wi-Fi and open its setup network, keeping its pairing.
const commandChangeWiFi = "change_wifi"

type reportResponse struct {
	LED        presence.LED `json:"led"`
	IntervalMS int64        `json:"interval_ms"`
	Command    string       `json:"command,omitempty"`
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	dev := r.Context().Value(deviceCtxKey{}).(devices.Device)
	var req reportRequest
	if !decode(w, r, &req) {
		return
	}
	uid := ""
	if req.UID != nil {
		var ok bool
		if uid, ok = normalizeUID(*req.UID); !ok {
			writeError(w, http.StatusBadRequest, "uid must be 4 to 10 bytes of hex")
			return
		}
	}
	dev, err := s.Devices.Touch(r.Context(), dev.ID, devices.Report{
		FW: req.FW, RSSI: req.RSSI, ReaderResets: req.ReaderResets, UptimeSec: req.UptimeSec,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	s.Watcher.Seen(dev)
	led, err := s.Presence.Observe(r.Context(), presence.Observation{
		UserID:   dev.UserID,
		DeviceID: dev.ID,
		UID:      uid,
		At:       time.Now(),
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	interval := reportInterval
	if uid == "" {
		interval = idleInterval
	}
	resp := reportResponse{LED: led, IntervalMS: interval.Milliseconds()}
	if dev.WiFiChangePending {
		if resp.Command, err = s.handOver(r.Context(), dev); err != nil {
			s.internal(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handOver(ctx context.Context, dev devices.Device) (string, error) {
	taken, err := s.Devices.TakeWiFiChange(ctx, dev.ID)
	if err != nil || !taken {
		return "", err
	}
	dev.WiFiChangePending = false
	s.Bus.Publish(events.Event{Type: devices.EventStatus, UserID: dev.UserID, Data: dev})
	return commandChangeWiFi, nil
}

func normalizeUID(s string) (string, bool) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
	b, err := hex.DecodeString(s)
	if err != nil || len(b) < 4 || len(b) > 10 {
		return "", false
	}
	return s, true
}
