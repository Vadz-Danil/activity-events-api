package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

var testNow = time.Date(2026, 8, 26, 13, 20, 0, 0, time.UTC)

func newTestAggregation(t *testing.T, repo *fakeAggregation, backfill int) *Aggregation {
	t.Helper()

	return NewAggregation(AggregationDeps{
		Repo:     repo,
		Bucket:   4 * time.Hour,
		Backfill: backfill,
		Now:      func() time.Time { return testNow },
	})
}

func bucketAt(hour int) time.Time {
	return time.Date(2026, 8, 26, hour, 0, 0, 0, time.UTC)
}

func TestRunBucketRefusesAWindowThatHasNotClosed(t *testing.T) {
	repo := newFakeAggregation()
	svc := newTestAggregation(t, repo, 12)

	_, err := svc.RunBucket(context.Background(), testNow, models.TriggerManual)

	require.ErrorIs(t, err, ErrBucketNotClosed)
	require.Empty(t, repo.startedWindows(), "an open bucket must not reach the repository")
}

func TestRunBucketWithoutTimestampTakesTheNewestClosedWindow(t *testing.T) {
	repo := newFakeAggregation()
	svc := newTestAggregation(t, repo, 12)

	run, err := svc.RunBucket(context.Background(), time.Time{}, models.TriggerManual)

	require.NoError(t, err)
	require.Equal(t, bucketAt(8), run.BucketStart)
	require.Equal(t, bucketAt(12), run.BucketEnd)
}

func TestRunBucketRecordsSuccess(t *testing.T) {
	repo := newFakeAggregation()
	repo.touched = 7
	svc := newTestAggregation(t, repo, 12)

	run, err := svc.RunBucket(context.Background(), bucketAt(4), models.TriggerManual)

	require.NoError(t, err)
	require.Equal(t, models.RunSucceeded, run.Status)
	require.Equal(t, models.TriggerManual, run.Trigger)
	require.Equal(t, 7, run.UsersTouched)
	require.NotNil(t, run.FinishedAt)
	require.Nil(t, run.Error)
}

func TestRunBucketRecordsSkipWhenAnotherRunnerHoldsTheLock(t *testing.T) {
	repo := newFakeAggregation()
	repo.failures[bucketAt(4)] = repository.ErrBucketLocked
	svc := newTestAggregation(t, repo, 12)

	run, err := svc.RunBucket(context.Background(), bucketAt(4), models.TriggerSchedule)

	require.NoError(t, err)
	require.Equal(t, models.RunSkipped, run.Status)
	require.Zero(t, run.UsersTouched)
	require.Nil(t, run.Error)
}

func TestRunBucketRecordsFailureWithTheReason(t *testing.T) {
	repo := newFakeAggregation()
	repo.failures[bucketAt(4)] = errors.New("connection refused")
	svc := newTestAggregation(t, repo, 12)

	run, err := svc.RunBucket(context.Background(), bucketAt(4), models.TriggerSchedule)

	require.NoError(t, err)
	require.Equal(t, models.RunFailed, run.Status)
	require.NotNil(t, run.Error)
	require.Contains(t, *run.Error, "connection refused")
}

func TestRunBucketAlignsAnArbitraryTimestampToItsWindow(t *testing.T) {
	repo := newFakeAggregation()
	svc := newTestAggregation(t, repo, 12)

	run, err := svc.RunBucket(context.Background(), time.Date(2026, 8, 26, 6, 47, 13, 0, time.UTC), models.TriggerManual)

	require.NoError(t, err)
	require.Equal(t, bucketAt(4), run.BucketStart)
}

func TestRunPendingStartsFromTheEarliestEventWhenNothingRanBefore(t *testing.T) {
	repo := newFakeAggregation()
	earliest := time.Date(2026, 8, 26, 1, 10, 0, 0, time.UTC)
	repo.earliest = &earliest
	svc := newTestAggregation(t, repo, 12)

	runs, err := svc.RunPending(context.Background())

	require.NoError(t, err)
	require.Equal(t, []time.Time{bucketAt(0), bucketAt(4), bucketAt(8)}, repo.startedWindows())
	require.Len(t, runs, 3)
}

