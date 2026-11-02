package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/sessions"
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
	if err := s.Durations.SetManual(r.Context(), userID(r), id, d); err != nil {
		s.fail(w, albumErr(err))
		return
	}
	album, err := s.Catalog.Album(r.Context(), userID(r), id)
	s.reply(w, http.StatusOK, album, err)
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

func (s *Server) removeAlbum(w http.ResponseWriter, r *http.Request) {
	s.albumChange(w, r, s.Catalog.RemoveFromCollection)
}

func (s *Server) restoreAlbum(w http.ResponseWriter, r *http.Request) {
	s.albumChange(w, r, s.Catalog.Restore)
}

func (s *Server) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	s.albumChange(w, r, s.Catalog.DeleteAlbum)
}

// albumChange runs a change to one of the user's albums and tells the app that its listens may have changed.
func (s *Server) albumChange(w http.ResponseWriter, r *http.Request, change func(ctx context.Context, userID, albumID int64) error) {
	s.act(w, r, func(ctx context.Context, user, id int64) error {
		err := change(ctx, user, id)
		if err == nil {
			s.Bus.Publish(events.Event{Type: sessions.EventInboxChanged, UserID: user})
		}
		return albumErr(err)
	})
}

// albumErr names a missing album, which the catalog reports as no rows.
func albumErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errAlbumNotFound
	}
	return err
}

func (s *Server) removePressing(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Catalog.RemovePressing)
}

func (s *Server) restorePressing(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Catalog.RestorePressing)
}
