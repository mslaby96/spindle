package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/stats"
)

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	items, err := s.Stats.Library(r.Context(), userID(r), r.URL.Query().Get("sort"))
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) forgotten(w http.ResponseWriter, r *http.Request) {
	f, err := s.Stats.Forgotten(r.Context(), userID(r), s.Config.Sessions.ForgottenAfter)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) diary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := stats.DiaryFilter{}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before must be an RFC 3339 time")
			return
		}
		f.Before = &t
		f.BeforeID, _ = strconv.ParseInt(q.Get("before_id"), 10, 64)
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	entries, err := s.Stats.Diary(r.Context(), userID(r), f)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) albumDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	page, err := s.albumPage(r.Context(), userID(r), id)
	s.reply(w, http.StatusOK, page, albumErr(err))
}

type albumPage struct {
	Album    catalog.Album      `json:"album"`
	Stats    stats.Item         `json:"stats"`
	Releases []catalog.Release  `json:"releases"`
	History  []stats.DiaryEntry `json:"history"`
	Sleeves  []string           `json:"sleeves"`
}

func (s *Server) albumPage(ctx context.Context, user, id int64) (p albumPage, err error) {
	if p.Album, err = s.Catalog.Album(ctx, user, id); err != nil {
		return p, err
	}
	if p.Stats, err = s.Stats.AlbumItem(ctx, user, id); err != nil {
		return p, err
	}
	if p.Releases, err = s.Catalog.Releases(ctx, user, id); err != nil {
		return p, err
	}
	if p.History, err = s.Stats.Diary(ctx, user, stats.DiaryFilter{AlbumID: id, Limit: 200}); err != nil {
		return p, err
	}
	p.Sleeves, err = s.Catalog.Sleeves(ctx, user, id)
	return p, err
}

func (s *Server) editSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		StartedAt time.Time `json:"started_at"`
		EndedAt   time.Time `json:"ended_at"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.noContent(w, s.Sessions.Edit(r.Context(), userID(r), id, req.StartedAt, req.EndedAt))
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	s.act(w, r, s.Sessions.Delete)
}

func (s *Server) topStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period, measure := q.Get("period"), q.Get("measure")
	if period == "" {
		period = "30d"
	}
	if measure == "" {
		measure = "plays"
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	top, err := s.Stats.Top(r.Context(), userID(r), period, measure, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, top)
}

func (s *Server) timeline(w http.ResponseWriter, r *http.Request) {
	weeks, _ := strconv.Atoi(r.URL.Query().Get("weeks"))
	list, err := s.Stats.Timeline(r.Context(), userID(r), weeks)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
