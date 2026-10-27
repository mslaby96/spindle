package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

type Server struct {
	db  *db.DB
	log *slog.Logger
	mux *http.ServeMux
}

func New(d *db.DB, log *slog.Logger) *Server {
	s := &Server{db: d, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.health)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.R.PingContext(ctx); err != nil {
		s.log.Error("health check failed", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}
