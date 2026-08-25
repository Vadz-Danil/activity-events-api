package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type EventRepository struct {
	pool *pgxpool.Pool
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

type EventFilter struct {
	UserID     int64
	Types      []string
	From       *time.Time
	To         *time.Time
	Limit      int
	BeforeTime *time.Time
	BeforeID   *int64
}

func (r *EventRepository) Create(ctx context.Context, e models.Event) (*models.Event, bool, error) {
	const q = `
		INSERT INTO events (user_id, type, payload, occurred_at, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING id, user_id, type, payload, occurred_at, created_at, idempotency_key`

	created, err := scanEvent(r.pool.QueryRow(ctx, q, e.UserID, e.Type, e.Payload, e.OccurredAt, e.IdempotencyKey))
	switch {
	case err == nil:
		return created, true, nil
	case !errors.Is(err, ErrNotFound) || e.IdempotencyKey == nil:
		return nil, false, err
	}

	existing, err := r.byIdempotencyKey(ctx, e.UserID, *e.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (r *EventRepository) CreateBatch(ctx context.Context, events []models.Event) ([]models.Event, int, error) {
	if len(events) == 0 {
		return nil, 0, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("repository: begin event batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stored := make([]models.Event, 0, len(events))
	duplicates := 0

	for _, e := range events {
		const q = `
			INSERT INTO events (user_id, type, payload, occurred_at, idempotency_key)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
			RETURNING id, user_id, type, payload, occurred_at, created_at, idempotency_key`

		created, err := scanEvent(tx.QueryRow(ctx, q, e.UserID, e.Type, e.Payload, e.OccurredAt, e.IdempotencyKey))
		switch {
		case err == nil:
			stored = append(stored, *created)
		case errors.Is(err, ErrNotFound):
			duplicates++
		default:
			return nil, 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("repository: commit event batch: %w", err)
	}
	return stored, duplicates, nil
}

func (r *EventRepository) List(ctx context.Context, f EventFilter) ([]models.Event, error) {
	const q = `
		SELECT id, user_id, type, payload, occurred_at, created_at, idempotency_key
		FROM events
		WHERE user_id = $1
		  AND ($2::text[] IS NULL OR type = ANY ($2))
		  AND ($3::timestamptz IS NULL OR occurred_at >= $3)
		  AND ($4::timestamptz IS NULL OR occurred_at < $4)
		  AND ($5::timestamptz IS NULL OR (occurred_at, id) < ($5, $6))
		ORDER BY occurred_at DESC, id DESC
		LIMIT $7`

	rows, err := r.pool.Query(ctx, q, f.UserID, f.Types, f.From, f.To, f.BeforeTime, f.BeforeID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list events: %w", err)
	}
	defer rows.Close()

	events := make([]models.Event, 0, f.Limit)
	for rows.Next() {
		var e models.Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.Type, &e.Payload, &e.OccurredAt, &e.CreatedAt, &e.IdempotencyKey); err != nil {
			return nil, fmt.Errorf("repository: scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read events: %w", err)
	}

	return events, nil
}

func (r *EventRepository) byIdempotencyKey(ctx context.Context, userID int64, key string) (*models.Event, error) {
	const q = `
		SELECT id, user_id, type, payload, occurred_at, created_at, idempotency_key
		FROM events
		WHERE user_id = $1 AND idempotency_key = $2`

	return scanEvent(r.pool.QueryRow(ctx, q, userID, key))
}

func scanEvent(row pgx.Row) (*models.Event, error) {
	var e models.Event

	err := row.Scan(&e.ID, &e.UserID, &e.Type, &e.Payload, &e.OccurredAt, &e.CreatedAt, &e.IdempotencyKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan event: %w", err)
	}

	return &e, nil
}
