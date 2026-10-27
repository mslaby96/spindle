package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mslaby96/spindle/server/internal/auth"
	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/duration"
	"github.com/mslaby96/spindle/server/internal/presence"
	"github.com/mslaby96/spindle/server/internal/webui"
)

type Deps struct {
	Config    *config.Config
	DB        *db.DB
	Auth      *auth.Service
	Devices   *devices.Store
	Catalog   *catalog.Service
	Mode      *catalog.Mode
	Discogs   *discogs.Client
	Covers    *covers.Store
	Durations *duration.Resolver
	Bus       *events.Bus
	Presence  presence.Handler
	Log       *slog.Logger
}

type Server struct {
	Deps
	limiter *auth.Limiter
	mux     *http.ServeMux
}

func New(d Deps) *Server {
	s := &Server{Deps: d, limiter: auth.NewLimiter(), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.health)

	s.mux.HandleFunc("GET /api/setup", s.setupStatus)
	s.mux.HandleFunc("POST /api/setup", s.setup)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("POST /api/logout", s.logout)
	s.mux.Handle("GET /api/me", s.user(s.me))
	s.mux.Handle("POST /api/account/password", s.user(s.changePassword))

	s.mux.Handle("GET /api/devices", s.user(s.listDevices))
	s.mux.Handle("POST /api/devices", s.user(s.createDevice))
	s.mux.Handle("DELETE /api/devices/{id}", s.user(s.revokeDevice))
	s.mux.Handle("GET /api/events", s.user(s.events))

	s.mux.Handle("GET /api/discogs/search", s.user(s.searchDiscogs))
	s.mux.Handle("GET /api/discogs/masters/{id}/versions", s.user(s.masterVersions))
	s.mux.Handle("GET /api/albums/{id}/cover", s.user(s.cover))
	s.mux.Handle("PUT /api/albums/{id}/duration", s.user(s.setAlbumDuration))

	s.mux.Handle("GET /api/catalog/mode", s.user(s.catalogMode))
	s.mux.Handle("PUT /api/catalog/mode", s.user(s.setCatalogMode))
	s.mux.Handle("GET /api/tags/unassigned", s.user(s.unassignedTags))
	s.mux.Handle("GET /api/tags/{uid}", s.user(s.tagByUID))
	s.mux.Handle("POST /api/tags/{uid}/assign", s.user(s.assignTag))

	s.mux.Handle("POST /api/device/v1/report", s.device(s.report))

	s.mux.Handle("/", webui.Handler())
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.DB.R.PingContext(ctx); err != nil {
		s.Log.Error("health check failed", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}
