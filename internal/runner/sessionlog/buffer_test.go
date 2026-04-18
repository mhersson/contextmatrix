package sessionlog

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(seq uint64, typ string, payload string) Event {
	return Event{
		Seq:       seq,
		Timestamp: time.Now(),
		Type:      typ,
		Payload:   []byte(payload),
	}
}

// TestAppendSnapshot verifies basic round-trip: appended events are returned by Snapshot in order.
func TestAppendSnapshot(t *testing.T) {
	m := NewManager()
	const cardID = "PROJ-001"

	m.Append(cardID, makeEvent(1, "log", "hello"))
	m.Append(cardID, makeEvent(2, "log", "world"))

	snap := m.Snapshot(cardID)
	require.Len(t, snap, 2)
	assert.Equal(t, uint64(1), snap[0].Seq)
	assert.Equal(t, "hello", string(snap[0].Payload))
	assert.Equal(t, uint64(2), snap[1].Seq)
	assert.Equal(t, "world", string(snap[1].Payload))
}

// TestSnapshotEmptyCard verifies Snapshot on an unknown card returns nil/empty.
func TestSnapshotEmptyCard(t *testing.T) {
	m := NewManager()
	snap := m.Snapshot("NONEXISTENT-001")
	assert.Empty(t, snap)
}

// TestSnapshotDefensiveCopy verifies that mutating the returned slice does not affect the internal buffer.
func TestSnapshotDefensiveCopy(t *testing.T) {
	m := NewManager()
	const cardID = "PROJ-002"

	m.Append(cardID, makeEvent(1, "log", "original"))
	snap := m.Snapshot(cardID)
	require.Len(t, snap, 1)

	// Mutate the snapshot.
	snap[0].Type = "mutated"
	snap[0].Payload = []byte("mutated payload")

	// Internal state must be unchanged.
	snap2 := m.Snapshot(cardID)
	require.Len(t, snap2, 1)
	assert.Equal(t, "log", snap2[0].Type)
	assert.Equal(t, "original", string(snap2[0].Payload))
}

// TestEventCountCapEnforcement verifies that exceeding MaxEvents triggers drop-oldest with a marker.
func TestEventCountCapEnforcement(t *testing.T) {
	m := NewManager(WithMaxEvents(5))
	const cardID = "PROJ-003"

	// Fill to capacity.
	for i := range 5 {
		m.Append(cardID, makeEvent(uint64(i+1), "log", fmt.Sprintf("msg-%d", i+1)))
	}
	snap := m.Snapshot(cardID)
	assert.Len(t, snap, 5, "should hold exactly MaxEvents events")

	// One more event should cause oldest to be dropped and a marker inserted.
	m.Append(cardID, makeEvent(6, "log", "msg-6"))
	snap = m.Snapshot(cardID)

	// Buffer should still be at cap (5).
	assert.Len(t, snap, 5)

	// First event should now be a dropped marker.
	assert.Equal(t, EventTypeDropped, snap[0].Type)

	// Last event should be the newly appended one.
	assert.Equal(t, uint64(6), snap[len(snap)-1].Seq)
}

// TestDropMarkerCoalescing verifies that consecutive drops coalesce into a single marker.
//
// With maxEvents=4 and a full buffer [e1,e2,e3,e4]:
//   - Overflow 1 (add e5): evicts e1 and e2 to make room for marker + e5.
//     Result: [M(2), e3, e4, e5].
//   - Overflow 2 (add e6): evicts M(2) (absorbed=2) and e3 (dropped=1).
//     Total droppedNow=3. Result: [M(3), e4, e5, e6].
func TestDropMarkerCoalescing(t *testing.T) {
	m := NewManager(WithMaxEvents(4))
	const cardID = "PROJ-004"

	// Fill buffer: events 1, 2, 3, 4.
	for i := range 4 {
		m.Append(cardID, makeEvent(uint64(i+1), "log", fmt.Sprintf("msg-%d", i+1)))
	}

	// Overflow twice — both drops should coalesce into a single marker.
	m.Append(cardID, makeEvent(5, "log", "msg-5"))
	m.Append(cardID, makeEvent(6, "log", "msg-6"))

	snap := m.Snapshot(cardID)
	assert.Len(t, snap, 4)

	// First event should be a single dropped marker covering all dropped events.
	assert.Equal(t, EventTypeDropped, snap[0].Type)
	droppedCount := droppedMarkerCount(snap[0])
	assert.Equal(t, uint64(3), droppedCount, "coalesced marker should report 3 dropped events")

	// Last event should be the most recently appended one.
	assert.Equal(t, uint64(6), snap[len(snap)-1].Seq)
}

