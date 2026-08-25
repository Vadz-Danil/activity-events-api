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

	log.Info("старт застосунку",
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

	m := metrics.New(string(cfg.App.Mode))

	srv := &http.Server{
		Addr: cfg.HTTP.Addr(),
		Handler: router.New(router.Deps{
			Config:  cfg,
			Logger:  log,
			Pool:    pool,
			Metrics: m,
			Version: version,
		}),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http-сервер слухає", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http-сервер: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("отримано сигнал зупинки, завершуємо роботу")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("зупинено коректно")
	return nil
}
