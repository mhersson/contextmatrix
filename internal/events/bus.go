// Package events provides an in-process pub/sub event bus for real-time
// notifications of card mutations. Used by SSE endpoint and future webhooks.
package events

import (
	"log/slog"
	"sync"
	"time"
)

// EventType identifies the kind of event that occurred.
type EventType string

const (
	CardCreated      EventType = "card.created"
	CardUpdated      EventType = "card.updated"
	CardDeleted      EventType = "card.deleted"
	CardStateChanged EventType = "card.state_changed"
	CardClaimed      EventType = "card.claimed"
	CardReleased     EventType = "card.released"
	CardStalled      EventType = "card.stalled"
	CardLogAdded     EventType = "card.log_added"
)

// Event represents a board event that can be published to subscribers.
type Event struct {
	Type      EventType      `json:"type"`
	Project   string         `json:"project"`
	CardID    string         `json:"card_id"`
	Agent     string         `json:"agent,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

const (
	// subscriberBufferSize is the channel buffer size for each subscriber.
	// Events are dropped if a subscriber's buffer is full.
	subscriberBufferSize = 64
)

// subscriber represents a single event subscriber with a buffered channel.
type subscriber struct {
	ch chan Event
}

// Bus is an in-process pub/sub event bus. It fans out published events
// to all subscribers without blocking on slow consumers.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Subscribe registers a new subscriber and returns a channel for receiving
// events plus an unsubscribe function. The channel has a buffer of 64 events.
// Events are dropped if the subscriber doesn't consume them fast enough.
//
// The caller must call the unsubscribe function when done to prevent leaks.
// After unsubscribe, the returned channel will be closed.
func (b *Bus) Subscribe() (ch <-chan Event, unsubscribe func()) {
	sub := &subscriber{
		ch: make(chan Event, subscriberBufferSize),
	}

	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	return sub.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		if _, ok := b.subscribers[sub]; ok {
			delete(b.subscribers, sub)
			close(sub.ch)
		}
	}
}

// Publish sends an event to all subscribers. This method never blocks;
// if a subscriber's buffer is full, the event is dropped for that subscriber
// and a warning is logged.
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		select {
		case sub.ch <- event:
			// Event delivered
		default:
			// Buffer full, drop event for this subscriber
			slog.Warn("event dropped for slow subscriber",
				"event_type", event.Type,
				"project", event.Project,
				"card_id", event.CardID,
			)
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
// Useful for testing and monitoring.
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
