package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"math/rand/v2"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/database"
	"github.com/Vadz-Danil/activity-events-api/internal/logger"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

const usage = `Demo data for a fresh database.

Usage:
  seed

Environment:
  DATABASE_URL     connection string (required)
  SEED_EMAIL              admin account, defaults to demo@example.com
  SEED_PASSWORD           admin password, defaults to demo-password-123
  SEED_ROLE               user or admin, defaults to admin
  SEED_VIEWER_EMAIL       second account, defaults to viewer@example.com
  SEED_VIEWER_PASSWORD    second password, defaults to viewer-password-123
  SEED_DAYS               how many days back to fill, defaults to 7
  SEED_EVENTS             how many events to generate, defaults to 600

Two accounts are created on purpose: the admin one to demonstrate ?user_id= and
the aggregation panel, the plain one to show what a regular user is limited to.
`

const (
	batchSize = 100
	day       = 24 * time.Hour

	seedBcryptCost = 10

	defaultDays   = 7
	defaultEvents = 600

	viewerEventsDivisor = 3

	minUploadKB = 50
	maxUploadKB = 4000
)

var eventTypes = []struct {
	name   string
	weight int
}{
	{"page.view", 50},
	{"button.click", 20},
	{"form.submit", 10},
	{"search.query", 8},
	{"file.upload", 5},
	{"settings.change", 4},
	{"session.start", 3},
}

var hourWeight = [24]int{1, 1, 1, 1, 1, 2, 4, 8, 14, 18, 20, 18, 12, 16, 20, 19, 16, 12, 9, 7, 6, 5, 3, 2}

func main() {
	stdlog.SetFlags(0)

	if len(os.Args) > 1 {
		fmt.Print(usage)
		return
	}

	if err := run(); err != nil {
		stdlog.Fatalf("fatal: %v", err)
	}
}

