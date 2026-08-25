package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testUserID = int64(7)

type eventFixture struct {
	svc    *Event
	events *fakeEvents
	clock  *testClock
}

func newEventFixture(t *testing.T) eventFixture {
	t.Helper()

	events := newFakeEvents()
	clock := &testClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)}

	svc := NewEvent(EventDeps{Events: events, Logger: zap.NewNop(), Now: clock.Now})

	return eventFixture{svc: svc, events: events, clock: clock}
}

func TestRecordEvent(t *testing.T) {
	f := newEventFixture(t)

	event, created, err := f.svc.Record(context.Background(), testUserID, EventInput{Type: "  Page_View "})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "page_view", event.Type)
	require.Equal(t, testUserID, event.UserID)
	require.Equal(t, f.clock.Now(), event.OccurredAt)
	require.JSONEq(t, "{}", string(event.Payload))
	require.Nil(t, event.IdempotencyKey)
}

func TestRecordEventKeepsClientTimeAndPayload(t *testing.T) {
	f := newEventFixture(t)

	occurred := f.clock.Now().Add(-2 * time.Hour)

	event, _, err := f.svc.Record(context.Background(), testUserID, EventInput{
		Type:       "click",
		Payload:    json.RawMessage(`{"button":"save"}`),
		OccurredAt: &occurred,
	})
	require.NoError(t, err)
	require.Equal(t, occurred, event.OccurredAt)
	require.JSONEq(t, `{"button":"save"}`, string(event.Payload))
}

