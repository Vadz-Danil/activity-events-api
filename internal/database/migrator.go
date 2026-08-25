package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"
)

type Migrator struct {
	dsn string
	dir string
	log *zap.Logger
}

func NewMigrator(dsn, dir string, log *zap.Logger) *Migrator {
	return &Migrator{dsn: dsn, dir: dir, log: log}
}

func (m *Migrator) Run(ctx context.Context, command string, args ...string) error {
	if _, err := os.Stat(m.dir); err != nil {
		return fmt.Errorf("migrator: migrations dir %q is not accessible: %w", m.dir, err)
	}

	goose.SetLogger(gooseLogger{m.log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrator: set goose dialect: %w", err)
	}

	if command == "create" || command == "fix" {
		goose.SetSequential(true)
		return goose.RunContext(ctx, command, nil, m.dir, args...)
	}

	db, err := sql.Open("pgx", m.dsn)
	if err != nil {
		return fmt.Errorf("migrator: connect to database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := waitForDB(ctx, db, m.log); err != nil {
		return err
	}

	m.log.Info("running migrations",
		zap.String("command", command),
		zap.String("dir", m.dir),
	)

	if err := goose.RunContext(ctx, command, db, m.dir, args...); err != nil {
		return fmt.Errorf("migrator: command %q: %w", command, err)
	}

	version, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("migrator: schema version: %w", err)
	}

	m.log.Info("done", zap.Int64("schema_version", version))
	return nil
}

func waitForDB(ctx context.Context, db *sql.DB, log *zap.Logger) error {
	const attempts = 10
	backoff := time.Second

	for attempt := 1; ; attempt++ {
		err := db.PingContext(ctx)
		if err == nil {
			return nil
		}
		if attempt >= attempts {
			return fmt.Errorf("migrator: database unavailable after %d attempts: %w", attempt, err)
		}

		log.Warn("database is unavailable, retrying",
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

type gooseLogger struct{ log *zap.Logger }

func (g gooseLogger) Printf(format string, v ...any) {
	g.log.Info(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.log.Fatal(strings.TrimSpace(fmt.Sprintf(format, v...)))
}
