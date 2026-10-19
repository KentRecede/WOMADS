// Command gateway is the public entry point: it forwards requests to the
// back-end services and adds CORS and request ids.
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

	"womads/internal/config"
	"womads/internal/gateway"
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
	cfg, err := config.LoadGateway()
	if err != nil {
		return err
	}
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if cfg.Env == "production" {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))

	if len(cfg.AllowedOrigins) == 0 {
		slog.Warn("CORS_ALLOWED_ORIGINS is empty: browsers will not be able to call the API")
	}
	handler, err := gateway.New(cfg)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "service", "gateway", "addr", srv.Addr, "core", cfg.CoreURL)
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
		slog.Info("shutting down", "service", "gateway")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
	return nil
}
