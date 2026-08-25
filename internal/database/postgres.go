package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/config"
)

func Connect(ctx context.Context, cfg config.DB, log *zap.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: розбір DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: створення пулу: %w", err)
	}

	if err := ping(ctx, pool, cfg, log); err != nil {
		pool.Close()
		return nil, err
	}

	log.Info("підключення до postgres встановлено",
		zap.Int32("max_conns", cfg.MaxConns),
		zap.Int32("min_conns", cfg.MinConns),
	)
	return pool, nil
}

func ping(ctx context.Context, pool *pgxpool.Pool, cfg config.DB, log *zap.Logger) error {
	backoff := cfg.ConnectBackoff

	for attempt := 1; ; attempt++ {
		err := pool.Ping(ctx)
		if err == nil {
			return nil
		}
		if attempt >= cfg.ConnectRetries {
			return fmt.Errorf("database: не вдалося підключитися за %d спроб: %w", attempt, err)
		}

		log.Warn("база недоступна, повторна спроба",
			zap.Int("attempt", attempt),
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}