func run() error {
	// .env потрібен лише для локального запуску: у Docker змінні вже в оточенні процесу.
	_ = godotenv.Load()

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

	pool, err := database.Connect(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	role := models.Role(env("SEED_ROLE", string(models.RoleAdmin)))
	if !role.Valid() {
		return fmt.Errorf("seed: SEED_ROLE %q must be user or admin", role)
	}

	admin, err := demoUser(ctx, pool, log, account{
		email:    env("SEED_EMAIL", "demo@example.com"),
		password: env("SEED_PASSWORD", "demo-password-123"),
		role:     role,
	})
	if err != nil {
		return err
	}

	viewer, err := demoUser(ctx, pool, log, account{
		email:    env("SEED_VIEWER_EMAIL", "viewer@example.com"),
		password: env("SEED_VIEWER_PASSWORD", "viewer-password-123"),
		role:     models.RoleUser,
	})
	if err != nil {
		return err
	}

	days, total := envInt("SEED_DAYS", defaultDays), envInt("SEED_EVENTS", defaultEvents)

	for _, seeded := range []struct {
		user  *models.User
		count int
	}{
		{admin, total},
		{viewer, total / viewerEventsDivisor},
	} {
		created, err := fill(ctx, pool, seeded.user.ID, days, seeded.count)
		if err != nil {
			return err
		}
		log.Info("events generated",
			zap.String("email", seeded.user.Email),
			zap.Int("created", created),
		)
	}

	buckets, err := aggregate(ctx, pool, log, days)
	if err != nil {
		return err
	}

	log.Info("demo data is ready",
		zap.String("admin", admin.Email),
		zap.String("viewer", viewer.Email),
		zap.Int("buckets_aggregated", buckets),
	)
	return nil
}

type account struct {
	email    string
	password string
	role     models.Role
}

func demoUser(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger, spec account) (*models.User, error) {
	users := repository.NewUserRepository(pool)

	existing, err := users.ByEmail(ctx, spec.email)
	switch {
	case err == nil:
		log.Info("reusing the demo account", zap.String("email", spec.email))
		return existing, nil
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}

	hash, err := auth.HashPassword(spec.password, seedBcryptCost)
	if err != nil {
		return nil, err
	}

	created, err := users.Create(ctx, models.User{
		Email:         spec.email,
		PasswordHash:  &hash,
		Role:          spec.role,
		EmailVerified: true,
	})
	if err != nil {
		return nil, err
	}

	log.Info("demo account created",
		zap.String("email", spec.email),
		zap.String("role", string(spec.role)),
	)
	return created, nil
}

func fill(ctx context.Context, pool *pgxpool.Pool, userID int64, days, total int) (int, error) {
	events := repository.NewEventRepository(pool)
	now := time.Now().UTC()

	created := 0
	for sent := 0; sent < total; sent += batchSize {
		size := min(batchSize, total-sent)

		batch := make([]models.Event, 0, size)
		for i := range size {
			eventType := pickType()
			key := fmt.Sprintf("seed-%d-%d", now.Unix(), sent+i)

			batch = append(batch, models.Event{
				UserID:         userID,
				Type:           eventType,
				Payload:        payloadFor(eventType),
				OccurredAt:     pickMoment(now, days),
				IdempotencyKey: &key,
			})
		}

		stored, _, err := events.CreateBatch(ctx, batch)
		if err != nil {
			return created, err
		}
		created += len(stored)
	}

	return created, nil
}

func aggregate(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger, days int) (int, error) {
	bucket := envDuration("AGGREGATION_BUCKET", models.BucketDuration)

	aggregation := service.NewAggregation(service.AggregationDeps{
		Repo:   repository.NewAggregationRepository(pool),
		Logger: log,
		Bucket: bucket,
	})

	now := time.Now().UTC()
	from := models.BucketStart(now.Add(-time.Duration(days)*day), bucket)
	newestClosed := models.BucketStart(now, bucket).Add(-bucket)

	total := 0
	for _, start := range models.BucketsBetween(from, newestClosed, bucket) {
		run, err := aggregation.RunBucket(ctx, start, models.TriggerManual)
		if err != nil {
			return total, err
		}
		if run.Status == models.RunFailed {
			return total, fmt.Errorf("seed: aggregating the window at %s failed", run.BucketStart)
		}
		total++
	}

	return total, nil
}

func pickType() string {
	total := 0
	for _, t := range eventTypes {
		total += t.weight
	}

	n := rand.IntN(total)
	for _, t := range eventTypes {
		if n < t.weight {
			return t.name
		}
		n -= t.weight
	}

	return eventTypes[0].name
}

func pickMoment(now time.Time, days int) time.Time {
	total := 0
	for _, w := range hourWeight {
		total += w
	}

	n := rand.IntN(total)
	hour := 0
	for h, w := range hourWeight {
		if n < w {
			hour = h
			break
		}
		n -= w
	}

	midnight := now.Add(-time.Duration(rand.IntN(days)) * day).Truncate(day)
	moment := midnight.Add(time.Duration(hour)*time.Hour +
		time.Duration(rand.IntN(60))*time.Minute +
		time.Duration(rand.IntN(60))*time.Second)

	if moment.After(now) {
		return now.Add(-time.Duration(rand.IntN(int(time.Hour/time.Second))) * time.Second)
	}
	return moment
}

func payloadFor(eventType string) json.RawMessage {
	switch eventType {
	case "page.view":
		pages := []string{"/", "/events", "/stats", "/settings"}
		return json.RawMessage(fmt.Sprintf(`{"path":%q}`, pages[rand.IntN(len(pages))]))
	case "search.query":
		terms := []string{"activity", "report", "export", "api"}
		return json.RawMessage(fmt.Sprintf(`{"term":%q}`, terms[rand.IntN(len(terms))]))
	case "file.upload":
		return json.RawMessage(fmt.Sprintf(`{"size_kb":%d}`, minUploadKB+rand.IntN(maxUploadKB-minUploadKB)))
	default:
		return json.RawMessage(`{}`)
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}
