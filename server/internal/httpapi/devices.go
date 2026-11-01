package httpapi

import (
	"net/http"

	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
)

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	dev, token, err := s.Devices.Create(r.Context(), userID(r), req.Name)
	s.reply(w, http.StatusCreated, map[string]any{"device": dev, "token": token}, err)
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.Devices.Revoke(r.Context(), userID(r), id)
	if err == nil {
		s.Bus.Publish(events.Event{Type: devices.EventRemoved, UserID: userID(r), Data: devices.Removed{ID: id}})
	}
	s.noContent(w, err)
}

func (s *Server) startPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := s.Devices.StartPairing(r.Context(), userID(r), req.Name)
	s.reply(w, http.StatusCreated, p, err)
}

// pair is called by a stand during setup, before it has a token. Wrong codes count against the caller's address.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		FW   string `json:"fw"`
	}
	if !decode(w, r, &req) || !s.allow(w, r) {
		return
	}
	dev, token, err := s.Devices.Pair(r.Context(), req.Code)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.limiter.Reset(clientIP(r))
	if dev, err = s.Devices.Touch(r.Context(), dev.ID, devices.Report{FW: req.FW}); err != nil {
		s.internal(w, err)
		return
	}
	s.Bus.Publish(events.Event{Type: devices.EventPaired, UserID: dev.UserID, Data: dev})
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) requestWiFiChange(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Devices.RequestWiFiChange)
}

func (s *Server) cancelWiFiChange(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Devices.CancelWiFiChange)
}