// TestByteCapEnforcement verifies that exceeding MaxBytes triggers drop-oldest with a marker.
func TestByteCapEnforcement(t *testing.T) {
	// Cap at 100 bytes total payload.
	m := NewManager(WithMaxBytes(100), WithMaxEvents(1000))
	const cardID = "PROJ-005"

	// Each event has a 40-byte payload; after 3 (120 bytes) the cap is exceeded.
	payload := "12345678901234567890123456789012345678901" // 41 bytes
	m.Append(cardID, makeEvent(1, "log", payload))
	m.Append(cardID, makeEvent(2, "log", payload))

	// At 82 bytes we're under 100. Add a third to push over.
	m.Append(cardID, makeEvent(3, "log", payload))

	snap := m.Snapshot(cardID)
	// The oldest event(s) should have been dropped to make room.
	// Verify a dropped marker is present.
	hasDropped := false
	for _, e := range snap {
		if e.Type == EventTypeDropped {
			hasDropped = true
			break
		}
	}
	assert.True(t, hasDropped, "should have a dropped marker after byte cap exceeded")

	// Total payload bytes in snapshot must be <= MaxBytes.
	var total int
	for _, e := range snap {
		total += len(e.Payload)
	}
	assert.LessOrEqual(t, total, 100)
}

// TestClearRemovesSession verifies that Clear removes all events for a card.
func TestClearRemovesSession(t *testing.T) {
	m := NewManager()
	const cardID = "PROJ-006"

	m.Append(cardID, makeEvent(1, "log", "hello"))
	m.Append(cardID, makeEvent(2, "log", "world"))
	require.Len(t, m.Snapshot(cardID), 2)

	m.Clear(cardID)
	snap := m.Snapshot(cardID)
	assert.Empty(t, snap)
}

// TestClearNonExistentCard verifies Clear on an unknown card is a no-op.
func TestClearNonExistentCard(t *testing.T) {
	m := NewManager()
	// Should not panic.
	m.Clear("NONEXISTENT-999")
}

// TestConcurrentAppendSnapshot exercises concurrent Append and Snapshot under the race detector.
func TestConcurrentAppendSnapshot(t *testing.T) {
	m := NewManager(WithMaxEvents(50))
	const cardID = "PROJ-007"
	const goroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range eventsPerGoroutine {
				seq := uint64(g*eventsPerGoroutine + i + 1)
				m.Append(cardID, makeEvent(seq, "log", fmt.Sprintf("g%d-e%d", g, i)))
				if i%10 == 0 {
					_ = m.Snapshot(cardID)
				}
			}
		}(g)
	}
	wg.Wait()

	// Final snapshot must be consistent (no panic, length <= MaxEvents).
	snap := m.Snapshot(cardID)
	assert.LessOrEqual(t, len(snap), 50)
}

// TestMultipleCards verifies that different card IDs are isolated in separate buffers.
func TestMultipleCards(t *testing.T) {
	m := NewManager()

	m.Append("PROJ-A", makeEvent(1, "log", "card-a-1"))
	m.Append("PROJ-B", makeEvent(1, "log", "card-b-1"))
	m.Append("PROJ-A", makeEvent(2, "log", "card-a-2"))

	snapA := m.Snapshot("PROJ-A")
	snapB := m.Snapshot("PROJ-B")

	require.Len(t, snapA, 2)
	require.Len(t, snapB, 1)

	assert.Equal(t, "card-a-1", string(snapA[0].Payload))
	assert.Equal(t, "card-a-2", string(snapA[1].Payload))
	assert.Equal(t, "card-b-1", string(snapB[0].Payload))
}
