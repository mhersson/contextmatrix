package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBus(t *testing.T) {
	bus := NewBus()
	assert.NotNil(t, bus)
	assert.Equal(t, 0, bus.SubscriberCount())
}

func TestSubscribe(t *testing.T) {
	bus := NewBus()

	ch, unsub := bus.Subscribe()
	defer unsub()

	assert.NotNil(t, ch)
	assert.Equal(t, 1, bus.SubscriberCount())
}

func TestUnsubscribe(t *testing.T) {
	bus := NewBus()

	ch, unsub := bus.Subscribe()
	assert.Equal(t, 1, bus.SubscriberCount())

	unsub()
	assert.Equal(t, 0, bus.SubscriberCount())

	// Channel should be closed after unsubscribe
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unsubscribe")
}

func TestUnsubscribeIdempotent(t *testing.T) {
	bus := NewBus()

	_, unsub := bus.Subscribe()
	assert.Equal(t, 1, bus.SubscriberCount())

	// Multiple unsubscribes should not panic
	unsub()
	unsub()
	unsub()
	assert.Equal(t, 0, bus.SubscriberCount())
}

func TestPublishToSubscriber(t *testing.T) {
	bus := NewBus()
	ch, unsub := bus.Subscribe()
	defer unsub()

	event := Event{
		Type:      CardCreated,
		Project:   "test-project",
		CardID:    "TEST-001",
		Agent:     "agent-1",
		Timestamp: time.Now(),
		Data:      map[string]any{"key": "value"},
	}

	bus.Publish(event)

	select {
	case received := <-ch:
		assert.Equal(t, event.Type, received.Type)
		assert.Equal(t, event.Project, received.Project)
		assert.Equal(t, event.CardID, received.CardID)
		assert.Equal(t, event.Agent, received.Agent)
		assert.Equal(t, event.Data, received.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestPublishToMultipleSubscribers(t *testing.T) {
	bus := NewBus()

	ch1, unsub1 := bus.Subscribe()
	defer unsub1()
	ch2, unsub2 := bus.Subscribe()
	defer unsub2()
	ch3, unsub3 := bus.Subscribe()
	defer unsub3()

	assert.Equal(t, 3, bus.SubscriberCount())

	event := Event{
		Type:      CardUpdated,
		Project:   "test-project",
		CardID:    "TEST-002",
		Timestamp: time.Now(),
	}

	bus.Publish(event)

	// All subscribers should receive the event
	for i, ch := range []<-chan Event{ch1, ch2, ch3} {
		select {
		case received := <-ch:
			assert.Equal(t, event.Type, received.Type, "subscriber %d", i+1)
			assert.Equal(t, event.CardID, received.CardID, "subscriber %d", i+1)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event on subscriber %d", i+1)
		}
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	bus := NewBus()

	// Should not panic with no subscribers
	event := Event{
		Type:      CardDeleted,
		Project:   "test-project",
		CardID:    "TEST-003",
		Timestamp: time.Now(),
	}

	bus.Publish(event)
}

func TestPublishAfterUnsubscribe(t *testing.T) {
	bus := NewBus()

	ch, unsub := bus.Subscribe()
	unsub()

	event := Event{
		Type:      CardCreated,
		Project:   "test-project",
		CardID:    "TEST-004",
		Timestamp: time.Now(),
	}

	// Should not panic
	bus.Publish(event)

	// Channel should be closed, no event received
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	default:
		// This is also acceptable - closed channel returns immediately
	}
}

func TestPublishNonBlocking(t *testing.T) {
	bus := NewBus()

	// Subscribe but never read from channel
	_, unsub := bus.Subscribe()
	defer unsub()

	// Fill the buffer (64 events)
	for i := 0; i < subscriberBufferSize; i++ {
		bus.Publish(Event{
			Type:      CardUpdated,
			Project:   "test-project",
			CardID:    "TEST-FILL",
			Timestamp: time.Now(),
		})
	}

	// Additional publishes should not block (events dropped)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			bus.Publish(Event{
				Type:      CardUpdated,
				Project:   "test-project",
				CardID:    "TEST-OVERFLOW",
				Timestamp: time.Now(),
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Good - publish completed without blocking
	case <-time.After(time.Second):
		t.Fatal("publish blocked on slow subscriber")
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	bus := NewBus()

	const (
		numSubscribers = 10
		numPublishers  = 5
		numEvents      = 100
	)

	var wg sync.WaitGroup
	received := make([]int, numSubscribers)
	unsubscribers := make([]func(), numSubscribers)

	// Start subscribers
	for i := 0; i < numSubscribers; i++ {
		ch, unsub := bus.Subscribe()
		unsubscribers[i] = unsub
		wg.Add(1)
		go func(idx int, ch <-chan Event) {
			defer wg.Done()
			for range ch {
				received[idx]++
			}
		}(i, ch)
	}

	// Start publishers
	var pubWg sync.WaitGroup
	for i := 0; i < numPublishers; i++ {
		pubWg.Add(1)
		go func() {
			defer pubWg.Done()
			for j := 0; j < numEvents; j++ {
				bus.Publish(Event{
					Type:      CardUpdated,
					Project:   "test-project",
					CardID:    "TEST-CONCURRENT",
					Timestamp: time.Now(),
				})
			}
		}()
	}

	// Wait for all publishes
	pubWg.Wait()

	// Unsubscribe all
	for _, unsub := range unsubscribers {
		unsub()
	}

	// Wait for all subscribers to finish
	wg.Wait()

	// Each subscriber should have received events (exact count depends on timing)
	totalExpected := numPublishers * numEvents
	for i, count := range received {
		// Due to non-blocking sends, some events may be dropped if buffers fill
		// But with 64 buffer size and reasonable timing, most should get through
		assert.GreaterOrEqual(t, count, 1, "subscriber %d should have received at least 1 event", i)
		assert.LessOrEqual(t, count, totalExpected, "subscriber %d received too many events", i)
	}
}

func TestEventTypes(t *testing.T) {
	// Verify all event type constants have expected values
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{CardCreated, "card.created"},
		{CardUpdated, "card.updated"},
		{CardDeleted, "card.deleted"},
		{CardStateChanged, "card.state_changed"},
		{CardClaimed, "card.claimed"},
		{CardReleased, "card.released"},
		{CardStalled, "card.stalled"},
		{CardLogAdded, "card.log_added"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.eventType))
		})
	}
}

