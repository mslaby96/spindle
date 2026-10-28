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
	var d *time.Duration
	if req.Seconds != nil {
		if *req.Seconds <= 0 || *req.Seconds > 24*3600 {
			writeError(w, http.StatusBadRequest, "length must be between 1 second and 24 hours")
			return
		}
		v := time.Duration(*req.Seconds) * time.Second
		d = &v
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
