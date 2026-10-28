package httpapi

import (
	"errors"
	"net/http"

	"github.com/mslaby96/spindle/server/internal/devices"
)

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
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	dev, token, err := s.Devices.Create(r.Context(), userID(r), req.Name)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"device": dev, "token": token})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.Devices.Revoke(r.Context(), userID(r), id)
	if errors.Is(err, devices.ErrNotFound) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