func TestEventWithNilData(t *testing.T) {
	bus := NewBus()
	ch, unsub := bus.Subscribe()
	defer unsub()

	event := Event{
		Type:      CardCreated,
		Project:   "test-project",
		CardID:    "TEST-NIL",
		Timestamp: time.Now(),
		Data:      nil,
	}

	bus.Publish(event)

	select {
	case received := <-ch:
		assert.Nil(t, received.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestSubscriberReceivesOnlyAfterSubscription(t *testing.T) {
	bus := NewBus()

	// Publish before subscription
	bus.Publish(Event{
		Type:      CardCreated,
		Project:   "test-project",
		CardID:    "TEST-BEFORE",
		Timestamp: time.Now(),
	})

	// Now subscribe
	ch, unsub := bus.Subscribe()
	defer unsub()

	// Publish after subscription
	bus.Publish(Event{
		Type:      CardUpdated,
		Project:   "test-project",
		CardID:    "TEST-AFTER",
		Timestamp: time.Now(),
	})

	// Should only receive the second event
	select {
	case received := <-ch:
		assert.Equal(t, "TEST-AFTER", received.CardID)
		assert.Equal(t, CardUpdated, received.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}

	// Verify no more events in channel
	select {
	case e := <-ch:
		t.Fatalf("unexpected event: %v", e)
	case <-time.After(50 * time.Millisecond):
		// Good - no more events
	}
}

func BenchmarkPublish(b *testing.B) {
	bus := NewBus()

	// Add some subscribers
	for i := 0; i < 10; i++ {
		ch, unsub := bus.Subscribe()
		defer unsub()
		go func(ch <-chan Event) {
			for range ch {
				// Consume events
			}
		}(ch)
	}

	event := Event{
		Type:      CardUpdated,
		Project:   "bench-project",
		CardID:    "BENCH-001",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(event)
	}
}

func BenchmarkSubscribeUnsubscribe(b *testing.B) {
	bus := NewBus()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, unsub := bus.Subscribe()
		unsub()
	}
}

func TestPartialUnsubscribe(t *testing.T) {
	bus := NewBus()

	ch1, unsub1 := bus.Subscribe()
	defer unsub1()
	ch2, unsub2 := bus.Subscribe()
	ch3, unsub3 := bus.Subscribe()
	defer unsub3()

	require.Equal(t, 3, bus.SubscriberCount())

	// Unsubscribe middle one
	unsub2()
	require.Equal(t, 2, bus.SubscriberCount())

	// ch2 should be closed
	_, ok := <-ch2
	assert.False(t, ok, "ch2 should be closed")

	// Publish should still reach ch1 and ch3
	event := Event{
		Type:      CardCreated,
		Project:   "test-project",
		CardID:    "TEST-PARTIAL",
		Timestamp: time.Now(),
	}
	bus.Publish(event)

	for _, ch := range []<-chan Event{ch1, ch3} {
		select {
		case received := <-ch:
			assert.Equal(t, "TEST-PARTIAL", received.CardID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event")
		}
	}
}
