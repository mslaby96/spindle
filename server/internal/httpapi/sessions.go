package httpapi

import (
	"net/http"
	"time"

	"github.com/mslaby96/spindle/server/internal/sessions"
)

func (s *Server) nowPlaying(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sessions.Current(r.Context(), userID(r), s.Catalog.Album)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Resolution sessions.Resolution `json:"resolution"`
		// AlbumLengthSec goes with the answer "wrong_length": the whole album was heard and this is its real length.
		AlbumLengthSec int64 `json:"album_length_sec"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Resolution == resolutionWrongLength {
		length := time.Duration(req.AlbumLengthSec) * time.Second
		s.noContent(w, s.Sessions.CorrectLength(r.Context(), userID(r), id, length))
		return
	}
	s.noContent(w, s.Sessions.Resolve(r.Context(), userID(r), id, req.Resolution))
}

const resolutionWrongLength = "wrong_length"

func (s *Server) reviewSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Rating *int   `json:"rating"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.noContent(w, s.Inbox.Review(r.Context(), userID(r), id, req.Rating, req.Note))
}

func (s *Server) dismissSession(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.noContent(w, s.Inbox.Dismiss(r.Context(), userID(r), id))
	}
}

// endListen ends the listen on a stand whose sleeve is off, without waiting for the countdown.
func (s *Server) endListen(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Sessions.EndNow)
}
