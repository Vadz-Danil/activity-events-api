package stream

import (
	"sync"
	"sync/atomic"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

const defaultBuffer = 32

type Broker struct {
	mu      sync.RWMutex
	buffer  int
	subs    map[int64]map[*subscriber]struct{}
	dropped atomic.Uint64
}

type subscriber struct {
	events chan models.Event
}

func NewBroker() *Broker {
	return &Broker{buffer: defaultBuffer, subs: make(map[int64]map[*subscriber]struct{})}
}

func (b *Broker) Subscribe(userID int64) (<-chan models.Event, func()) {
	sub := &subscriber{events: make(chan models.Event, b.buffer)}

	b.mu.Lock()
	if b.subs[userID] == nil {
		b.subs[userID] = make(map[*subscriber]struct{})
	}
	b.subs[userID][sub] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[userID], sub)
			if len(b.subs[userID]) == 0 {
				delete(b.subs, userID)
			}
			b.mu.Unlock()

			close(sub.events)
		})
	}

	return sub.events, cancel
}

func (b *Broker) Publish(e models.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subs[e.UserID] {
		select {
		case sub.events <- e:
		default:
			b.dropped.Add(1)
		}
	}
}

func (b *Broker) Subscribers(userID int64) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subs[userID])
}

func (b *Broker) Dropped() uint64 {
	return b.dropped.Load()
}
