package service

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

type fakeEvents struct {
	mu   sync.Mutex
	seq  int64
	rows []models.Event
}

func newFakeEvents() *fakeEvents {
	return &fakeEvents{}
}

func (f *fakeEvents) Create(_ context.Context, e models.Event) (*models.Event, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.insert(e)
}

func (f *fakeEvents) CreateBatch(_ context.Context, events []models.Event) ([]models.Event, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	stored := make([]models.Event, 0, len(events))
	duplicates := 0

	for _, e := range events {
		created, isNew, err := f.insert(e)
		if err != nil {
			return nil, 0, err
		}
		if isNew {
			stored = append(stored, *created)
			continue
		}
		duplicates++
	}

	return stored, duplicates, nil
}

func (f *fakeEvents) List(_ context.Context, filter repository.EventFilter) ([]models.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	matching := make([]models.Event, 0, len(f.rows))
	for _, row := range f.rows {
		switch {
		case row.UserID != filter.UserID:
		case len(filter.Types) > 0 && !slices.Contains(filter.Types, row.Type):
		case filter.From != nil && row.OccurredAt.Before(*filter.From):
		case filter.To != nil && !row.OccurredAt.Before(*filter.To):
		case filter.BeforeTime != nil && !before(row, *filter.BeforeTime, *filter.BeforeID):
		default:
			matching = append(matching, row)
		}
	}

	slices.SortFunc(matching, func(a, b models.Event) int {
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return b.OccurredAt.Compare(a.OccurredAt)
		}
		return int(b.ID - a.ID)
	})

	if len(matching) > filter.Limit {
		matching = matching[:filter.Limit]
	}
	return matching, nil
}

func (f *fakeEvents) insert(e models.Event) (*models.Event, bool, error) {
	if e.IdempotencyKey != nil {
		for _, row := range f.rows {
			if row.UserID == e.UserID && row.IdempotencyKey != nil && *row.IdempotencyKey == *e.IdempotencyKey {
				existing := row
				return &existing, false, nil
			}
		}
	}

	f.seq++
	e.ID = f.seq
	e.CreatedAt = time.Now()
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("{}")
	}
	f.rows = append(f.rows, e)

	created := e
	return &created, true, nil
}

func before(row models.Event, occurredAt time.Time, id int64) bool {
	if !row.OccurredAt.Equal(occurredAt) {
		return row.OccurredAt.Before(occurredAt)
	}
	return row.ID < id
}
