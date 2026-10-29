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
	"github.com/mslaby96/spindle/server/internal/inbox"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/duration"
	"github.com/mslaby96/spindle/server/internal/presence"
	"github.com/mslaby96/spindle/server/internal/sessions"
	"github.com/mslaby96/spindle/server/internal/stats"
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
	Sessions  *sessions.Engine
	Inbox     *inbox.Inbox
	Stats     *stats.Stats
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
	s.mux.Handle("POST /api/devices/pairing", s.user(s.startPairing))
	s.mux.Handle("GET /api/events", s.user(s.events))
	s.mux.Handle("GET /api/now-playing", s.user(s.nowPlaying))
	s.mux.Handle("GET /api/inbox", s.user(s.listInbox))
	s.mux.Handle("POST /api/sessions/{id}/resolve", s.user(s.resolveSession))
	s.mux.Handle("PUT /api/sessions/{id}/review", s.user(s.reviewSession))
	s.mux.Handle("POST /api/sessions/{id}/dismiss", s.user(s.dismissSession))
	s.mux.Handle("PUT /api/sessions/{id}", s.user(s.editSession))
	s.mux.Handle("DELETE /api/sessions/{id}", s.user(s.deleteSession))

	s.mux.Handle("GET /api/library", s.user(s.library))
	s.mux.Handle("GET /api/library/forgotten", s.user(s.forgotten))
	s.mux.Handle("GET /api/diary", s.user(s.diary))
	s.mux.Handle("GET /api/stats/top", s.user(s.topStats))
	s.mux.Handle("GET /api/stats/timeline", s.user(s.timeline))
	s.mux.Handle("GET /api/albums/{id}", s.user(s.albumDetail))

	s.mux.Handle("GET /api/discogs/search", s.user(s.searchDiscogs))
	s.mux.Handle("GET /api/discogs/masters/{id}/versions", s.user(s.masterVersions))
	s.mux.Handle("GET /api/albums/{id}/cover", s.user(s.cover))
	s.mux.Handle("PUT /api/albums/{id}/duration", s.user(s.setAlbumDuration))
	s.mux.Handle("POST /api/albums/{id}/remove", s.user(s.removeAlbum))
	s.mux.Handle("POST /api/albums/{id}/restore", s.user(s.restoreAlbum))
	s.mux.Handle("DELETE /api/albums/{id}", s.user(s.deleteAlbum))

	s.mux.Handle("GET /api/catalog/mode", s.user(s.catalogMode))
	s.mux.Handle("PUT /api/catalog/mode", s.user(s.setCatalogMode))
	s.mux.Handle("GET /api/tags/unassigned", s.user(s.unassignedTags))
	s.mux.Handle("GET /api/tags/{uid}", s.user(s.tagByUID))
	s.mux.Handle("POST /api/tags/{uid}/assign", s.user(s.assignTag))

	s.mux.Handle("POST /api/device/v1/report", s.device(s.report))
	s.mux.HandleFunc("POST /api/device/v1/pair", s.pair)

	s.mux.Handle("/", webui.Handler())
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
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
