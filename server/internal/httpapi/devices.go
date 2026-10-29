package httpapi

import (
	"net/http"

	"github.com/mslaby96/spindle/server/internal/events"
)

// EventDevicePaired tells the app that a stand finished setting itself up with a pairing code.
const EventDevicePaired = "device_paired"

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.Devices.List(r.Context(), userID(r))
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	dev, token, err := s.Devices.Create(r.Context(), userID(r), req.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"device": dev, "token": token})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.noContent(w, s.Devices.Revoke(r.Context(), userID(r), id))
}

func (s *Server) startPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := s.Devices.StartPairing(r.Context(), userID(r), req.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
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