func TestRunPendingContinuesAfterTheLastSuccessfulBucket(t *testing.T) {
	repo := newFakeAggregation()
	earliest := time.Date(2026, 8, 26, 1, 10, 0, 0, time.UTC)
	repo.earliest = &earliest
	svc := newTestAggregation(t, repo, 12)

	_, err := svc.RunBucket(context.Background(), bucketAt(0), models.TriggerManual)
	require.NoError(t, err)

	repo.windows = nil
	runs, err := svc.RunPending(context.Background())

	require.NoError(t, err)
	require.Equal(t, []time.Time{bucketAt(4), bucketAt(8)}, repo.startedWindows())
	require.Len(t, runs, 2)
}

func TestRunPendingStopsAtTheFirstBucketThatDidNotSucceed(t *testing.T) {
	repo := newFakeAggregation()
	earliest := time.Date(2026, 8, 26, 1, 10, 0, 0, time.UTC)
	repo.earliest = &earliest
	repo.failures[bucketAt(4)] = errors.New("boom")
	svc := newTestAggregation(t, repo, 12)

	runs, err := svc.RunPending(context.Background())

	require.NoError(t, err)
	require.Equal(t, []time.Time{bucketAt(0), bucketAt(4)}, repo.startedWindows(),
		"the window after a failure must wait for the next pass")
	require.Len(t, runs, 2)
	require.Equal(t, models.RunFailed, runs[1].Status)
}

func TestRunPendingRespectsTheBackfillLimit(t *testing.T) {
	repo := newFakeAggregation()
	earliest := time.Date(2026, 8, 25, 1, 10, 0, 0, time.UTC)
	repo.earliest = &earliest
	svc := newTestAggregation(t, repo, 2)

	runs, err := svc.RunPending(context.Background())

	require.NoError(t, err)
	require.Len(t, runs, 2)
}

func TestRunPendingDoesNothingWithoutEvents(t *testing.T) {
	repo := newFakeAggregation()
	svc := newTestAggregation(t, repo, 12)

	runs, err := svc.RunPending(context.Background())

	require.NoError(t, err)
	require.Empty(t, runs)
	require.Empty(t, repo.startedWindows())
}

func TestStatsRejectsAnInvertedRange(t *testing.T) {
	svc := newTestAggregation(t, newFakeAggregation(), 12)

	_, err := svc.Stats(context.Background(), StatsQuery{
		UserID: 1,
		From:   testNow,
		To:     testNow.Add(-time.Hour),
	})

	require.ErrorIs(t, err, ErrInvalidTimeRange)
}

func TestStatsRejectsAWindowLongerThanTheLimit(t *testing.T) {
	svc := newTestAggregation(t, newFakeAggregation(), 12)

	_, err := svc.Stats(context.Background(), StatsQuery{
		UserID: 1,
		From:   testNow.Add(-MaxStatsRange - time.Hour),
		To:     testNow,
	})

	require.ErrorIs(t, err, ErrStatsRangeTooLarge)
}

func TestStatsDefaultsToTheLastWeekAlignedToABucket(t *testing.T) {
	svc := newTestAggregation(t, newFakeAggregation(), 12)

	stats, err := svc.Stats(context.Background(), StatsQuery{UserID: 1})

	require.NoError(t, err)
	require.Equal(t, testNow, stats.To)
	require.Equal(t, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), stats.From)
	require.Equal(t, 4*time.Hour, stats.Bucket)
}

func TestStatsReturnsDaysOnlyWhenAskedForThem(t *testing.T) {
	repo := newFakeAggregation()
	repo.buckets = []models.ActivityBucket{{UserID: 1, BucketStart: bucketAt(4), EventCount: 3}}
	repo.days = []models.DailyActivity{{Day: bucketAt(0), EventCount: 9}}
	svc := newTestAggregation(t, repo, 12)

	byBucket, err := svc.Stats(context.Background(), StatsQuery{UserID: 1})
	require.NoError(t, err)
	require.Len(t, byBucket.Buckets, 1)
	require.Empty(t, byBucket.Days)

	byDay, err := svc.Stats(context.Background(), StatsQuery{UserID: 1, Daily: true})
	require.NoError(t, err)
	require.Len(t, byDay.Days, 1)
	require.Empty(t, byDay.Buckets)
}
