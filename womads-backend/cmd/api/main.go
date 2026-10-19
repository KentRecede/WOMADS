// Command api is the core back-end service: all business modules behind a
// REST API, talking to Supabase PostgreSQL through GORM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"womads/internal/auth"
	"womads/internal/config"
	"womads/internal/database"
	"womads/internal/server"
	"womads/internal/supabase"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.LoadAPI()
	if err != nil {
		return err
	}
	setupLogging(cfg.IsProduction())

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if cfg.JWTSecret == "" {
		slog.Info("SUPABASE_JWT_SECRET not set: only asymmetric (JWKS) tokens will be accepted")
	}
	if cfg.ServiceKey == "" {
		slog.Warn("SUPABASE_SERVICE_ROLE_KEY not set: POST /api/v1/users will answer 503")
	}

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: server.NewRouter(server.Deps{
			Cfg:      cfg,
			DB:       db,
			Verifier: auth.NewVerifier(cfg.SupabaseURL, cfg.JWTSecret),
			Profiles: auth.GormProfiles{DB: db},
			Admin:    supabase.NewAdmin(cfg.SupabaseURL, cfg.ServiceKey),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return serve(srv, "api")
}

func setupLogging(production bool) {
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if production {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))
}

// serve runs the server and shuts it down cleanly on Ctrl+C / docker stop,
// letting in-flight requests finish.
func serve(srv *http.Server, name string) error {
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "service", name, "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-stop:
		slog.Info("shutting down", "service", name)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
	return nil
}
