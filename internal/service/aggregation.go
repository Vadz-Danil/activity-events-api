package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

const (
	DefaultRunsLimit = 20
	MaxRunsLimit     = 100
	MaxStatsRange    = 90 * 24 * time.Hour
)

type AggregationRepository interface {
	Aggregate(ctx context.Context, start, end time.Time) (int, error)
	RecordRun(ctx context.Context, run models.AggregationRun) (*models.AggregationRun, error)
	LastSucceededBucket(ctx context.Context) (*time.Time, error)
	EarliestEvent(ctx context.Context) (*time.Time, error)
	ListRuns(ctx context.Context, limit int) ([]models.AggregationRun, error)
	Buckets(ctx context.Context, userID int64, from, to time.Time) ([]models.ActivityBucket, error)
	Daily(ctx context.Context, userID int64, from, to time.Time) ([]models.DailyActivity, error)
}

type Aggregation struct {
	repo     AggregationRepository
	log      *zap.Logger
	now      func() time.Time
	bucket   time.Duration
	backfill int
}

type AggregationDeps struct {
	Repo     AggregationRepository
	Logger   *zap.Logger
	Now      func() time.Time
	Bucket   time.Duration
	Backfill int
}

func NewAggregation(d AggregationDeps) *Aggregation {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	log := d.Logger
	if log == nil {
		log = zap.NewNop()
	}
	bucket := d.Bucket
	if bucket <= 0 {
		bucket = models.BucketDuration
	}
	backfill := d.Backfill
	if backfill <= 0 {
		backfill = 12
	}

	return &Aggregation{repo: d.Repo, log: log, now: now, bucket: bucket, backfill: backfill}
}

type StatsQuery struct {
	UserID int64
	From   time.Time
	To     time.Time
	Daily  bool
}

type Stats struct {
	Bucket  time.Duration
	From    time.Time
	To      time.Time
	Buckets []models.ActivityBucket
	Days    []models.DailyActivity
}

func (s *Aggregation) RunBucket(ctx context.Context, at time.Time, trigger models.RunTrigger) (*models.AggregationRun, error) {
	if at.IsZero() {
		at = s.now().UTC().Add(-s.bucket)
	}

	start := models.BucketStart(at, s.bucket)
	end := start.Add(s.bucket)

	if end.After(s.now().UTC()) {
		return nil, ErrBucketNotClosed
	}

	run := models.AggregationRun{
		BucketStart: start,
		BucketEnd:   end,
		Trigger:     trigger,
		StartedAt:   s.now().UTC(),
	}

	touched, err := s.repo.Aggregate(ctx, start, end)
	switch {
	case errors.Is(err, repository.ErrBucketLocked):
		run.Status = models.RunSkipped
	case err != nil:
		message := err.Error()
		run.Status, run.Error = models.RunFailed, &message
		s.log.Error("aggregation failed",
			zap.Time("bucket_start", start),
			zap.String("trigger", string(trigger)),
			zap.Error(err),
		)
	default:
		run.Status, run.UsersTouched = models.RunSucceeded, touched
	}

	finished := s.now().UTC()
	run.FinishedAt = &finished

	return s.repo.RecordRun(ctx, run)
}

func (s *Aggregation) RunPending(ctx context.Context) ([]models.AggregationRun, error) {
	pending, err := s.pendingBuckets(ctx)
	if err != nil || len(pending) == 0 {
		return nil, err
	}

	runs := make([]models.AggregationRun, 0, len(pending))
	for _, start := range pending {
		run, err := s.RunBucket(ctx, start, models.TriggerSchedule)
		if err != nil {
			return runs, err
		}

		runs = append(runs, *run)
		if run.Status != models.RunSucceeded {
			break
		}
	}

	return runs, nil
}

func (s *Aggregation) pendingBuckets(ctx context.Context) ([]time.Time, error) {
	newestClosed := models.BucketStart(s.now().UTC(), s.bucket).Add(-s.bucket)

	last, err := s.repo.LastSucceededBucket(ctx)
	if err != nil {
		return nil, err
	}

	var from time.Time
	switch {
	case last != nil:
		from = last.UTC().Add(s.bucket)
	default:
		earliest, err := s.repo.EarliestEvent(ctx)
		if err != nil {
			return nil, err
		}
		if earliest == nil {
			return nil, nil
		}
		from = models.BucketStart(*earliest, s.bucket)
	}

	if from.After(newestClosed) {
		return nil, nil
	}

	buckets := models.BucketsBetween(from, newestClosed, s.bucket)
	if len(buckets) > s.backfill {
		buckets = buckets[:s.backfill]
	}

	return buckets, nil
}

func (s *Aggregation) Runs(ctx context.Context, limit int) ([]models.AggregationRun, error) {
	switch {
	case limit <= 0:
		limit = DefaultRunsLimit
	case limit > MaxRunsLimit:
		limit = MaxRunsLimit
	}

	return s.repo.ListRuns(ctx, limit)
}

func (s *Aggregation) Stats(ctx context.Context, q StatsQuery) (*Stats, error) {
	to := q.To.UTC()
	if to.IsZero() {
		to = s.now().UTC()
	}
	from := q.From.UTC()
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}

	if !from.Before(to) {
		return nil, ErrInvalidTimeRange
	}
	if to.Sub(from) > MaxStatsRange {
		return nil, fmt.Errorf("%w: the window is longer than %s", ErrStatsRangeTooLarge, MaxStatsRange)
	}

	from = models.BucketStart(from, s.bucket)
	stats := &Stats{Bucket: s.bucket, From: from, To: to}

	if q.Daily {
		days, err := s.repo.Daily(ctx, q.UserID, from, to)
		if err != nil {
			return nil, err
		}
		stats.Days = days
		return stats, nil
	}

	buckets, err := s.repo.Buckets(ctx, q.UserID, from, to)
	if err != nil {
		return nil, err
	}
	stats.Buckets = buckets

	return stats, nil
}