func TestRecordEventRejects(t *testing.T) {
	tests := []struct {
		name  string
		input EventInput
		want  error
	}{
		{"empty type", EventInput{Type: "   "}, ErrInvalidEventType},
		{"type over the limit", EventInput{Type: strings.Repeat("a", 65)}, ErrInvalidEventType},
		{"payload is not an object", EventInput{Type: "click", Payload: json.RawMessage(`[1,2]`)}, ErrInvalidEventPayload},
		{"payload is broken json", EventInput{Type: "click", Payload: json.RawMessage(`{"a":`)}, ErrInvalidEventPayload},
		{"payload over the limit", EventInput{Type: "click", Payload: oversizedPayload()}, ErrInvalidEventPayload},
		{"idempotency key over the limit", EventInput{Type: "click", IdempotencyKey: strings.Repeat("k", 129)}, ErrInvalidIdempotencyKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEventFixture(t)

			_, _, err := f.svc.Record(context.Background(), testUserID, tt.input)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestRecordEventRejectsTimeOutOfRange(t *testing.T) {
	f := newEventFixture(t)

	future := f.clock.Now().Add(time.Hour)
	_, _, err := f.svc.Record(context.Background(), testUserID, EventInput{Type: "click", OccurredAt: &future})
	require.ErrorIs(t, err, ErrEventTimeOutOfRange)

	ancient := f.clock.Now().Add(-maxEventAge - time.Hour)
	_, _, err = f.svc.Record(context.Background(), testUserID, EventInput{Type: "click", OccurredAt: &ancient})
	require.ErrorIs(t, err, ErrEventTimeOutOfRange)

	withinSkew := f.clock.Now().Add(time.Minute)
	_, _, err = f.svc.Record(context.Background(), testUserID, EventInput{Type: "click", OccurredAt: &withinSkew})
	require.NoError(t, err)
}

func TestRecordEventIsIdempotent(t *testing.T) {
	f := newEventFixture(t)

	first, created, err := f.svc.Record(context.Background(), testUserID, EventInput{
		Type:           "click",
		IdempotencyKey: "retry-1",
	})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := f.svc.Record(context.Background(), testUserID, EventInput{
		Type:           "click",
		IdempotencyKey: "retry-1",
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, f.events.rows, 1)

	_, created, err = f.svc.Record(context.Background(), testUserID+1, EventInput{
		Type:           "click",
		IdempotencyKey: "retry-1",
	})
	require.NoError(t, err)
	require.True(t, created, "an idempotency key belongs to one user only")
}

func TestRecordBatch(t *testing.T) {
	f := newEventFixture(t)

	_, err := f.svc.RecordBatch(context.Background(), testUserID, nil)
	require.ErrorIs(t, err, ErrEmptyBatch)

	tooMany := make([]EventInput, MaxBatchEvents+1)
	for i := range tooMany {
		tooMany[i] = EventInput{Type: "click"}
	}
	_, err = f.svc.RecordBatch(context.Background(), testUserID, tooMany)
	require.ErrorIs(t, err, ErrBatchTooLarge)

	_, err = f.svc.RecordBatch(context.Background(), testUserID, []EventInput{
		{Type: "click", IdempotencyKey: "same"},
		{Type: "click", IdempotencyKey: "same"},
	})
	require.ErrorIs(t, err, ErrDuplicateIdempotencyKey)

	_, err = f.svc.RecordBatch(context.Background(), testUserID, []EventInput{
		{Type: "click"},
		{Type: ""},
	})
	require.ErrorIs(t, err, ErrInvalidEventType)
	require.Empty(t, f.events.rows, "a batch with a broken event must not store anything")

	result, err := f.svc.RecordBatch(context.Background(), testUserID, []EventInput{
		{Type: "click", IdempotencyKey: "a"},
		{Type: "view"},
	})
	require.NoError(t, err)
	require.Len(t, result.Created, 2)
	require.Zero(t, result.Duplicates)

	result, err = f.svc.RecordBatch(context.Background(), testUserID, []EventInput{
		{Type: "click", IdempotencyKey: "a"},
		{Type: "scroll"},
	})
	require.NoError(t, err)
	require.Len(t, result.Created, 1)
	require.Equal(t, 1, result.Duplicates)
}

func TestListEventsPaginates(t *testing.T) {
	f := newEventFixture(t)

	for i := range 7 {
		occurred := f.clock.Now().Add(-time.Duration(i) * time.Minute)
		_, _, err := f.svc.Record(context.Background(), testUserID, EventInput{Type: "click", OccurredAt: &occurred})
		require.NoError(t, err)
	}
	_, _, err := f.svc.Record(context.Background(), testUserID+1, EventInput{Type: "click"})
	require.NoError(t, err)

	seen := map[int64]struct{}{}
	cursor := ""
	pages := 0

	for {
		page, err := f.svc.List(context.Background(), EventQuery{UserID: testUserID, Limit: 3, Cursor: cursor})
		require.NoError(t, err)
		pages++

		for _, event := range page.Items {
			require.Equal(t, testUserID, event.UserID, "a feed must not leak another user's events")
			_, duplicate := seen[event.ID]
			require.False(t, duplicate, "keyset pagination must not repeat an event")
			seen[event.ID] = struct{}{}
		}

		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		require.Less(t, pages, 10)
	}

	require.Len(t, seen, 7)
	require.Equal(t, 3, pages)
}

func TestListEventsFilters(t *testing.T) {
	f := newEventFixture(t)

	base := f.clock.Now()
	for i, eventType := range []string{"click", "view", "click"} {
		occurred := base.Add(-time.Duration(i) * time.Hour)
		_, _, err := f.svc.Record(context.Background(), testUserID, EventInput{Type: eventType, OccurredAt: &occurred})
		require.NoError(t, err)
	}

	page, err := f.svc.List(context.Background(), EventQuery{UserID: testUserID, Types: []string{" Click "}})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)

	from := base.Add(-90 * time.Minute)
	to := base.Add(-30 * time.Minute)
	page, err = f.svc.List(context.Background(), EventQuery{UserID: testUserID, From: &from, To: &to})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "view", page.Items[0].Type)

	_, err = f.svc.List(context.Background(), EventQuery{UserID: testUserID, From: &to, To: &from})
	require.ErrorIs(t, err, ErrInvalidTimeRange)

	_, err = f.svc.List(context.Background(), EventQuery{UserID: testUserID, Cursor: "not-a-cursor!"})
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func TestListEventsClampsPageSize(t *testing.T) {
	require.Equal(t, DefaultPageSize, pageSize(0))
	require.Equal(t, DefaultPageSize, pageSize(-5))
	require.Equal(t, MaxPageSize, pageSize(MaxPageSize+1))
	require.Equal(t, 10, pageSize(10))
}

func oversizedPayload() json.RawMessage {
	return json.RawMessage(`{"blob":"` + strings.Repeat("x", 9*1024) + `"}`)
}
