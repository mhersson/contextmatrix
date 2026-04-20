// Package sessionlog provides a thread-safe per-card ring buffer for runner
// session events. It is used as the server-side buffering layer for log
// streaming: events are appended as they arrive from the runner and can be
// snapshot-replayed to reconnecting web-UI subscribers.
package sessionlog

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultMaxEvents is the default event-count cap per session.
	DefaultMaxEvents = 2000
	// DefaultMaxBytes is the default payload-byte cap per session (1 MiB).
	DefaultMaxBytes = 1 << 20

	// EventTypeDropped is the synthetic event type inserted when oldest events
	// are evicted to make room for new ones.
	EventTypeDropped = "dropped"
)

// Event is a single log entry produced by a runner session.
type Event struct {
	Seq       uint64
	Timestamp time.Time
	Type      string
	Payload   []byte
	// CardID is the originating card for project-scoped events. It is empty
	// for card-scoped events (those sessions already carry the card ID as the
	// session key).
	CardID string
}

// droppedMarkerCount decodes the drop count stored in a dropped-marker event's
// Payload. Returns 0 for non-marker events or malformed payloads.
func droppedMarkerCount(e Event) uint64 {
	if e.Type != EventTypeDropped || len(e.Payload) < 8 {
		return 0
	}

	return binary.LittleEndian.Uint64(e.Payload[:8])
}

// encodeDropCount encodes a drop count as an 8-byte little-endian payload.
func encodeDropCount(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)

	return b
}

// sessionBuffer is a bounded FIFO buffer for a single card's events.
// It uses a plain slice with append-to-back and drop-from-front semantics.
// The buffer size stays at or below maxEvents at all times; totalBytes tracks
// the sum of payload lengths for the byte cap.
type sessionBuffer struct {
	events     []Event
	totalBytes int
	maxEvents  int
	maxBytes   int
}

func newSessionBuffer(maxEvents, maxBytes int) *sessionBuffer {
	return &sessionBuffer{
		events:    make([]Event, 0, maxEvents),
		maxEvents: maxEvents,
		maxBytes:  maxBytes,
	}
}

// append adds evt to the buffer, enforcing dual caps.
//
// Algorithm:
//  1. Evict the oldest event (drop from front) if either cap would be exceeded
//     after inserting evt.
//  2. Track how many real events were discarded; accumulate counts from any
//     existing dropped marker at the front.
//  3. After making room, prepend a single dropped marker (or update an
//     existing one at the front) to record the total discarded count.
//
// The marker occupies one slot in the buffer, so each overflow evicts one
// real event: the oldest real event is replaced by the (updated) marker, and
// evt is appended at the back.
func (b *sessionBuffer) append(evt Event) {
	evtBytes := len(evt.Payload)

	// Count of real events evicted in this call.
	droppedNow := uint64(0)

	// Step 1: make room. We must satisfy BOTH:
	//   - len(events)+1 <= maxEvents  (space for evt)
	//   - totalBytes+evtBytes <= maxBytes
	// We need to keep one slot free for evt; the marker will take its own slot
	// by replacing the oldest evicted event's position.
	for len(b.events)+1 > b.maxEvents || b.totalBytes+evtBytes > b.maxBytes {
		if len(b.events) == 0 {
			// evt itself is too large even for an empty buffer; drop it.
			return
		}

		oldest := b.events[0]
		b.events = b.events[1:]
		b.totalBytes -= len(oldest.Payload)

		if oldest.Type == EventTypeDropped {
			// Absorb the count from the evicted marker — it summarised
			// events already gone, carry that count forward.
			droppedNow += droppedMarkerCount(oldest)
		} else {
			droppedNow++
		}
	}

	// Step 2: if anything was dropped, insert or update the dropped marker at
	// the front of the buffer.
	if droppedNow > 0 {
		if len(b.events) > 0 && b.events[0].Type == EventTypeDropped {
			// Coalesce with existing marker at front.
			existing := droppedMarkerCount(b.events[0])
			b.totalBytes -= len(b.events[0].Payload)
			b.events[0].Payload = encodeDropCount(existing + droppedNow)
			b.totalBytes += len(b.events[0].Payload)
		} else {
			// Prepend a new marker. We need one extra slot for it.
			// Since len(events)+1 <= maxEvents was satisfied for evt, we may
			// have only exactly one free slot. Shift evt's slot to the marker
			// by evicting one more real event if needed.
			if len(b.events)+2 > b.maxEvents {
				// Evict one more to make room for the marker.
				extra := b.events[0]
				b.events = b.events[1:]

				b.totalBytes -= len(extra.Payload)
				if extra.Type == EventTypeDropped {
					droppedNow += droppedMarkerCount(extra)
				} else {
					droppedNow++
				}
			}

			marker := Event{
				Seq:       0,
				Timestamp: time.Now(),
				Type:      EventTypeDropped,
				Payload:   encodeDropCount(droppedNow),
			}
			// Prepend: insert at index 0.
			b.events = append(b.events, Event{}) // grow by 1
			copy(b.events[1:], b.events)
			b.events[0] = marker
			b.totalBytes += len(marker.Payload)
		}
	}

	// Step 3: append the new event.
	b.events = append(b.events, evt)
	b.totalBytes += evtBytes
}

