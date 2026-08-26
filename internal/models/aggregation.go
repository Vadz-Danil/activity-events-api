package models

import (
	"time"
)

const BucketDuration = 4 * time.Hour

type RunStatus string

const (
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunSkipped   RunStatus = "skipped"
)

type RunTrigger string

const (
	TriggerSchedule RunTrigger = "schedule"
	TriggerManual   RunTrigger = "manual"
)

type ActivityBucket struct {
	UserID       int64
	BucketStart  time.Time
	EventCount   int64
	TypeCounts   map[string]int64
	FirstEventAt time.Time
	LastEventAt  time.Time
	ComputedAt   time.Time
}

type AggregationRun struct {
	ID           int64
	BucketStart  time.Time
	BucketEnd    time.Time
	Status       RunStatus
	Trigger      RunTrigger
	UsersTouched int
	StartedAt    time.Time
	FinishedAt   *time.Time
	Error        *string
}

type DailyActivity struct {
	Day        time.Time
	EventCount int64
	TypeCounts map[string]int64
}

func BucketStart(t time.Time, size time.Duration) time.Time {
	return t.UTC().Truncate(size)
}

func BucketsBetween(from, to time.Time, size time.Duration) []time.Time {
	from, to = BucketStart(from, size), BucketStart(to, size)

	var buckets []time.Time
	for b := from; !b.After(to); b = b.Add(size) {
		buckets = append(buckets, b)
	}
	return buckets
}
