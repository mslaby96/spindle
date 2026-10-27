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
	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/config"
	"github.com/mslaby96/spindle/server/internal/covers"
	"github.com/mslaby96/spindle/server/internal/db"
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/events"
	"github.com/mslaby96/spindle/server/internal/httpapi"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/metadata/duration"
	"github.com/mslaby96/spindle/server/internal/metadata/lastfm"
	"github.com/mslaby96/spindle/server/internal/metadata/musicbrainz"
	"github.com/mslaby96/spindle/server/internal/presence"
)

const userAgent = "Spindle/0.1 ( https://github.com/mslaby96/spindle )"

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
	case "reset-password":
		err = resetPassword(cfg, os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q (expected serve, healthcheck or reset-password)", cmd)
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
	go resolver.Run(ctx)
	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.New(httpapi.Deps{
			Config:    cfg,
			DB:        d,
			Auth:      authSvc,
			Devices:   devices.NewStore(d),
			Catalog:   catalogSvc,
			Durations: resolver,
			Discogs:   discogsClient,
			Covers:    coverStore,
			Bus:       bus,
			Presence:  presence.NewAnnouncer(bus),
			Log:       log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
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
