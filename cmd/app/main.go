package main

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/app"
	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/database"
	"github.com/Vadz-Danil/activity-events-api/internal/logger"
	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
	"github.com/Vadz-Danil/activity-events-api/internal/router"
)

var version = "dev"

func main() {
	stdlog.SetFlags(0)

	if err := run(); err != nil {
		stdlog.Fatalf("fatal: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()

	log.Info("starting application",
		zap.String("version", version),
		zap.String("env", cfg.App.Env),
		zap.String("mode", string(cfg.App.Mode)),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	deps, worker, err := app.BuildDeps(cfg, pool, metrics.New(string(cfg.App.Mode)), log, version)
	if err != nil {
		return err
	}

	var workerStopped chan struct{}
	if worker != nil {
		workerStopped = make(chan struct{})
		go func() {
			defer close(workerStopped)
			worker.Run(ctx)
		}()
	}

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr(),
		Handler:      router.New(deps),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server is listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, stopping")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	if workerStopped != nil {
		select {
		case <-workerStopped:
		case <-shutdownCtx.Done():
			log.Warn("worker did not stop within the shutdown timeout")
		}
	}

	log.Info("stopped gracefully")
	return nil
}
