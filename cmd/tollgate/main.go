// Command tollgate runs the LLM gateway.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
	"github.com/sathwikbairaboina2/tollgate/internal/gateway"
	"github.com/sathwikbairaboina2/tollgate/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tollgate exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "tollgate.yaml", "path to the YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tp, shutdownTracing, err := telemetry.SetupTracing(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	gw, err := gateway.New(cfg, gateway.Options{Tracer: tp.Tracer("tollgate")})
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: gw.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() {
		slog.Info("tollgate listening", "addr", cfg.Listen, "routes", len(cfg.Routes), "keys", len(cfg.Keys))
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
