package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) setAlbumDuration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Seconds *int `json:"seconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	d, ok := albumLength(req.Seconds)
	if !ok {
		writeError(w, http.StatusBadRequest, "length must be between 1 second and 24 hours")
		return
	}
	err := s.Durations.SetManual(r.Context(), userID(r), id, d)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album not found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	album, err := s.Catalog.Album(r.Context(), userID(r), id)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// albumLength parses a length typed in by the user; no length at all means "look it up again".
func albumLength(seconds *int) (*time.Duration, bool) {
	if seconds == nil {
		return nil, true
	}
	if *seconds <= 0 || *seconds > 24*3600 {
		return nil, false
	}
	d := time.Duration(*seconds) * time.Second
	return &d, true
}
