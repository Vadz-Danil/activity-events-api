package scheduler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type Aggregator interface {
	RunPending(ctx context.Context) ([]models.AggregationRun, error)
}

type Scheduler struct {
	aggregator Aggregator
	log        *zap.Logger
	tick       time.Duration
}

type Deps struct {
	Aggregator Aggregator
	Logger     *zap.Logger
	Tick       time.Duration
}

func New(d Deps) *Scheduler {
	log := d.Logger
	if log == nil {
		log = zap.NewNop()
	}
	tick := d.Tick
	if tick <= 0 {
		tick = 5 * time.Minute
	}

	return &Scheduler{aggregator: d.Aggregator, log: log, tick: tick}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.log.Info("aggregation scheduler started", zap.Duration("tick", s.tick))

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	s.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			s.log.Info("aggregation scheduler stopped")
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	runs, err := s.aggregator.RunPending(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.Error("aggregation pass failed", zap.Error(err))
	}

	for _, run := range runs {
		s.log.Info("bucket aggregated",
			zap.Time("bucket_start", run.BucketStart),
			zap.String("status", string(run.Status)),
			zap.Int("users_touched", run.UsersTouched),
		)
	}
}
