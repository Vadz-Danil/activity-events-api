package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/database"
	"github.com/Vadz-Danil/activity-events-api/internal/logger"
)

const usage = `Database migrations (goose).

Usage:
  migrate <command> [arguments]

Commands:
  up                  apply all migrations
  up-by-one           apply the next one
  up-to <version>     apply up to the given version
  down                roll back the last one
  down-to <version>   roll back to the given version
  redo                re-apply the last one
  status              migration status
  version             current schema version
  create <name> sql   create a new migration file

Environment:
  DATABASE_URL        connection string (required)
  MIGRATIONS_DIR      migrations directory (defaults to ./migrations)
`

func main() {
	stdlog.SetFlags(0)

	if err := run(); err != nil {
		stdlog.Fatalf("fatal: %v", err)
	}
}

func run() error {
	_ = godotenv.Load()

	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}

	cfg, err := config.LoadMigrations()
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return database.NewMigrator(cfg.DB.URL, cfg.DB.MigrationsDir, log).Run(ctx, args[0], args[1:]...)
}
