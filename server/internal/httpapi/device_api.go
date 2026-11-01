package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/devices"
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
	// UpdateFailed names a firmware the stand couldn't download or had to roll back.
	UpdateFailed string `json:"update_failed"`
}

const (
	// commandChangeWiFi tells the stand to forget its Wi-Fi and open its setup network, keeping its pairing.
	commandChangeWiFi = "change_wifi"
	// commandUpdate tells the stand to download the firmware at /api/device/v1/firmware/ and restart into it.
	commandUpdate = "update"
)

type reportResponse struct {
	LED        presence.LED `json:"led"`
	IntervalMS int64        `json:"interval_ms"`
	Command    string       `json:"command,omitempty"`
	Version    string       `json:"version,omitempty"`
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	dev := r.Context().Value(deviceCtxKey{}).(devices.Device)
	var req reportRequest
	if !decode(w, r, &req) {
		return
	}
	uid, ok := reportedUID(req.UID)
	if !ok {
		writeError(w, http.StatusBadRequest, "uid must be 4 to 10 bytes of hex")
		return
	}
	resp, err := s.observe(r.Context(), dev, req, uid)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func reportedUID(raw *string) (string, bool) {
	if raw == nil {
		return "", true
	}
	return normalizeUID(*raw)
}

func (s *Server) observe(ctx context.Context, dev devices.Device, req reportRequest, uid string) (reportResponse, error) {
	dev, err := s.Devices.Touch(ctx, dev.ID, devices.Report{
		FW: req.FW, RSSI: req.RSSI, ReaderResets: req.ReaderResets, UptimeSec: req.UptimeSec,
	})
	if err != nil {
		return reportResponse{}, err
	}
	// after Touch, so a finished update is announced with the firmware it ended on
	if err := s.Devices.Settle(ctx, dev.ID, req.FW, req.UpdateFailed); err != nil {
		return reportResponse{}, err
	}
	s.Watcher.Seen(dev)
	led, err := s.Presence.Observe(ctx, presence.Observation{UserID: dev.UserID, DeviceID: dev.ID, UID: uid, At: time.Now()})
	if err != nil {
		return reportResponse{}, err
	}
	interval := reportInterval
	if uid == "" {
		interval = idleInterval
	}
	resp := reportResponse{LED: led, IntervalMS: interval.Milliseconds()}
	resp.Command, resp.Version, err = s.handOver(ctx, dev.ID, uid == "")
	return resp, err
}

// handOver gives the stand what was asked of it in the app, one thing per report. An update waits for an empty stand.
func (s *Server) handOver(ctx context.Context, id int64, empty bool) (command, version string, err error) {
	if taken, err := s.Devices.TakeWiFiChange(ctx, id); err != nil || taken {
		return commandIf(taken, commandChangeWiFi), "", err
	}
	if !empty {
		return "", "", nil
	}
	version, taken, err := s.Devices.TakeUpdate(ctx, id)
	return commandIf(taken, commandUpdate), version, err
}

func commandIf(taken bool, command string) string {
	if !taken {
		return ""
	}
	return command
}

func normalizeUID(s string) (string, bool) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
	b, err := hex.DecodeString(s)
	if err != nil || len(b) < 4 || len(b) > 10 {
		return "", false
	}
	return s, true
}
