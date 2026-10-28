package httpapi

import (
	"errors"
	"net/http"

	"github.com/mslaby96/spindle/server/internal/inbox"
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

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	cards, err := s.Inbox.List(r.Context(), userID(r))
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Resolution sessions.Resolution `json:"resolution"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := s.Sessions.Resolve(r.Context(), userID(r), id, req.Resolution); {
	case errors.Is(err, sessions.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, sessions.ErrNotPending):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sessions.ErrUnknownResolution):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

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
	s.inboxResult(w, s.Inbox.Review(r.Context(), userID(r), id, req.Rating, req.Note))
}

func (s *Server) dismissSession(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		s.inboxResult(w, s.Inbox.Dismiss(r.Context(), userID(r), id))
	}
}

func (s *Server) inboxResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, inbox.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, inbox.ErrInvalidRating):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
