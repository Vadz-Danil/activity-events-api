package stream

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

func event(userID, id int64) models.Event {
	return models.Event{ID: id, UserID: userID, Type: "page.view", OccurredAt: time.Now()}
}

func receive(t *testing.T, ch <-chan models.Event) models.Event {
	t.Helper()

	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("expected an event, got nothing")
		return models.Event{}
	}
}

func TestEverySubscriberOfTheUserGetsTheEvent(t *testing.T) {
	broker := NewBroker()

	first, cancelFirst := broker.Subscribe(1)
	defer cancelFirst()
	second, cancelSecond := broker.Subscribe(1)
	defer cancelSecond()

	require.Equal(t, 2, broker.Subscribers(1))

	broker.Publish(event(1, 42))

	require.Equal(t, int64(42), receive(t, first).ID)
	require.Equal(t, int64(42), receive(t, second).ID)
}

func TestEventsAreNotDeliveredToOtherUsers(t *testing.T) {
	broker := NewBroker()

	mine, cancel := broker.Subscribe(1)
	defer cancel()

	broker.Publish(event(2, 7))

	select {
	case e := <-mine:
		t.Fatalf("received an event of another user: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCancelRemovesTheSubscriberAndClosesTheChannel(t *testing.T) {
	broker := NewBroker()

	events, cancel := broker.Subscribe(1)
	require.Equal(t, 1, broker.Subscribers(1))

	cancel()
	require.Zero(t, broker.Subscribers(1))

	_, open := <-events
	require.False(t, open, "the channel must be closed so the handler can return")
}

func TestCancelIsSafeToCallTwice(t *testing.T) {
	broker := NewBroker()

	_, cancel := broker.Subscribe(1)
	cancel()

	require.NotPanics(t, cancel)
}

func TestPublishDropsInsteadOfBlockingASlowReader(t *testing.T) {
	broker := NewBroker()

	_, cancel := broker.Subscribe(1)
	defer cancel()

	for i := range defaultBuffer + 10 {
		broker.Publish(event(1, int64(i)))
	}

	require.Equal(t, uint64(10), broker.Dropped(),
		"a reader that cannot keep up must not stall the writer")
}

func TestPublishStaysSafeWhileSubscribersComeAndGo(t *testing.T) {
	broker := NewBroker()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				broker.Publish(event(1, int64(i)))
			}
		}
	}()

	for range 50 {
		events, cancel := broker.Subscribe(1)
		go func() {
			for range events {
			}
		}()
		cancel()
	}

	close(stop)
	wg.Wait()
}
