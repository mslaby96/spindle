package httpapi

import (
	"net/http"

	"github.com/mslaby96/spindle/server/internal/events"
)

// EventDevicePaired tells the app that a stand finished setting itself up with a pairing code.
const EventDevicePaired = "device_paired"

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
	s.act(w, r, s.Devices.Revoke)
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
	if err := s.Devices.Touch(r.Context(), dev.ID, req.FW); err != nil {
		s.internal(w, err)
		return
	}
	dev.FWVersion = req.FW
	s.Bus.Publish(events.Event{Type: EventDevicePaired, UserID: dev.UserID, Data: dev})
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
