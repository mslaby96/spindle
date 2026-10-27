package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/events"
)

const EventTagAssigned = "tag_assigned"

func (s *Server) catalogMode(w http.ResponseWriter, r *http.Request) {
	state, err := s.Mode.State(r.Context(), userID(r))
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) setCatalogMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Active bool `json:"active"`
	}
	if !decode(w, r, &req) {
		return
	}
	state, err := s.Mode.Set(r.Context(), userID(r), req.Active)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) unassignedTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.Catalog.UnassignedTags(r.Context(), userID(r))
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) tagByUID(w http.ResponseWriter, r *http.Request) {
	uid, ok := normalizeUID(r.PathValue("uid"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid uid")
		return
	}
	tag, err := s.Catalog.TagByUID(r.Context(), userID(r), uid)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) assignTag(w http.ResponseWriter, r *http.Request) {
	uid, ok := normalizeUID(r.PathValue("uid"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid uid")
		return
	}
	var req catalog.Assignment
	if !decode(w, r, &req) {
		return
	}
	tag, err := s.Catalog.Assign(r.Context(), userID(r), uid, req)
	switch {
	case errors.Is(err, catalog.ErrNothingToAssign):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.discogsError(w, err)
		return
	}
	s.Durations.Enqueue(userID(r), tag.Album.ID)
	s.Bus.Publish(events.Event{Type: EventTagAssigned, UserID: userID(r), Data: tag})
	writeJSON(w, http.StatusOK, tag)
}