// snapshot returns an ordered, defensive copy of all buffered events.
func (b *sessionBuffer) snapshot() []Event {
	if len(b.events) == 0 {
		return nil
	}

	out := make([]Event, len(b.events))
	for i, e := range b.events {
		if len(e.Payload) > 0 {
			cp := make([]byte, len(e.Payload))
			copy(cp, e.Payload)
			e.Payload = cp
		}

		out[i] = e
	}

	return out
}

// Option configures a Manager.
type Option func(*Manager)

// WithMaxEvents sets the per-session event-count cap.
func WithMaxEvents(n int) Option {
	return func(m *Manager) { m.maxEvents = n }
}

// WithMaxBytes sets the per-session payload-byte cap.
func WithMaxBytes(n int) Option {
	return func(m *Manager) { m.maxBytes = n }
}

// Manager holds per-card session buffers and synchronises concurrent access.
// It also manages upstream SSE connections and subscriber fan-out for live
// log streaming.
type Manager struct {
	mu        sync.Mutex
	sessions  map[string]*sessionBuffer
	maxEvents int
	maxBytes  int

	// droppedEvents counts fan-out drops across all sessions (slow-subscriber
	// events discarded in the default branch of the fan-out select). Atomically
	// updated so callers can observe the counter without holding m.mu.
	droppedEvents atomic.Uint64
	// lastDropWarn records the last Unix nanosecond a drop warning was emitted,
	// used to throttle slog.Warn calls to at most one per second globally.
	lastDropWarn atomic.Int64

	// Upstream session management (populated by WithRunnerConfig / Start / Stop).
	activeSessions map[string]*activeSession
	pendingSubs    map[string][]*subscriber // subscribers registered before Start
	failedSessions map[string]struct{}      // sessions that permanently failed upstream
	maxSessions    int
	sessionTTL     time.Duration
	runnerURL      string
	runnerAPIKey   string
}

// DroppedEvents returns the total number of fan-out events dropped because a
// subscriber's channel was full (slow subscriber). The count is monotonically
// increasing and safe to read without holding m.mu.
func (m *Manager) DroppedEvents() uint64 {
	return m.droppedEvents.Load()
}

// NewManager creates a Manager with optional configuration overrides.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		sessions:    make(map[string]*sessionBuffer),
		maxEvents:   DefaultMaxEvents,
		maxBytes:    DefaultMaxBytes,
		maxSessions: DefaultMaxSessions,
		sessionTTL:  DefaultSessionTTL,
	}
	for _, o := range opts {
		o(m)
	}

	return m
}

// getOrCreate returns the sessionBuffer for cardID, creating it if needed.
// Must be called with m.mu held.
func (m *Manager) getOrCreate(cardID string) *sessionBuffer {
	b, ok := m.sessions[cardID]
	if !ok {
		b = newSessionBuffer(m.maxEvents, m.maxBytes)
		m.sessions[cardID] = b
	}

	return b
}

// Append adds evt to the session buffer for cardID. If either cap is exceeded,
// the oldest event(s) are dropped and a marker is inserted or updated.
func (m *Manager) Append(cardID string, evt Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	b := m.getOrCreate(cardID)
	b.append(evt)
}

// Snapshot returns an ordered, defensive copy of all buffered events for
// cardID. Returns nil if no events have been appended for cardID.
func (m *Manager) Snapshot(cardID string) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.sessions[cardID]
	if !ok {
		return nil
	}

	return b.snapshot()
}

// SnapshotProject returns an ordered, defensive copy of all buffered events
// for the given project session. Returns nil if no events have been buffered.
func (m *Manager) SnapshotProject(project string) []Event {
	return m.Snapshot(projectKey(project))
}

// Clear removes all buffered events for cardID and frees the underlying buffer.
// After Clear, Snapshot returns nil for the same cardID. It also clears any
// permanent-failure flag so a future Start can restart the session cleanly.
func (m *Manager) Clear(cardID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, cardID)
	delete(m.failedSessions, cardID)
}
