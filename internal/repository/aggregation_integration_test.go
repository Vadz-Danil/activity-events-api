//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

const MaxIntegrationRuns = 100

var testBucket = time.Date(2020, 3, 15, 8, 0, 0, 0, time.UTC)

func addEvent(t *testing.T, pool *pgxpool.Pool, userID int64, eventType string, at time.Time) {
	t.Helper()

	_, _, err := NewEventRepository(pool).Create(context.Background(), models.Event{
		UserID:     userID,
		Type:       eventType,
		Payload:    json.RawMessage(`{}`),
		OccurredAt: at,
	})
	require.NoError(t, err)
}

func cleanupBucket(t *testing.T, pool *pgxpool.Pool, start time.Time) {
	t.Helper()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aggregation_runs WHERE bucket_start = $1`, start)
	})
}

func TestAggregateCountsEventsPerUserAndType(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)
	cleanupBucket(t, pool, testBucket)

	alice := createUser(t, pool, "aggregation.alice@example.com", true)
	bob := createUser(t, pool, "aggregation.bob@example.com", true)

	addEvent(t, pool, alice.ID, "page.view", testBucket.Add(10*time.Minute))
	addEvent(t, pool, alice.ID, "page.view", testBucket.Add(20*time.Minute))
	addEvent(t, pool, alice.ID, "button.click", testBucket.Add(30*time.Minute))
	addEvent(t, pool, bob.ID, "page.view", testBucket.Add(time.Hour))
	addEvent(t, pool, alice.ID, "page.view", testBucket.Add(4*time.Hour))

	touched, err := repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, touched)

	buckets, err := repo.Buckets(ctx, alice.ID, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)
	require.Len(t, buckets, 1)

	require.Equal(t, int64(3), buckets[0].EventCount, "the event at the upper bound belongs to the next bucket")
	require.Equal(t, map[string]int64{"page.view": 2, "button.click": 1}, buckets[0].TypeCounts)
	require.Equal(t, testBucket.Add(10*time.Minute).UTC(), buckets[0].FirstEventAt.UTC())
	require.Equal(t, testBucket.Add(30*time.Minute).UTC(), buckets[0].LastEventAt.UTC())
}

func TestAggregateIsIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)
	cleanupBucket(t, pool, testBucket)

	user := createUser(t, pool, "aggregation.idempotent@example.com", true)
	addEvent(t, pool, user.ID, "page.view", testBucket.Add(time.Minute))
	addEvent(t, pool, user.ID, "page.view", testBucket.Add(2*time.Minute))

	first, err := repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)

	second, err := repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)
	require.Equal(t, first, second)

	buckets, err := repo.Buckets(ctx, user.ID, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)
	require.Len(t, buckets, 1, "a rerun must update the row, not add another one")
	require.Equal(t, int64(2), buckets[0].EventCount)
}

func TestAggregatePicksUpEventsThatArrivedLate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)
	cleanupBucket(t, pool, testBucket)

	user := createUser(t, pool, "aggregation.late@example.com", true)
	addEvent(t, pool, user.ID, "page.view", testBucket.Add(time.Minute))

	_, err := repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)

	addEvent(t, pool, user.ID, "page.view", testBucket.Add(2*time.Minute))

	_, err = repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)

	buckets, err := repo.Buckets(ctx, user.ID, testBucket, testBucket.Add(4*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(2), buckets[0].EventCount)
}

func TestAggregateSkipsABucketAnotherRunnerHolds(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)
	cleanupBucket(t, pool, testBucket)

	user := createUser(t, pool, "aggregation.locked@example.com", true)
	addEvent(t, pool, user.ID, "page.view", testBucket.Add(time.Minute))

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()

	var locked bool
	err = holder.QueryRow(ctx,
		`SELECT pg_try_advisory_xact_lock(hashtextextended('aggregation:' || $1, 0))`,
		testBucket.UTC().Format(time.RFC3339),
	).Scan(&locked)
	require.NoError(t, err)
	require.True(t, locked)

	_, err = repo.Aggregate(ctx, testBucket, testBucket.Add(4*time.Hour))
	require.ErrorIs(t, err, ErrBucketLocked, "a second runner must report a skip instead of waiting")
}

func TestDailyRollupEqualsTheSumOfItsBuckets(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)

	dayStart := testBucket.Truncate(24 * time.Hour)
	for i := range 6 {
		cleanupBucket(t, pool, dayStart.Add(time.Duration(i)*4*time.Hour))
	}

	user := createUser(t, pool, "aggregation.daily@example.com", true)

	addEvent(t, pool, user.ID, "page.view", dayStart.Add(time.Hour))
	addEvent(t, pool, user.ID, "button.click", dayStart.Add(2*time.Hour))
	addEvent(t, pool, user.ID, "page.view", dayStart.Add(5*time.Hour))

	for i := range 2 {
		start := dayStart.Add(time.Duration(i) * 4 * time.Hour)
		_, err := repo.Aggregate(ctx, start, start.Add(4*time.Hour))
		require.NoError(t, err)
	}

	days, err := repo.Daily(ctx, user.ID, dayStart, dayStart.Add(24*time.Hour))
	require.NoError(t, err)
	require.Len(t, days, 1)

	require.Equal(t, int64(3), days[0].EventCount)
	require.Equal(t, map[string]int64{"page.view": 2, "button.click": 1}, days[0].TypeCounts)
	require.Equal(t, dayStart.UTC(), days[0].Day.UTC())
}

// The buckets sit far in the future so that the assertions hold whatever else the
// database already contains: the watermark is a maximum over the whole table.
func TestASkippedRunDoesNotMoveTheWatermark(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)

	succeeded := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	skipped := succeeded.Add(4 * time.Hour)
	cleanupBucket(t, pool, succeeded)
	cleanupBucket(t, pool, skipped)

	_, err := repo.RecordRun(ctx, models.AggregationRun{
		BucketStart: succeeded,
		BucketEnd:   succeeded.Add(4 * time.Hour),
		Status:      models.RunSucceeded,
		Trigger:     models.TriggerSchedule,
		StartedAt:   time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)

	_, err = repo.RecordRun(ctx, models.AggregationRun{
		BucketStart: skipped,
		BucketEnd:   skipped.Add(4 * time.Hour),
		Status:      models.RunSkipped,
		Trigger:     models.TriggerManual,
		StartedAt:   time.Now(),
	})
	require.NoError(t, err)

	last, err := repo.LastSucceededBucket(ctx)
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, succeeded.UTC(), last.UTC())
}

func TestRunHistoryReturnsTheNewestFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAggregationRepository(pool)
	cleanupBucket(t, pool, testBucket)
	cleanupBucket(t, pool, testBucket.Add(4*time.Hour))

	older, err := repo.RecordRun(ctx, models.AggregationRun{
		BucketStart: testBucket,
		BucketEnd:   testBucket.Add(4 * time.Hour),
		Status:      models.RunFailed,
		Trigger:     models.TriggerSchedule,
		StartedAt:   time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)

	newer, err := repo.RecordRun(ctx, models.AggregationRun{
		BucketStart: testBucket.Add(4 * time.Hour),
		BucketEnd:   testBucket.Add(8 * time.Hour),
		Status:      models.RunSkipped,
		Trigger:     models.TriggerManual,
		StartedAt:   time.Now(),
	})
	require.NoError(t, err)

	runs, err := repo.ListRuns(ctx, MaxIntegrationRuns)
	require.NoError(t, err)

	positions := map[int64]int{}
	for i, run := range runs {
		positions[run.ID] = i
	}

	require.Contains(t, positions, newer.ID)
	require.Contains(t, positions, older.ID)
	require.Less(t, positions[newer.ID], positions[older.ID])
}
