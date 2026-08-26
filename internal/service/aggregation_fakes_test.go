package service

import (
	"context"
	"sync"
	"time"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type fakeAggregation struct {
	mu sync.Mutex

	seq      int64
	runs     []models.AggregationRun
	windows  [][2]time.Time
	touched  int
	earliest *time.Time
	failures map[time.Time]error

	buckets []models.ActivityBucket
	days    []models.DailyActivity
}

func newFakeAggregation() *fakeAggregation {
	return &fakeAggregation{touched: 1, failures: map[time.Time]error{}}
}

func (f *fakeAggregation) Aggregate(_ context.Context, start, end time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.windows = append(f.windows, [2]time.Time{start, end})
	if err, ok := f.failures[start.UTC()]; ok {
		return 0, err
	}

	return f.touched, nil
}

func (f *fakeAggregation) RecordRun(_ context.Context, run models.AggregationRun) (*models.AggregationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.seq++
	run.ID = f.seq
	f.runs = append(f.runs, run)

	stored := run
	return &stored, nil
}

func (f *fakeAggregation) LastSucceededBucket(_ context.Context) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var last *time.Time
	for _, run := range f.runs {
		if run.Status != models.RunSucceeded {
			continue
		}
		if last == nil || run.BucketStart.After(*last) {
			bucket := run.BucketStart
			last = &bucket
		}
	}

	return last, nil
}

func (f *fakeAggregation) EarliestEvent(_ context.Context) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.earliest, nil
}

func (f *fakeAggregation) ListRuns(_ context.Context, limit int) ([]models.AggregationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.runs) > limit {
		return f.runs[:limit], nil
	}
	return f.runs, nil
}

func (f *fakeAggregation) Buckets(_ context.Context, _ int64, _, _ time.Time) ([]models.ActivityBucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.buckets, nil
}

func (f *fakeAggregation) Daily(_ context.Context, _ int64, _, _ time.Time) ([]models.DailyActivity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.days, nil
}

func (f *fakeAggregation) startedWindows() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	starts := make([]time.Time, 0, len(f.windows))
	for _, w := range f.windows {
		starts = append(starts, w[0])
	}
	return starts
}
