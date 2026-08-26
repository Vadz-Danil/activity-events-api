package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

const (
	MaxBatchEvents       = 100
	DefaultPageSize      = 50
	MaxPageSize          = 200
	maxIdempotencyKeyLen = 128
	maxClockSkew         = 5 * time.Minute
	maxEventAge          = 90 * 24 * time.Hour
)

type EventRepository interface {
	Create(ctx context.Context, e models.Event) (*models.Event, bool, error)
	CreateBatch(ctx context.Context, events []models.Event) ([]models.Event, int, error)
	List(ctx context.Context, f repository.EventFilter) ([]models.Event, error)
}

type EventPublisher interface {
	Publish(e models.Event)
}

type Event struct {
	events    EventRepository
	publisher EventPublisher
	log       *zap.Logger
	now       func() time.Time
}

type EventDeps struct {
	Events    EventRepository
	Publisher EventPublisher
	Logger    *zap.Logger
	Now       func() time.Time
}

func NewEvent(d EventDeps) *Event {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	log := d.Logger
	if log == nil {
		log = zap.NewNop()
	}

	return &Event{events: d.Events, publisher: d.Publisher, log: log, now: now}
}

type EventInput struct {
	Type           string
	Payload        json.RawMessage
	OccurredAt     *time.Time
	IdempotencyKey string
}

type EventQuery struct {
	UserID int64
	Types  []string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
}

type EventPage struct {
	Items      []models.Event
	NextCursor string
}

type BatchResult struct {
	Created    []models.Event
	Duplicates int
}

func (s *Event) Record(ctx context.Context, userID int64, in EventInput) (*models.Event, bool, error) {
	event, err := s.normalize(userID, in)
	if err != nil {
		return nil, false, err
	}

	stored, created, err := s.events.Create(ctx, event)
	if err == nil && created {
		s.publish(*stored)
	}

	return stored, created, err
}

func (s *Event) publish(e models.Event) {
	if s.publisher != nil {
		s.publisher.Publish(e)
	}
}

func (s *Event) RecordBatch(ctx context.Context, userID int64, in []EventInput) (*BatchResult, error) {
	switch {
	case len(in) == 0:
		return nil, ErrEmptyBatch
	case len(in) > MaxBatchEvents:
		return nil, fmt.Errorf("%w: %d events, the limit is %d", ErrBatchTooLarge, len(in), MaxBatchEvents)
	}

	events := make([]models.Event, 0, len(in))
	keys := make(map[string]struct{}, len(in))

	for i, item := range in {
		event, err := s.normalize(userID, item)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}

		if event.IdempotencyKey != nil {
			if _, seen := keys[*event.IdempotencyKey]; seen {
				return nil, fmt.Errorf("event %d: %w", i, ErrDuplicateIdempotencyKey)
			}
			keys[*event.IdempotencyKey] = struct{}{}
		}

		events = append(events, event)
	}

	created, duplicates, err := s.events.CreateBatch(ctx, events)
	if err != nil {
		return nil, err
	}

	for _, e := range created {
		s.publish(e)
	}

	return &BatchResult{Created: created, Duplicates: duplicates}, nil
}

func (s *Event) List(ctx context.Context, q EventQuery) (*EventPage, error) {
	filter := repository.EventFilter{
		UserID: q.UserID,
		From:   q.From,
		To:     q.To,
		Limit:  pageSize(q.Limit),
	}

	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return nil, ErrInvalidTimeRange
	}

	for _, t := range q.Types {
		if t = models.NormalizeEventType(t); t != "" {
			filter.Types = append(filter.Types, t)
		}
	}

	if q.Cursor != "" {
		occurredAt, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, err
		}
		filter.BeforeTime, filter.BeforeID = &occurredAt, &id
	}

	filter.Limit++

	events, err := s.events.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	page := &EventPage{Items: events}
	if len(events) == filter.Limit {
		page.Items = events[:filter.Limit-1]
		page.NextCursor = encodeCursor(page.Items[len(page.Items)-1])
	}

	return page, nil
}

func (s *Event) normalize(userID int64, in EventInput) (models.Event, error) {
	eventType := models.NormalizeEventType(in.Type)
	if eventType == "" || len(eventType) > models.MaxEventTypeLen {
		return models.Event{}, ErrInvalidEventType
	}

	payload, err := normalizePayload(in.Payload)
	if err != nil {
		return models.Event{}, err
	}

	now := s.now()
	occurredAt := now
	if in.OccurredAt != nil {
		occurredAt = in.OccurredAt.UTC()
	}
	if occurredAt.After(now.Add(maxClockSkew)) || occurredAt.Before(now.Add(-maxEventAge)) {
		return models.Event{}, ErrEventTimeOutOfRange
	}

	event := models.Event{
		UserID:     userID,
		Type:       eventType,
		Payload:    payload,
		OccurredAt: occurredAt,
	}

	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if len(key) > maxIdempotencyKeyLen {
			return models.Event{}, ErrInvalidIdempotencyKey
		}
		event.IdempotencyKey = &key
	}

	return event, nil
}

func normalizePayload(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}"), nil
	}
	if len(trimmed) > models.MaxPayloadBytes {
		return nil, ErrInvalidEventPayload
	}

	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, ErrInvalidEventPayload
	}

	return trimmed, nil
}

func pageSize(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPageSize
	case limit > MaxPageSize:
		return MaxPageSize
	default:
		return limit
	}
}

func encodeCursor(e models.Event) string {
	raw := strconv.FormatInt(e.OccurredAt.UTC().UnixNano(), 10) + ":" + strconv.FormatInt(e.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}

	occurredAt, id, found := strings.Cut(string(raw), ":")
	if !found {
		return time.Time{}, 0, ErrInvalidCursor
	}

	nanos, err := strconv.ParseInt(occurredAt, 10, 64)
	if err != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}
	parsedID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}

	return time.Unix(0, nanos).UTC(), parsedID, nil
}
