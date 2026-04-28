package events

import (
	"sync"
	"time"
)

// RunnerEvent is one event emitted by CM for consumption by the runner's
// per-card SSE subscription.
type RunnerEvent struct {
	EventID uint64    `json:"event_id"`
	Type    string    `json:"type"`           // chat_input, stop, plan_approved, etc.
	Data    string    `json:"data,omitempty"` // payload, type-specific
	At      time.Time `json:"at"`
}

// RunnerEventBuffer keeps a per-card ring of recent events with a
// monotonic event_id, supporting Last-Event-ID-style replay on
// reconnect and live fan-out via Subscribe.
type RunnerEventBuffer struct {
	maxPerCard int
	maxAge     time.Duration

	mu     sync.Mutex
	nextID uint64
	cards  map[string]*cardSlot
}

type cardSlot struct {
	events []RunnerEvent
	subs   []chan RunnerEvent
}

// NewRunnerEventBuffer creates a buffer keeping at most maxPerCard
// events per card_id; events older than maxAge are eligible for
// eviction on next Append.
func NewRunnerEventBuffer(maxPerCard int, maxAge time.Duration) *RunnerEventBuffer {
	return &RunnerEventBuffer{
		maxPerCard: maxPerCard,
		maxAge:     maxAge,
		cards:      make(map[string]*cardSlot),
	}
}

// Append records an event for cardID. Assigns the next monotonic
// EventID and the current timestamp if At is zero. Fans out to live
// subscribers. Returns the stored event (with EventID and At set).
func (b *RunnerEventBuffer) Append(cardID string, ev RunnerEvent) RunnerEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++

	ev.EventID = b.nextID
	if ev.At.IsZero() {
		ev.At = time.Now()
	}

	slot := b.cards[cardID]
	if slot == nil {
		slot = &cardSlot{}
		b.cards[cardID] = slot
	}

	slot.events = append(slot.events, ev)

	// Evict by count: keep newest maxPerCard.
	if len(slot.events) > b.maxPerCard {
		slot.events = slot.events[len(slot.events)-b.maxPerCard:]
	}

	// Evict by age.
	cutoff := time.Now().Add(-b.maxAge)

	pruned := slot.events[:0]
	for _, e := range slot.events {
		if !e.At.Before(cutoff) {
			pruned = append(pruned, e)
		}
	}

	slot.events = pruned

	// Fan out to subscribers (non-blocking; drop if full).
	for _, ch := range slot.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber too slow; they'll catch up on reconnect via Since.
		}
	}

	return ev
}

// Since returns events newer than lastEventID for the given card.
// Returns nil if the card has no buffered events.
func (b *RunnerEventBuffer) Since(cardID string, lastEventID uint64) []RunnerEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	slot := b.cards[cardID]
	if slot == nil {
		return nil
	}

	out := make([]RunnerEvent, 0, len(slot.events))
	for _, e := range slot.events {
		if e.EventID > lastEventID {
			out = append(out, e)
		}
	}

	return out
}

// Subscribe opens a live event channel for cardID. The channel
// receives all subsequent Append'd events. The returned cancel
// function detaches the subscriber and closes the channel.
func (b *RunnerEventBuffer) Subscribe(cardID string) (<-chan RunnerEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	slot := b.cards[cardID]
	if slot == nil {
		slot = &cardSlot{}
		b.cards[cardID] = slot
	}

	ch := make(chan RunnerEvent, 32)
	slot.subs = append(slot.subs, ch)

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		slot := b.cards[cardID]
		if slot == nil {
			return
		}

		for i, c := range slot.subs {
			if c == ch {
				slot.subs = append(slot.subs[:i], slot.subs[i+1:]...)

				close(ch)

				return
			}
		}
	}

	return ch, cancel
}
