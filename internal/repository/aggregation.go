package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type AggregationRepository struct {
	pool *pgxpool.Pool
}

func NewAggregationRepository(pool *pgxpool.Pool) *AggregationRepository {
	return &AggregationRepository{pool: pool}
}

func (r *AggregationRepository) Aggregate(ctx context.Context, start, end time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("repository: begin aggregation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locked, err := tryLockBucket(ctx, tx, start)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, ErrBucketLocked
	}

	const q = `
		WITH per_type AS (
			SELECT user_id,
			       type,
			       count(*)          AS cnt,
			       min(occurred_at)  AS first_at,
			       max(occurred_at)  AS last_at
			FROM events
			WHERE occurred_at >= $1 AND occurred_at < $2
			GROUP BY user_id, type
		), rolled AS (
			SELECT user_id,
			       sum(cnt)::bigint            AS event_count,
			       jsonb_object_agg(type, cnt) AS type_counts,
			       min(first_at)               AS first_event_at,
			       max(last_at)                AS last_event_at
			FROM per_type
			GROUP BY user_id
		)
		INSERT INTO activity_buckets
			(user_id, bucket_start, event_count, type_counts, first_event_at, last_event_at, computed_at)
		SELECT user_id, $1, event_count, type_counts, first_event_at, last_event_at, now()
		FROM rolled
		ON CONFLICT (user_id, bucket_start) DO UPDATE
		SET event_count    = EXCLUDED.event_count,
		    type_counts    = EXCLUDED.type_counts,
		    first_event_at = EXCLUDED.first_event_at,
		    last_event_at  = EXCLUDED.last_event_at,
		    computed_at    = EXCLUDED.computed_at`

	tag, err := tx.Exec(ctx, q, start, end)
	if err != nil {
		return 0, fmt.Errorf("repository: aggregate bucket: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("repository: commit aggregation: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *AggregationRepository) RecordRun(ctx context.Context, run models.AggregationRun) (*models.AggregationRun, error) {
	const q = `
		INSERT INTO aggregation_runs
			(bucket_start, bucket_end, status, trigger, users_touched, started_at, finished_at, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, bucket_start, bucket_end, status, trigger, users_touched, started_at, finished_at, error`

	return scanRun(r.pool.QueryRow(ctx, q,
		run.BucketStart, run.BucketEnd, run.Status, run.Trigger,
		run.UsersTouched, run.StartedAt, run.FinishedAt, run.Error,
	))
}

func (r *AggregationRepository) LastSucceededBucket(ctx context.Context) (*time.Time, error) {
	const q = `SELECT max(bucket_start) FROM aggregation_runs WHERE status = 'succeeded'`

	var bucket *time.Time
	if err := r.pool.QueryRow(ctx, q).Scan(&bucket); err != nil {
		return nil, fmt.Errorf("repository: read last aggregated bucket: %w", err)
	}
	return bucket, nil
}

func (r *AggregationRepository) EarliestEvent(ctx context.Context) (*time.Time, error) {
	const q = `SELECT min(occurred_at) FROM events`

	var earliest *time.Time
	if err := r.pool.QueryRow(ctx, q).Scan(&earliest); err != nil {
		return nil, fmt.Errorf("repository: read earliest event: %w", err)
	}
	return earliest, nil
}

func (r *AggregationRepository) ListRuns(ctx context.Context, limit int) ([]models.AggregationRun, error) {
	const q = `
		SELECT id, bucket_start, bucket_end, status, trigger, users_touched, started_at, finished_at, error
		FROM aggregation_runs
		ORDER BY started_at DESC, id DESC
		LIMIT $1`

	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list aggregation runs: %w", err)
	}
	defer rows.Close()

	runs := make([]models.AggregationRun, 0, limit)
	for rows.Next() {
		run, err := scanRunFrom(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read aggregation runs: %w", err)
	}

	return runs, nil
}

func (r *AggregationRepository) Buckets(ctx context.Context, userID int64, from, to time.Time) ([]models.ActivityBucket, error) {
	const q = `
		SELECT user_id, bucket_start, event_count, type_counts, first_event_at, last_event_at, computed_at
		FROM activity_buckets
		WHERE user_id = $1 AND bucket_start >= $2 AND bucket_start < $3
		ORDER BY bucket_start DESC`

	rows, err := r.pool.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("repository: list activity buckets: %w", err)
	}
	defer rows.Close()

	buckets := make([]models.ActivityBucket, 0, 8)
	for rows.Next() {
		var (
			b   models.ActivityBucket
			raw []byte
		)
		if err := rows.Scan(&b.UserID, &b.BucketStart, &b.EventCount, &raw,
			&b.FirstEventAt, &b.LastEventAt, &b.ComputedAt); err != nil {
			return nil, fmt.Errorf("repository: scan activity bucket: %w", err)
		}
		if err := json.Unmarshal(raw, &b.TypeCounts); err != nil {
			return nil, fmt.Errorf("repository: decode bucket type counts: %w", err)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read activity buckets: %w", err)
	}

	return buckets, nil
}

func (r *AggregationRepository) Daily(ctx context.Context, userID int64, from, to time.Time) ([]models.DailyActivity, error) {
	const q = `
		WITH per_day_type AS (
			SELECT date_trunc('day', b.bucket_start AT TIME ZONE 'UTC') AS day,
			       t.key                                                AS type,
			       sum(t.value::bigint)                                 AS cnt
			FROM activity_buckets b, jsonb_each_text(b.type_counts) t
			WHERE b.user_id = $1 AND b.bucket_start >= $2 AND b.bucket_start < $3
			GROUP BY 1, 2
		)
		SELECT day, sum(cnt)::bigint, jsonb_object_agg(type, cnt)
		FROM per_day_type
		GROUP BY day
		ORDER BY day DESC`

	rows, err := r.pool.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("repository: roll buckets up into days: %w", err)
	}
	defer rows.Close()

	days := make([]models.DailyActivity, 0, 8)
	for rows.Next() {
		var (
			d   models.DailyActivity
			raw []byte
		)
		if err := rows.Scan(&d.Day, &d.EventCount, &raw); err != nil {
			return nil, fmt.Errorf("repository: scan daily activity: %w", err)
		}
		if err := json.Unmarshal(raw, &d.TypeCounts); err != nil {
			return nil, fmt.Errorf("repository: decode daily type counts: %w", err)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read daily activity: %w", err)
	}

	return days, nil
}

func tryLockBucket(ctx context.Context, db pgx.Tx, bucket time.Time) (bool, error) {
	const q = `SELECT pg_try_advisory_xact_lock(hashtextextended('aggregation:' || $1, 0))`

	var locked bool
	if err := db.QueryRow(ctx, q, bucket.UTC().Format(time.RFC3339)).Scan(&locked); err != nil {
		return false, fmt.Errorf("repository: lock aggregation bucket: %w", err)
	}
	return locked, nil
}

func scanRun(row pgx.Row) (*models.AggregationRun, error) {
	var run models.AggregationRun

	err := row.Scan(&run.ID, &run.BucketStart, &run.BucketEnd, &run.Status, &run.Trigger,
		&run.UsersTouched, &run.StartedAt, &run.FinishedAt, &run.Error)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan aggregation run: %w", err)
	}

	return &run, nil
}

func scanRunFrom(rows pgx.Rows) (*models.AggregationRun, error) {
	var run models.AggregationRun

	if err := rows.Scan(&run.ID, &run.BucketStart, &run.BucketEnd, &run.Status, &run.Trigger,
		&run.UsersTouched, &run.StartedAt, &run.FinishedAt, &run.Error); err != nil {
		return nil, fmt.Errorf("repository: scan aggregation run: %w", err)
	}

	return &run, nil
}
