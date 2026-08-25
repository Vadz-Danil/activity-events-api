package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/database"
	"github.com/Vadz-Danil/activity-events-api/internal/logger"
)

const usage = `Міграції бази (goose).

Використання:
  migrate <команда> [аргументи]

Команди:
  up                  накотити всі міграції
  up-by-one           накотити одну наступну
  up-to <версія>      накотити до вказаної версії
  down                відкотити останню
  down-to <версія>    відкотити до вказаної версії
  redo                перекотити останню
  status              стан міграцій
  version             поточна версія схеми
  create <назва> sql  створити новий файл міграції

Оточення:
  DATABASE_URL        рядок підключення (обовʼязковий)
  MIGRATIONS_DIR      тека з міграціями (типово ./migrations)
`

func main() {
	stdlog.SetFlags(0)

	if err := run(); err != nil {
		stdlog.Fatalf("fatal: %v", err)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}

	cfg, err := config.Load()
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
