package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

func (s *Server) searchDiscogs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []discogs.Result{})
		return
	}
	results, err := s.Discogs.Search(r.Context(), q)
	if err != nil {
		s.discogsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) masterVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	q := r.URL.Query()
	versions, err := s.Discogs.Versions(r.Context(), id, discogs.VersionFilter{
		Country:  q.Get("country"),
		Year:     q.Get("year"),
		Label:    q.Get("label"),
		CatNo:    q.Get("catno"),
		AnyMedia: q.Get("any_media") == "true",
	})
	if err != nil {
		s.discogsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) cover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var path sql.NullString
	err = s.DB.R.QueryRowContext(r.Context(), `SELECT cover_path FROM albums WHERE id = ? AND user_id = ?`, id, userID(r)).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !path.Valid) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, s.Covers.Path(path.String))
}

func (s *Server) discogsError(w http.ResponseWriter, err error) {
	var se *discogs.StatusError
	switch {
	case errors.Is(err, discogs.ErrNoToken):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, discogs.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "Discogs is rate limiting requests, try again in a minute")
	case errors.Is(err, discogs.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found on Discogs")
	case errors.As(err, &se):
		writeError(w, http.StatusBadGateway, "Discogs is not responding properly, try again later")
	default:
		s.internal(w, err)
	}
}
