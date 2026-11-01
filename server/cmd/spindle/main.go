package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mslaby96/spindle/server/internal/auth"
	"github.com/mslaby96/spindle/server/internal/backup"
	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/homeassistant"
	"github.com/mslaby96/spindle/server/internal/httpapi"
	"github.com/mslaby96/spindle/server/internal/inbox"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/duration"
	"github.com/mslaby96/spindle/server/internal/metadata/lastfm"
	"github.com/mslaby96/spindle/server/internal/metadata/musicbrainz"
	"github.com/mslaby96/spindle/server/internal/presence"
	"github.com/mslaby96/spindle/server/internal/sessions"
	"github.com/mslaby96/spindle/server/internal/stats"
)

const (
	version   = "0.1.0"
	userAgent = "Spindle/" + version + " ( https://github.com/mslaby96/spindle )"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:\n"+err.Error())
		os.Exit(2)
	}

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		err = serve(cfg, log)
	case "healthcheck":
		err = healthcheck(cfg)
	case "backup":
		err = backupNow(cfg)
	case "restore":
		err = restore(cfg, os.Args[2:])
	case "reset-password":
		err = resetPassword(cfg, os.Args[2:])
	case "recolor-covers":
		err = recolorCovers(cfg)
	default:
		err = fmt.Errorf("unknown command %q (expected serve, healthcheck, backup, restore, reset-password or recolor-covers)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(cfg *config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	d, err := db.Open(ctx, filepath.Join(cfg.DataDir, "spindle.db"))
	if err != nil {
		return err
	}
	defer d.Close()

	authSvc := auth.New(d)
	code, err := authSvc.PrepareSetup(ctx)
	if err != nil {
		return err
	}
	if code != "" {
		log.Info("no account yet: open Spindle in the browser and enter this setup code", "code", code)
	}

	bus := events.NewBus()
	requests, endRequests := context.WithCancel(context.Background())
	defer endRequests()
	discogsClient := discogs.New(cfg.DiscogsToken, userAgent)
	coverStore := &covers.Store{Dir: filepath.Join(cfg.DataDir, "covers")}
	catalogSvc := &catalog.Service{DB: d, Discogs: discogsClient, Covers: coverStore}
	resolver := &duration.Resolver{
		DB:           d,
		Discogs:      discogsClient,
		MusicBrainz:  musicbrainz.New(userAgent),
		ResolveCover: catalogSvc.ResolveCover,
		Log:          log,
	}
	if cfg.LastFMKey != "" {
		resolver.LastFM = lastfm.New(cfg.LastFMKey, userAgent)
	}
	engine := &sessions.Engine{DB: d, Tags: catalogSvc, Bus: bus, Cfg: cfg.Sessions, Now: time.Now, Durations: resolver, Log: log}
	resolver.OnResolved = func(userID, albumID int64) {
		if err := engine.ClassifyAlbum(ctx, userID, albumID); err != nil {
			log.Error("classify listens", "album", albumID, "err", err)
		}
	}
	go resolver.Run(ctx)
	go engine.RunSweeper(ctx, 10*time.Second)
	if cfg.Backup.Every > 0 {
		backups := &backup.Store{DB: d, DataDir: cfg.DataDir, Dir: cfg.Backup.Dir, Keep: cfg.Backup.Keep, Now: localNow(cfg)}
		go backups.Run(ctx, cfg.Backup.Every, func(path string, err error) {
			if err != nil {
				log.Error("backup failed", "dir", cfg.Backup.Dir, "err", err)
				return
			}
			log.Info("backup written", "path", path)
		})
	}
	ha := &homeassistant.Publisher{
		Cfg:    cfg.MQTT,
		DB:     d,
		Bus:    bus,
		Covers: coverStore,
		Current: func(ctx context.Context, userID int64) ([]sessions.NowPlaying, error) {
			return engine.Current(ctx, userID, catalogSvc.Album)
		},
		Version: version,
		Log:     log,
	}
	go func() {
		if err := ha.Run(ctx); err != nil {
			log.Error("Home Assistant integration stopped", "err", err)
		}
	}()

	mode := &catalog.Mode{Service: catalogSvc, Bus: bus, Idle: cfg.Sessions.CatalogIdle}
	router := &presence.Router{
		Mode:      mode,
		Catalog:   mode,
		Sessions:  engine,
		Tolerance: cfg.Sessions.AbsenceTolerance,
	}
	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.New(httpapi.Deps{
			Config:    cfg,
			DB:        d,
			Auth:      authSvc,
			Devices:   devices.NewStore(d),
			Catalog:   catalogSvc,
			Mode:      mode,
			Durations: resolver,
			Sessions:  engine,
			Inbox:     &inbox.Inbox{DB: d, Bus: bus, Now: time.Now},
			Stats:     &stats.Stats{DB: d, Now: time.Now, Loc: cfg.Location},
			Discogs:   discogsClient,
			Covers:    coverStore,
			Bus:       bus,
			Presence:  router,
			Log:       log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return requests },
	}
	srv.RegisterOnShutdown(endRequests)
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("spindle started", "config", cfg.Summary())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("spindle stopped")
	return nil
}

func healthcheck(cfg *config.Config) error {
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s", resp.Status)
	}
	return nil
}

func recolorCovers(cfg *config.Config) error {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(cfg.DataDir, "spindle.db"))
	if err != nil {
		return err
	}
	defer d.Close()

	svc := &catalog.Service{DB: d, Covers: &covers.Store{Dir: filepath.Join(cfg.DataDir, "covers")}}
	n, err := svc.RecolorCovers(ctx)
	fmt.Fprintf(os.Stderr, "Recoloured %d covers.\n", n)
	return err
}

// backupNow writes a backup next to the scheduled ones. It is safe while the server runs.
func backupNow(cfg *config.Config) error {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(cfg.DataDir, "spindle.db"))
	if err != nil {
		return err
	}
	defer d.Close()
	s := &backup.Store{DB: d, DataDir: cfg.DataDir, Dir: cfg.Backup.Dir, Keep: cfg.Backup.Keep, Now: localNow(cfg)}
	path, err := s.Create(ctx)
	if err == nil {
		fmt.Println("Backup written to", path)
	}
	return err
}

func restore(cfg *config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: spindle restore <backup.tar.gz> (stop the server first)")
	}
	aside, err := backup.Restore(args[0], cfg.DataDir)
	if err != nil {
		return err
	}
	fmt.Println("Restored.")
	if aside != "" {
		fmt.Println("The data that was there before is in", aside)
	}
	return nil
}

func resetPassword(cfg *config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: spindle reset-password <username>")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(cfg.DataDir, "spindle.db"))
	if err != nil {
		return err
	}
	defer d.Close()

	fmt.Fprint(os.Stderr, "New password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return err
	}
	if err := auth.New(d).ResetPassword(ctx, args[0], strings.TrimRight(line, "\r\n")); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password changed. All sessions were logged out.")
	return nil
}

// localNow names backups in the owner's time zone, which a container doesn't know about by itself.
func localNow(cfg *config.Config) func() time.Time {
	return func() time.Time { return time.Now().In(cfg.Location) }
}
