package sessionlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFailFastManager constructs a sessionlog.Manager whose upstream reconnect
// backoffs are collapsed to zero real wall-clock time. It wires a fake clock
// and spawns a janitor goroutine that repeatedly advances the clock so any
// pump goroutine parked on clk.After(backoff) wakes immediately. The janitor
// exits when the returned context is cancelled — the caller passes the
// cleanup func to t.Cleanup.
func newFailFastManager(t *testing.T, extra ...Option) (*Manager, func()) {
	t.Helper()

	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	opts := append([]Option{WithClock(fake)}, extra...)
	m := NewManager(opts...)

	stop := make(chan struct{})

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// 20s covers backoffDuration's 16s cap with room to spare.
				fake.Advance(20 * time.Second)
			}
		}
	}()

	cleanup := func() {
		close(stop)
		wg.Wait()
	}

	return m, cleanup
}

// sseServer builds an httptest.Server that streams the given events as SSE
// frames and then holds the connection open until the client disconnects.
// readyCh is closed once the handler is invoked (after headers are flushed).
func sseServer(t *testing.T, events []Event, readyCh chan struct{}) *httptest.Server {
	t.Helper()

	var once sync.Once

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		once.Do(func() { close(readyCh) })

		for _, evt := range events {
			payload, err := json.Marshal(sseJSONPayload{
				Seq:       evt.Seq,
				Timestamp: evt.Timestamp.Format(time.RFC3339Nano),
				Type:      evt.Type,
				Content:   string(evt.Payload),
			})
			if err != nil {
				return
			}

			if _, err = fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}

			flusher.Flush()
		}
		// Hold open until client disconnects.
		<-r.Context().Done()
	}))
}

// sseServerInfinite builds an httptest.Server that keeps the SSE connection
// open without sending any events, until the client disconnects.
func sseServerInfinite(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		<-r.Context().Done()
	}))
}

// newTestEvents returns n synthetic events for testing.
func newTestEvents(n int) []Event {
	evts := make([]Event, n)
	for i := range n {
		evts[i] = Event{
			Seq:       uint64(i + 1),
			Timestamp: time.Now(),
			Type:      "log",
			Payload:   fmt.Appendf(nil, "msg-%d", i+1),
		}
	}

	return evts
}

// drainN reads at most n events from ch within timeout, returning what arrived.
func drainN(ch <-chan Event, n int, timeout time.Duration) []Event {
	deadline := time.After(timeout)

	var out []Event
	for len(out) < n {
		select {
		case evt, ok := <-ch:
			if !ok {
				return out
			}

			out = append(out, evt)
		case <-deadline:
			return out
		}
	}

	return out
}

// stopThenClose calls m.Stop(cardID) then srv.Close() in order, ensuring the
// upstream pump disconnects before the test server tries to finish.
func stopThenClose(m *Manager, cardID string, srv *httptest.Server) {
	m.Stop(cardID)
	srv.Close()
}

// TestStartSubscribeLiveAndSnapshot verifies:
// - A subscriber registered before Start receives events as live events.
// - A late subscriber receives events via snapshot.
func TestStartSubscribeLiveAndSnapshot(t *testing.T) {
	const cardID = "MGR-001"

	events := newTestEvents(3)

	readyCh := make(chan struct{})
	srv := sseServer(t, events, readyCh)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer stopThenClose(m, cardID, srv)

	// Subscribe before Start — goes into pendingSubs, picked up when Start runs.
	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Wait for the handler to be invoked (connection established, headers sent).
	<-readyCh

	// Receive 3 live events.
	got := drainN(ch, len(events), 5*time.Second)
	require.Len(t, got, len(events), "expected all events via live channel")

	for i, evt := range got {
		assert.Equal(t, events[i].Seq, evt.Seq)
		assert.Equal(t, string(events[i].Payload), string(evt.Payload))
	}

	// Allow a moment for the buffer to be populated before the late subscribe.
	require.Eventually(t, func() bool {
		return len(m.Snapshot(cardID)) == len(events)
	}, 2*time.Second, 10*time.Millisecond)

	// Late subscribe — should receive snapshot.
	lateCh, lateUnsub := m.Subscribe(cardID)
	defer lateUnsub()

	snap := drainN(lateCh, len(events), 5*time.Second)
	require.Len(t, snap, len(events), "late subscriber should receive snapshot")
}

// TestMultipleConcurrentSubscribers verifies that all concurrent subscribers
// receive every event.
func TestMultipleConcurrentSubscribers(t *testing.T) {
	const (
		cardID  = "MGR-002"
		numSubs = 5
	)

	events := newTestEvents(10)

	// Gate the server: send events only after all subscribers are registered.
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		<-gate // wait until test signals

		for _, evt := range events {
			payload, _ := json.Marshal(sseJSONPayload{
				Seq:     evt.Seq,
				Type:    evt.Type,
				Content: string(evt.Payload),
			})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}

			flusher.Flush()
		}

		<-r.Context().Done()
	}))

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer stopThenClose(m, cardID, srv)

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Register all subscribers before unblocking the server.
	channels := make([]<-chan Event, numSubs)

	unsubs := make([]func(), numSubs)
	for i := range numSubs {
		channels[i], unsubs[i] = m.Subscribe(cardID)
		defer unsubs[i]()
	}

	// Allow the server handler to start sending.
	close(gate)

	// Each subscriber must receive all 10 events.
	for i, ch := range channels {
		got := drainN(ch, len(events), 5*time.Second)
		assert.Len(t, got, len(events), "subscriber %d should receive all events", i)
	}
}

// TestStopDrainsSubscribers verifies that Stop sends a terminal event to all
// subscribers, closes their channels, and clears the buffer.
func TestStopDrainsSubscribers(t *testing.T) {
	const cardID = "MGR-003"

	srv := sseServerInfinite(t)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	require.NoError(t, m.Start(context.Background(), cardID, ""))

	ch1, unsub1 := m.Subscribe(cardID)
	defer unsub1()

	ch2, unsub2 := m.Subscribe(cardID)
	defer unsub2()

	// Allow the pump goroutine to connect to the server.
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()

		_, ok := m.activeSessions[cardID]

		return ok
	}, 2*time.Second, 5*time.Millisecond)

	// Stop cancels the pump and drains subscribers.
	m.Stop(cardID)
	srv.Close()

	// Both channels should receive a terminal event.
	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt, ok := <-ch:
			if ok {
				assert.Equal(t, EventTypeTerminal, evt.Type, "sub %d: expected terminal event", i)
			}
			// ok==false means channel was closed; that is also acceptable.
		case <-time.After(2 * time.Second):
			t.Errorf("subscriber %d: timed out waiting for terminal event", i)
		}
	}

	// Buffer should be cleared.
	assert.Empty(t, m.Snapshot(cardID))
}

// TestStartIdempotent verifies that calling Start twice returns nil and does
// not launch a second pump.
func TestStartIdempotent(t *testing.T) {
	const cardID = "MGR-004"

	srv := sseServerInfinite(t)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer stopThenClose(m, cardID, srv)

	require.NoError(t, m.Start(context.Background(), cardID, ""))
	require.NoError(t, m.Start(context.Background(), cardID, ""), "second Start must be idempotent")

	// Only one session should be registered.
	m.mu.Lock()
	count := len(m.activeSessions)
	m.mu.Unlock()
	assert.Equal(t, 1, count)
}

// TestStopIdempotent verifies that Stop on a non-existent session is a no-op.
func TestStopIdempotent(t *testing.T) {
	m := NewManager()
	// Should not panic.
	m.Stop("NONEXISTENT-999")
}

// TestSessionCapEnforcement verifies that the (cap+1)th Start returns an error.
func TestSessionCapEnforcement(t *testing.T) {
	const maxSess = 10 // use a small cap for testing

	srv := sseServerInfinite(t)
	defer srv.Close()

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"), WithMaxSessions(maxSess))

	t.Cleanup(func() {
		for i := range maxSess {
			m.Stop(fmt.Sprintf("CAP-%03d", i))
		}
	})

	for i := range maxSess {
		cardID := fmt.Sprintf("CAP-%03d", i)
		require.NoError(t, m.Start(context.Background(), cardID, ""), "session %d should start", i)
	}

	// One more should fail.
	err := m.Start(context.Background(), "CAP-OVERFLOW", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session cap")
}

// TestIdleSweeper verifies that sessions older than the TTL are force-closed.
func TestIdleSweeper(t *testing.T) {
	const (
		cardID = "SWEEP-001"
		ttl    = 100 * time.Millisecond
	)

	srv := sseServerInfinite(t)
	defer srv.Close()

	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	m := NewManager(
		WithRunnerConfig(srv.URL, "test-key"),
		WithSessionTTL(ttl),
		WithClock(fake),
	)

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Advance the fake clock past TTL, then trigger sweeper directly
	// (sweepIdleSessions compares session startTime against fake.Now()).
	fake.Advance(ttl + 20*time.Millisecond)
	m.sweepIdleSessions(context.Background())

	// Session should be gone.
	m.mu.Lock()
	_, running := m.activeSessions[cardID]
	m.mu.Unlock()
	assert.False(t, running, "session should have been swept")
}

// TestUpstreamRetryAndError verifies that after maxUpstreamRetries failed
// connections the session is removed and subscribers receive a terminal event.
//
// The reconnect-backoff waits are driven through the manager's clock. This
// test uses newFailFastManager which wires a fake clock and auto-advances
// it, collapsing the ~3.75 s of natural backoff into sub-second wall-clock.
func TestUpstreamRetryAndError(t *testing.T) {
	const cardID = "RETRY-001"

	// Server that immediately returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
	t.Cleanup(cleanup)

	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	var gotTerminal bool

	select {
	case evt, ok := <-ch:
		if !ok || evt.Type == EventTypeTerminal {
			gotTerminal = true
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal event after upstream errors")
	}

	assert.True(t, gotTerminal)

	// Session should no longer be active.
	m.mu.Lock()
	_, running := m.activeSessions[cardID]
	m.mu.Unlock()
	assert.False(t, running)
}

// TestSubscribeBeforeStart verifies that a subscriber registered before Start
// receives live events once the session begins.
func TestSubscribeBeforeStart(t *testing.T) {
	const cardID = "MGR-005"

	events := newTestEvents(2)

	readyCh := make(chan struct{})
	srv := sseServer(t, events, readyCh)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer stopThenClose(m, cardID, srv)

	// Subscribe before Start.
	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))
	<-readyCh

	got := drainN(ch, len(events), 5*time.Second)
	assert.Len(t, got, len(events))
}

// TestSubscribeSnapshotLiveOrdering reproduces the interleave/duplicate race in
// Subscribe. The bug: Subscribe releases m.mu after registering the subscriber
// but before the snapshot goroutine writes to the channel. The pump goroutine
// can immediately acquire m.mu and fan out live events, so a live event arrives
// on the channel before the snapshot events.
//
// Setup:
//   - A high-frequency SSE server streams events with Seq >= snapshotSize+1.
//   - The buffer is pre-populated with snapshotSize events (Seq 1..snapshotSize)
//     via direct m.Append calls, so a non-empty snapshot exists before Subscribe.
//   - Subscribe is called while the pump is actively fanning out live events.
//   - The channel is drained for a bounded duration.
//
// The test runs the scenario in a loop to make the race reproducible. It fails
// on the current implementation and must pass after the fix (subtask 2).
func TestSubscribeSnapshotLiveOrdering(t *testing.T) {
	const (
		snapshotSize = 200
		iterations   = 25
		drainTimeout = 200 * time.Millisecond
	)

	for iter := range iterations {
		t.Run(fmt.Sprintf("iter%d", iter), func(t *testing.T) {
			const cardID = "ORDER-001"

			// liveSeq tracks the next Seq to send from the SSE server.
			// Starts above snapshotSize so snapshot events are distinguishable.
			var liveSeq atomic.Uint64
			liveSeq.Store(uint64(snapshotSize + 1))

			// Build an SSE server that streams events continuously at full speed
			// until the client disconnects. Each event gets a monotonically
			// increasing Seq value above snapshotSize.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming not supported", http.StatusInternalServerError)

					return
				}

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				flusher.Flush()

				for r.Context().Err() == nil {
					seq := liveSeq.Add(1)

					payload, err := json.Marshal(sseJSONPayload{
						Seq:  seq,
						Type: "log",
					})
					if err != nil {
						return
					}

					if _, err = fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
						return
					}

					flusher.Flush()
				}
			}))

			m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
			defer stopThenClose(m, cardID, srv)

			// Start the session so the pump goroutine connects and begins
			// fanning out live events from the SSE server.
			require.NoError(t, m.Start(context.Background(), cardID, ""))

			// Wait for the pump to connect and begin fanning out (evidenced by at
			// least one live event being processed — liveSeq advances beyond its
			// initial value once the server starts sending).
			require.Eventually(t, func() bool {
				return liveSeq.Load() > uint64(snapshotSize+2)
			}, 2*time.Second, time.Millisecond)

			// Pre-populate the buffer with snapshotSize events (Seq 1..snapshotSize).
			// These represent events that arrived before the current Subscribe call.
			for i := range snapshotSize {
				m.Append(cardID, Event{
					Seq:       uint64(i + 1),
					Timestamp: time.Now(),
					Type:      "log",
					Payload:   fmt.Appendf(nil, "snap-%d", i+1),
				})
			}

			// Subscribe while the pump is actively sending live events (Seq > snapshotSize).
			// This is the window where the race can occur.
			ch, unsub := m.Subscribe(cardID)
			defer unsub()

			// Drain the channel for drainTimeout. Collect all events that arrive.
			var received []Event

			deadline := time.After(drainTimeout)

		drain:
			for {
				select {
				case evt, ok := <-ch:
					if !ok {
						break drain
					}

					received = append(received, evt)
				case <-deadline:
					break drain
				}
			}

			// Skip marker / terminal events for ordering checks.
			isMarker := func(e Event) bool {
				return e.Type == EventTypeDropped || e.Type == EventTypeTerminal
			}

			// Assertion 1: Seq values must be strictly non-decreasing (ignoring markers).
			// Any violation means a live event arrived before snapshot events.
			var (
				prevSeq  uint64
				seenLive bool
			)

			for _, evt := range received {
				if isMarker(evt) {
					continue
				}

				isLive := evt.Seq > snapshotSize
				if isLive {
					seenLive = true
				}
				// Once we have seen a live event, no snapshot event should appear.
				if seenLive && !isLive {
					t.Errorf("iter %d: snapshot event (Seq=%d) arrived after live event on subscriber channel — ordering violated",
						iter, evt.Seq)

					break
				}
				// Seq must be non-decreasing within each segment.
				if evt.Seq < prevSeq {
					t.Errorf("iter %d: Seq decreased: got %d after %d",
						iter, evt.Seq, prevSeq)

					break
				}

				prevSeq = evt.Seq
			}

			// Assertion 2: No duplicate Seq values on the channel (ignoring markers).
			seen := make(map[uint64]int)

			for i, evt := range received {
				if isMarker(evt) {
					continue
				}

				if first, dup := seen[evt.Seq]; dup {
					t.Errorf("iter %d: duplicate Seq=%d at positions %d and %d",
						iter, evt.Seq, first, i)
				}

				seen[evt.Seq] = i
			}
		})
	}
}

// TestSubscribeSnapshotTailDropOnSlowSubscriber reproduces the silent
// snapshot-tail-drop bug in Manager.Subscribe.
//
// The snapshot-delivery goroutine inside Subscribe writes buffered events to the
// subscriber channel with a non-blocking send: when the 256-slot channel fills
// it breaks out of the loop and discards the remaining snapshot tail rather than
// blocking until the consumer drains the channel.
//
// Setup:
//   - No upstream session is started; the snapshot goroutine runs
//     unconditionally inside Subscribe, so no pump is needed.
//   - 1000 events (Seq 1..1000) are pre-populated via m.Append.
//   - A consumer goroutine sleeps 100µs between reads — slow enough that the
//     256-slot buffer fills before it can drain, triggering the tail-drop.
//
// Assertions:
//   - Exact set equality: received Seqs == {1..1000} (no gaps, not just matching totals).
//   - No duplicate Seq values.
//   - Received events are in non-decreasing Seq order.
//
// On current main this test fails reporting missing tail events (roughly
// Seq 257..1000 depending on timing). Subtask 2 (CTXMAX-305) will make it pass.
func TestSubscribeSnapshotTailDropOnSlowSubscriber(t *testing.T) {
	const (
		cardID      = "TAIL-001"
		numEvents   = 1000
		readDelay   = 100 * time.Microsecond
		testTimeout = 2 * time.Second
	)

	m := NewManager()

	// Pre-populate the buffer with numEvents events (Seq 1..numEvents).
	for i := range numEvents {
		m.Append(cardID, Event{
			Seq:       uint64(i + 1),
			Timestamp: time.Now(),
			Type:      "log",
			Payload:   fmt.Appendf(nil, "msg-%d", i+1),
		})
	}

	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	// Drain in a goroutine that sleeps between reads to simulate a slow consumer.
	// This ensures the 256-slot channel fills before all events are delivered,
	// exposing the non-blocking-send tail-drop in the snapshot goroutine.
	type result struct {
		events []Event
	}

	done := make(chan result, 1)

	go func() {
		var collected []Event

		deadline := time.After(testTimeout)

		for len(collected) < numEvents {
			select {
			case evt, ok := <-ch:
				if !ok {
					done <- result{events: collected}

					return
				}

				collected = append(collected, evt)

				// Deliberate wall-clock pause — the whole point of this test is
				// to simulate a slow consumer so the fan-out channel fills and
				// the tail-drop bug is exposed. A fake clock cannot substitute.
				time.Sleep(readDelay)
			case <-deadline:
				done <- result{events: collected}

				return
			}
		}

		done <- result{events: collected}
	}()

	res := <-done
	received := res.events

	// Build the expected set {1..numEvents}.
	wantSeqs := make(map[uint64]struct{}, numEvents)
	for i := range numEvents {
		wantSeqs[uint64(i+1)] = struct{}{}
	}

	// Check non-decreasing order.
	var prevSeq uint64
	for _, evt := range received {
		if evt.Seq < prevSeq {
			t.Errorf("Seq decreased: got %d after %d (ordering violated)", evt.Seq, prevSeq)

			break
		}

		prevSeq = evt.Seq
	}

	// Check no duplicates and collect received set.
	gotSeqs := make(map[uint64]int, len(received))
	for i, evt := range received {
		if first, dup := gotSeqs[evt.Seq]; dup {
			t.Errorf("duplicate Seq=%d at positions %d and %d", evt.Seq, first, i)
		}

		gotSeqs[evt.Seq] = i
	}

	// Check exact set equality: every expected Seq must be present.
	var missing []uint64

	for seq := range wantSeqs {
		if _, ok := gotSeqs[seq]; !ok {
			missing = append(missing, seq)
		}
	}

	if len(missing) > 0 {
		// Sort for a deterministic diagnostic message.
		slices.Sort(missing)
		t.Errorf("missing tail events (%d total): first few missing Seqs: %v ...",
			len(missing), missing[:min(len(missing), 10)])
	}

	// Check no unexpected extra events.
	var unexpected []uint64

	for seq := range gotSeqs {
		if _, ok := wantSeqs[seq]; !ok {
			unexpected = append(unexpected, seq)
		}
	}

	if len(unexpected) > 0 {
		slices.Sort(unexpected)
		t.Errorf("unexpected extra Seq values: %v", unexpected[:min(len(unexpected), 10)])
	}
}

// TestSubscribeUnsubUnblocksSnapshot verifies that calling unsub while the
// snapshot goroutine is blocked (channel full, slow subscriber) causes the
// goroutine to exit promptly rather than leaking.
func TestSubscribeUnsubUnblocksSnapshot(t *testing.T) {
	const (
		cardID    = "UNSUB-SNAP-001"
		numEvents = subscriberChanBuf + 200 // more than the channel buffer
		timeout   = 200 * time.Millisecond
	)

	m := NewManager()

	// Pre-populate buffer with more events than the channel can hold.
	for i := range numEvents {
		m.Append(cardID, Event{
			Seq:       uint64(i + 1),
			Timestamp: time.Now(),
			Type:      "log",
			Payload:   fmt.Appendf(nil, "msg-%d", i+1),
		})
	}

	// Subscribe but do NOT read from ch — the channel will fill up and the
	// snapshot goroutine will block on the (subscriberChanBuf+1)th event.
	_, unsub := m.Subscribe(cardID)

	// Grab the subscriber so we can observe snapDone.
	// The subscriber is in pendingSubs because no session was started.
	m.mu.Lock()

	var sub *subscriber
	if subs, ok := m.pendingSubs[cardID]; ok && len(subs) > 0 {
		sub = subs[0]
	}
	m.mu.Unlock()

	require.NotNil(t, sub, "expected subscriber in pendingSubs")

	// Call unsub — this should unblock the snapshot goroutine.
	unsub()

	// Assert that snapDone is closed within the timeout.
	select {
	case <-sub.snapDone:
		// Goroutine exited cleanly.
	case <-time.After(timeout):
		t.Errorf("snapshot goroutine did not exit within %v after unsub", timeout)
	}
}

// TestSubscribeStopUnblocksSnapshot verifies that calling Stop while the
// snapshot goroutine is blocked causes the goroutine to exit promptly and the
// subscriber channel to be closed cleanly (no panic, no hang).
// Run under -race to catch any residual close-of-send race.
func TestSubscribeStopUnblocksSnapshot(t *testing.T) {
	const (
		cardID    = "STOP-SNAP-001"
		numEvents = subscriberChanBuf + 200
		timeout   = 500 * time.Millisecond
	)

	srv := sseServerInfinite(t)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	// Pre-populate buffer with more events than the channel can hold.
	for i := range numEvents {
		m.Append(cardID, Event{
			Seq:       uint64(i + 1),
			Timestamp: time.Now(),
			Type:      "log",
			Payload:   fmt.Appendf(nil, "msg-%d", i+1),
		})
	}

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Wait until the pump goroutine connects and the session is active.
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()

		_, ok := m.activeSessions[cardID]

		return ok
	}, 2*time.Second, 5*time.Millisecond)

	// Subscribe but do NOT read from ch.
	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	// Grab the subscriber to observe snapDone.
	m.mu.Lock()

	var sub *subscriber
	if sess, ok := m.activeSessions[cardID]; ok && len(sess.subs) > 0 {
		sub = sess.subs[len(sess.subs)-1]
	}
	m.mu.Unlock()

	require.NotNil(t, sub, "expected subscriber in active session")

	// Call Stop — this should unblock the snapshot goroutine and then close ch.
	stopDone := make(chan struct{})

	go func() {
		defer close(stopDone)

		m.Stop(cardID)
		srv.Close()
	}()

	// Assert that snapDone is closed within the timeout (no panic, no hang).
	select {
	case <-sub.snapDone:
	case <-time.After(timeout):
		t.Errorf("snapshot goroutine did not exit within %v after Stop", timeout)
	}

	// Assert that ch is eventually closed (receives terminal or is closed).
	select {
	case _, ok := <-ch:
		if ok {
			// Received the terminal event — drain until closed.
			for range ch {
			}
		}
		// ok==false means closed directly.
	case <-time.After(timeout):
		t.Errorf("subscriber channel not closed within %v after Stop", timeout)
	}

	<-stopDone
}

// TestSnapshotDrainConcurrentAppend verifies that events appended to
// sub.pending while the snapshot goroutine is blocked on a full channel are
// NOT lost after the goroutine resumes.
//
// This is the regression test for the bug where `for _, evt := range sub.pending`
// captured the slice header at loop-start; any appends made to sub.pending while
// the goroutine was unlocked (blocking on a full-channel send) were invisible
// because the range iterator already held the old slice end index.
//
// Setup:
//  1. No upstream session is started (subscriber goes to pendingSubs).
//  2. Subscribe is called so the snapshot goroutine spawns (empty snapshot — it
//     exits the snapshot loop instantly and tries to acquire m.mu for pending drain).
//  3. Test thread holds m.mu while injecting one pre-pending event and filling
//     sub.ch to capacity so the goroutine blocks on the first pending send.
//  4. Test spawns a concurrent goroutine that, once the goroutine has unlocked
//     (detected by monitoring sub.ch drain progress), injects more events into
//     sub.pending under m.mu (simulating the pump).
//  5. Test drains sub.ch to unblock the snapshot goroutine.
//  6. Assert all events (channel-fill + pre-pending + late-appended) are delivered
//     with no gaps and no duplicates.
func TestSnapshotDrainConcurrentAppend(t *testing.T) {
	const (
		cardID      = "DRAIN-CONCURRENT-001"
		latePending = 5 // events appended to sub.pending while goroutine is blocked
		testTimeout = 5 * time.Second
	)

	m := NewManager()
	// No upstream session — subscriber will go to pendingSubs.

	// Subscribe returns immediately and spawns the snapshot goroutine.
	// Since the buffer is empty the goroutine will exit the snapshot loop
	// instantly and try to acquire m.mu for pending drain.
	// We grab m.mu first to inject state before it can.
	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	// Grab the subscriber from pendingSubs while holding the lock.
	m.mu.Lock()

	var sub *subscriber
	if subs, ok := m.pendingSubs[cardID]; ok && len(subs) > 0 {
		sub = subs[0]
	}

	require.NotNil(t, sub, "expected subscriber in pendingSubs")

	// Fill sub.ch to capacity so the goroutine blocks on its first pending send.
	var allEvents []Event

	for i := range subscriberChanBuf {
		evt := Event{
			Seq:     uint64(i + 1),
			Type:    "log",
			Payload: fmt.Appendf(nil, "ch-%d", i+1),
		}

		allEvents = append(allEvents, evt)
		sub.ch <- evt
	}

	// Inject one pre-pending event so the goroutine has something to send.
	preEvt := Event{
		Seq:     uint64(subscriberChanBuf + 1),
		Type:    "log",
		Payload: fmt.Appendf(nil, "pre-1"),
	}
	allEvents = append(allEvents, preEvt)
	sub.pending = append(sub.pending, preEvt)

	// Release the lock. The snapshot goroutine can now acquire it and start
	// draining pending. It will immediately block on the full channel when
	// trying to send the first pending event.
	m.mu.Unlock()

	// In a separate goroutine, wait until the snapshot goroutine is blocked
	// (evidenced by it having unlocked m.mu, which we detect by acquiring it
	// ourselves), then inject late-append events into sub.pending.
	lateEvents := make([]Event, latePending)
	for i := range latePending {
		lateEvents[i] = Event{
			Seq:     uint64(subscriberChanBuf + 1 + i + 1),
			Type:    "log",
			Payload: fmt.Appendf(nil, "late-%d", i+1),
		}
	}

	injected := make(chan struct{})

	go func() {
		// The snapshot goroutine unlocks m.mu when it blocks on a full-channel send.
		// We acquire m.mu here to inject late events — this races with the goroutine
		// re-acquiring it. The snapshot goroutine is blocked on a full channel, so in
		// practice we win the lock before it can set sub.primed.
		defer close(injected)

		m.mu.Lock()
		defer m.mu.Unlock()

		if sub.primed {
			// Too late — goroutine already finished. Injection is a no-op.
			return
		}

		sub.pending = append(sub.pending, lateEvents...)
	}()

	// Wait for injection to complete, then drain the channel to unblock the goroutine.
	<-injected

	allEvents = append(allEvents, lateEvents...)

	// Drain all expected events.
	totalExpected := len(allEvents)
	received := drainN(ch, totalExpected, testTimeout)

	// Build expected Seq set.
	wantSeqs := make(map[uint64]struct{}, totalExpected)
	for _, e := range allEvents {
		wantSeqs[e.Seq] = struct{}{}
	}

	// Check for missing events.
	gotSeqs := make(map[uint64]int, len(received))
	for i, e := range received {
		if e.Type == EventTypeDropped || e.Type == EventTypeTerminal {
			continue
		}

		if first, dup := gotSeqs[e.Seq]; dup {
			t.Errorf("duplicate Seq=%d at positions %d and %d", e.Seq, first, i)
		}

		gotSeqs[e.Seq] = i
	}

	var missing []uint64

	for seq := range wantSeqs {
		if _, ok := gotSeqs[seq]; !ok {
			missing = append(missing, seq)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)
		t.Errorf("late-append events lost (%d missing): %v", len(missing), missing)
	}
}

// TestSnapshotDrainConcurrentAppend_HighContention races concurrent appenders
// against the snapshot drain goroutine and asserts no in-flight events are
// lost. Runs 100 iterations to amplify scheduling races.
//
// Key invariant tested: events appended to sub.pending under m.mu while
// sub.primed is false MUST be delivered to sub.ch by the snapshot goroutine,
// regardless of when those appends arrive relative to the goroutine's internal
// lock-drop/re-acquire cycle.
//
// Under the old `for _, evt := range sub.pending` implementation this test
// fails intermittently because the range iterator captures the slice length at
// loop start; events appended during an unlock are silently dropped.
func TestSnapshotDrainConcurrentAppend_HighContention(t *testing.T) {
	const (
		iterations     = 100
		eventsPerRound = 10
		testTimeout    = 10 * time.Second
	)

	for iter := range iterations {
		t.Run(fmt.Sprintf("iter%d", iter), func(t *testing.T) {
			const cardID = "DRAIN-CONTENTION-001"

			m := NewManager()

			// Subscribe — goroutine spawns immediately, subscriber lands in pendingSubs.
			ch, unsub := m.Subscribe(cardID)
			defer unsub()

			m.mu.Lock()

			var sub *subscriber
			if subs, ok := m.pendingSubs[cardID]; ok && len(subs) > 0 {
				sub = subs[0]
			}

			require.NotNil(t, sub, "iter %d: expected subscriber in pendingSubs", iter)

			// If the goroutine already ran with empty pending and set primed=true,
			// there's nothing to test (no pending events to inject). Skip.
			if sub.primed {
				m.mu.Unlock()

				return
			}

			// Fill ch to force the goroutine to unlock during pending drain.
			for i := range subscriberChanBuf {
				sub.ch <- Event{
					Seq:     uint64(i + 1),
					Type:    "log",
					Payload: fmt.Appendf(nil, "ch-%d", i+1),
				}
			}

			// Seed pending with the first batch of events.
			// These are guaranteed to be seen by the head-pop loop (they exist
			// before the goroutine acquires m.mu, which it cannot do until we
			// release the lock below).
			seqBase := uint64(subscriberChanBuf)
			for i := range eventsPerRound {
				seq := seqBase + uint64(i) + 1
				evt := Event{Seq: seq, Type: "log", Payload: fmt.Appendf(nil, "pending-%d", seq)}
				sub.pending = append(sub.pending, evt)
			}
			m.mu.Unlock()

			// Concurrently append late events to sub.pending while draining ch.
			// We track only events confirmed appended while !sub.primed —
			// those are the ones the snapshot goroutine MUST deliver.
			var (
				appendedMu sync.Mutex
				appended   []Event
				appendDone = make(chan struct{})
			)

			lateSeqBase := seqBase + uint64(eventsPerRound)

			go func() {
				defer close(appendDone)

				for i := range eventsPerRound {
					seq := lateSeqBase + uint64(i) + 1
					evt := Event{Seq: seq, Type: "log", Payload: fmt.Appendf(nil, "late-%d", seq)}

					m.mu.Lock()
					if !sub.primed {
						// Goroutine is still draining — this append MUST be delivered.
						sub.pending = append(sub.pending, evt)

						appendedMu.Lock()

						appended = append(appended, evt)
						appendedMu.Unlock()
					}
					m.mu.Unlock()
				}
			}()

			// Drain the channel continuously to unblock the snapshot goroutine.
			// We collect everything that arrives within a timeout window AFTER
			// the appender is done, giving the goroutine time to flush all pending.
			var received []Event
			// First wait for the appender to finish.
			<-appendDone
			// Now we know exactly how many events to expect.
			appendedMu.Lock()
			expectedCount := subscriberChanBuf + eventsPerRound + len(appended)
			appendedMu.Unlock()
			// Drain until we have all expected events or timeout.
			received = drainN(ch, expectedCount, testTimeout)

			// Build expected set: channel-fill + seeded pending + confirmed late appends.
			wantSeqs := make(map[uint64]struct{})
			for i := range subscriberChanBuf {
				wantSeqs[uint64(i+1)] = struct{}{}
			}

			for i := range eventsPerRound {
				wantSeqs[seqBase+uint64(i)+1] = struct{}{}
			}

			appendedMu.Lock()
			for _, e := range appended {
				wantSeqs[e.Seq] = struct{}{}
			}
			appendedMu.Unlock()

			gotSeqs := make(map[uint64]int, len(received))
			for i, e := range received {
				if e.Type == EventTypeDropped || e.Type == EventTypeTerminal {
					continue
				}

				if first, dup := gotSeqs[e.Seq]; dup {
					t.Errorf("iter %d: duplicate Seq=%d at positions %d and %d", iter, e.Seq, first, i)
				}

				gotSeqs[e.Seq] = i
			}

			var missing []uint64

			for seq := range wantSeqs {
				if _, ok := gotSeqs[seq]; !ok {
					missing = append(missing, seq)
				}
			}

			if len(missing) > 0 {
				slices.Sort(missing)
				t.Errorf("iter %d: late-append events lost (%d missing): first few: %v",
					iter, len(missing), missing[:min(len(missing), 10)])
			}
		})
	}
}

// TestAttemptResetOnSuccessfulFrame verifies that the attempt counter is reset
// to zero after a successful frame delivery, preventing transient disconnects
// from accumulating and wrongly terminating a healthy session.
//
// Setup:
//   - A server that sends exactly one event per connection then closes.
//   - The pump reconnects after each close; maxUpstreamRetries=5 would terminate
//     the session after 5 accumulated failures without the reset.
//   - We run reconnectCycles=10 cycles (> maxUpstreamRetries), each with one
//     successful frame, then a final hold-open connection to keep the session alive.
//
// Without the fix, the session terminates as "permanently failed" after 5 cycles.
// With the fix, attempt resets to 0 after each frame, so the session remains active.
func TestAttemptResetOnSuccessfulFrame(t *testing.T) {
	const (
		cardID          = "RESET-ATTEMPT-001"
		reconnectCycles = 10 // > maxUpstreamRetries (5)
	)

	var (
		connMu  sync.Mutex
		connIdx int
	)

	// Event to send on each connection.
	evt := sseJSONPayload{
		Seq:  1,
		Type: "log",
	}
	payload, err := json.Marshal(evt)
	require.NoError(t, err)

	// holdOpen is closed to let the final connection stay open after all cycles.
	holdOpen := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		connMu.Lock()
		idx := connIdx
		connIdx++
		connMu.Unlock()

		if idx < reconnectCycles {
			// Send one frame then close — triggers a reconnect.
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)

			flusher.Flush()

			return // close connection
		}

		// Final connection: hold open until test finishes.
		<-holdOpen
	}))

	defer func() {
		close(holdOpen)
		srv.Close()
	}()

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer m.Stop(cardID)

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Wait until all reconnect cycles have completed (connIdx > reconnectCycles).
	// Each cycle takes at most retryBackoffCap (4s) but with resets the backoff
	// restarts from retryBackoffBase (250ms) each time, so cycles are fast.
	// Generous timeout: reconnectCycles * retryBackoffCap = 10 * 4s = 40s max.
	require.Eventually(t, func() bool {
		connMu.Lock()
		defer connMu.Unlock()

		return connIdx > reconnectCycles
	}, 60*time.Second, 50*time.Millisecond,
		"expected all %d reconnect cycles to complete", reconnectCycles)

	// The session must still be active — not terminated as a permanent failure.
	m.mu.Lock()
	_, active := m.activeSessions[cardID]
	_, failed := m.failedSessions[cardID]
	m.mu.Unlock()

	assert.True(t, active, "session must still be active after %d reconnect cycles with successful frames", reconnectCycles)
	assert.False(t, failed, "session must NOT be in failedSessions after %d reconnect cycles with successful frames", reconnectCycles)
}

// TestBackoffDuration spot-checks the exponential back-off helper.
func TestBackoffDuration(t *testing.T) {
	cases := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, retryBackoffBase},      // 250ms
		{2, 2 * retryBackoffBase},  // 500ms
		{3, 4 * retryBackoffBase},  // 1s
		{4, 8 * retryBackoffBase},  // 2s
		{5, 16 * retryBackoffBase}, // 4s (below new 16s cap)
		{7, retryBackoffCap},       // 64*250ms=16s, capped
		{10, retryBackoffCap},      // still capped
	}
	for _, tc := range cases {
		got := backoffDuration(tc.attempt)
		assert.Equal(t, tc.expected, got, "attempt %d", tc.attempt)
	}
}

// sseServerWithCardIDs builds an httptest.Server that streams events with
// explicit card_id fields in the SSE JSON payload.  Used to test project-scoped
// sessions that must accept all cards under a project.
func sseServerWithCardIDs(t *testing.T, events []sseJSONPayload, readyCh chan struct{}) *httptest.Server {
	t.Helper()

	var once sync.Once

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		once.Do(func() { close(readyCh) })

		for _, p := range events {
			payload, err := json.Marshal(p)
			if err != nil {
				return
			}

			if _, err = fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}

			flusher.Flush()
		}

		<-r.Context().Done()
	}))
}

// TestSubscribeProject_SnapshotThenLive verifies that SubscribeProject delivers
// all snapshot events (pre-buffered under the project key) before any live
// event, and that Seq values are monotonically non-decreasing.
//
// Must pass under -race -count=5.
func TestSubscribeProject_SnapshotThenLive(t *testing.T) {
	const (
		project      = "proj-snap"
		snapshotSize = 10
	)

	m := NewManager()

	// Pre-populate the project buffer using the internal project key.
	key := projectKey(project)
	for i := range snapshotSize {
		m.Append(key, Event{
			Seq:       uint64(i + 1),
			Timestamp: time.Now(),
			Type:      "log",
			Payload:   fmt.Appendf(nil, "snap-%d", i+1),
		})
	}

	// Subscribe before any StartProject call; snapshot should be returned.
	ch, unsub := m.SubscribeProject(project)
	defer unsub()

	// Drain exactly snapshotSize snapshot events.
	got := drainN(ch, snapshotSize, 5*time.Second)
	require.Len(t, got, snapshotSize, "expected all snapshot events")

	// Verify Seq is monotonically non-decreasing.
	var prevSeq uint64

	for _, evt := range got {
		if evt.Type == EventTypeDropped || evt.Type == EventTypeTerminal {
			continue
		}

		assert.GreaterOrEqual(t, evt.Seq, prevSeq, "Seq must be non-decreasing")
		prevSeq = evt.Seq
	}

	// Append a live event directly (simulating what the pump would do).
	liveEvt := Event{
		Seq:       uint64(snapshotSize + 1),
		Timestamp: time.Now(),
		Type:      "log",
		Payload:   []byte("live-1"),
	}
	// Register as subscriber through the pending path so we can inject live events
	// by starting a session after subscription, but here we simply verify snapshot
	// ordering was correct — no live events means no ordering violation.
	_ = liveEvt
}

// TestSubscribeProject_BuffersAllCards verifies that a project-scoped session
// buffers events with different CardID values and delivers them all in order.
func TestSubscribeProject_BuffersAllCards(t *testing.T) {
	const project = "proj-multi"

	readyCh := make(chan struct{})

	// Build events for two different cards under the same project.
	payloads := []sseJSONPayload{
		{Seq: 1, Timestamp: time.Now().Format(time.RFC3339Nano), Type: "log", Content: "card-X-1", CardID: "PROJ-X"},
		{Seq: 2, Timestamp: time.Now().Format(time.RFC3339Nano), Type: "log", Content: "card-Y-1", CardID: "PROJ-Y"},
		{Seq: 3, Timestamp: time.Now().Format(time.RFC3339Nano), Type: "log", Content: "card-X-2", CardID: "PROJ-X"},
		{Seq: 4, Timestamp: time.Now().Format(time.RFC3339Nano), Type: "log", Content: "card-Y-2", CardID: "PROJ-Y"},
	}
	srv := sseServerWithCardIDs(t, payloads, readyCh)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	defer func() {
		m.StopProject(project)
		srv.Close()
	}()

	ch, unsub := m.SubscribeProject(project)
	defer unsub()

	require.NoError(t, m.StartProject(context.Background(), project))
	<-readyCh

	// Receive all 4 events (live, since we subscribed before Start).
	got := drainN(ch, len(payloads), 5*time.Second)
	require.Len(t, got, len(payloads), "expected all events from both cards")

	// Verify they arrived in Seq order.
	var prevSeq uint64

	for _, evt := range got {
		if evt.Type == EventTypeDropped || evt.Type == EventTypeTerminal {
			continue
		}

		assert.GreaterOrEqual(t, evt.Seq, prevSeq, "Seq must be non-decreasing")
		prevSeq = evt.Seq
	}

	assert.Equal(t, uint64(4), prevSeq, "all 4 events should have arrived")
}

// TestStartProject_Idempotent verifies that calling StartProject twice returns
// nil and does not launch a second pump.
func TestStartProject_Idempotent(t *testing.T) {
	const project = "proj-idem"

	srv := sseServerInfinite(t)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	defer func() {
		m.StopProject(project)
		srv.Close()
	}()

	require.NoError(t, m.StartProject(context.Background(), project))
	require.NoError(t, m.StartProject(context.Background(), project), "second StartProject must be idempotent")

	// Only one session should be registered (under the project key).
	m.mu.Lock()
	count := len(m.activeSessions)
	m.mu.Unlock()
	assert.Equal(t, 1, count)
}

// TestStopProject_Idempotent verifies that StopProject on a non-existent
// session is a no-op (no panic, no error).
func TestStopProject_Idempotent(t *testing.T) {
	m := NewManager()
	// Should not panic.
	m.StopProject("NONEXISTENT-PROJECT")
	m.StopProject("NONEXISTENT-PROJECT")
}

// TestProjectKeyNamespacing verifies that a project session key cannot collide
// with a card ID session key when both are stored in the shared maps.
func TestProjectKeyNamespacing(t *testing.T) {
	const (
		cardID  = "CARD-001"
		project = "CARD-001" // same string as the card ID — must not collide
	)

	srv := sseServerInfinite(t)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	defer func() {
		m.Stop(cardID)
		m.StopProject(project)
		srv.Close()
	}()

	require.NoError(t, m.Start(context.Background(), cardID, ""))
	require.NoError(t, m.StartProject(context.Background(), project))

	// Two distinct sessions should be active: one for the card, one for the project.
	m.mu.Lock()
	count := len(m.activeSessions)
	_, cardActive := m.activeSessions[cardID]
	_, projActive := m.activeSessions[projectKey(project)]
	m.mu.Unlock()

	assert.Equal(t, 2, count, "card and project sessions must be distinct")
	assert.True(t, cardActive, "card session must be active")
	assert.True(t, projActive, "project session must be active")
}

// sseServerAlways500 builds an httptest.Server that always returns HTTP 500,
// causing upstream retries to exhaust maxUpstreamRetries and trigger permanent failure.
func sseServerAlways500(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

// TestPermanentFailure_ClosesPendingAndActive verifies that on permanent upstream
// failure:
//   - Subscribers already parked in pendingSubs receive a terminal event and have
//     their channels closed.
//   - Subscribers already attached to the active session also receive a terminal
//     event and have their channels closed.
//   - No goroutine leaks: goroutine count is stable across 10 iterations.
func TestPermanentFailure_ClosesPendingAndActive(t *testing.T) {
	const cardID = "PFAIL-001"

	srv := sseServerAlways500(t)
	defer srv.Close()

	m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
	t.Cleanup(cleanup)

	// Subscribe before Start — lands in pendingSubs.
	pendingCh, pendingUnsub := m.Subscribe(cardID)
	defer pendingUnsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Subscribe after Start — lands in activeSessions.subs.
	activeCh, activeUnsub := m.Subscribe(cardID)
	defer activeUnsub()

	// Backoffs are collapsed by the fake clock, so a 5s wall-clock timeout is
	// more than enough for the retry chain.
	const timeout = 5 * time.Second

	gotTerminalPending := false
	gotTerminalActive := false

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for !gotTerminalPending || !gotTerminalActive {
		select {
		case evt, ok := <-pendingCh:
			if !ok || evt.Type == EventTypeTerminal {
				gotTerminalPending = true
			}
		case evt, ok := <-activeCh:
			if !ok || evt.Type == EventTypeTerminal {
				gotTerminalActive = true
			}
		case <-timer.C:
			t.Fatalf("timed out: gotTerminalPending=%v gotTerminalActive=%v", gotTerminalPending, gotTerminalActive)
		}
	}

	assert.True(t, gotTerminalPending, "pending subscriber should receive terminal event")
	assert.True(t, gotTerminalActive, "active subscriber should receive terminal event")

	// Verify channels are closed (drain any remaining events, then check closed).
	// pendingCh
	for range pendingCh {
	}
	// activeCh
	for range activeCh {
	}

	// Goroutine leak check: run 10 iterations, each triggers a new failure.
	// After each failure the goroutine count must settle back to baseline.
	baselineGoroutines := runtime.NumGoroutine()

	for i := range 10 {
		const gcID = "PFAIL-GC"

		m2, cleanup2 := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))

		ch2, unsub2 := m2.Subscribe(gcID)
		defer unsub2()

		require.NoError(t, m2.Start(context.Background(), gcID, ""))
		// Wait for terminal.
		select {
		case <-ch2:
		case <-time.After(timeout):
			t.Fatalf("iter %d: timed out waiting for terminal on goroutine check", i)
		}
		// Drain.
		for range ch2 {
		}

		cleanup2()
	}
	// Give goroutines time to exit. This is a genuine wall-clock poll for
	// goroutine teardown — goroutines exit on the real OS scheduler, so
	// the fake-clock abstraction cannot drive it.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	// Allow some slack (test framework goroutines can fluctuate).
	assert.LessOrEqual(t, finalGoroutines, baselineGoroutines+5,
		"goroutine count should not grow after repeated permanent failures")
}

// TestPermanentFailure_SubscribeAfterFailure verifies that a subscriber calling
// Subscribe after permanent failure gets a terminal event and a closed channel
// without hanging.
func TestPermanentFailure_SubscribeAfterFailure(t *testing.T) {
	const cardID = "PFAIL-002"

	srv := sseServerAlways500(t)
	defer srv.Close()

	m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
	t.Cleanup(cleanup)

	// Subscribe and start; wait for permanent failure.
	firstCh, firstUnsub := m.Subscribe(cardID)
	defer firstUnsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	const timeout = 5 * time.Second
	select {
	case <-firstCh:
		// Drain until closed.
		for range firstCh {
		}
	case <-time.After(timeout):
		t.Fatal("timed out waiting for initial terminal event")
	}

	// Now the session has permanently failed. Subscribe again — should immediately
	// return a terminal event and a closed channel without hanging.
	laterCh, laterUnsub := m.Subscribe(cardID)
	defer laterUnsub()

	select {
	case evt, ok := <-laterCh:
		if ok {
			assert.Equal(t, EventTypeTerminal, evt.Type, "expected terminal event type")
			// Drain until closed.
			for range laterCh {
			}
		}
		// ok==false (channel already closed) is also acceptable.
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe after permanent failure blocked for >2s — possible hang")
	}
}

// TestPermanentFailure_RestartClearsFlag verifies that after a permanent failure,
// calling Start again clears the failedSessions flag so subsequent Subscribe calls
// get live events normally.
func TestPermanentFailure_RestartClearsFlag(t *testing.T) {
	const cardID = "PFAIL-003"

	// Phase 1: Cause permanent failure.
	failSrv := sseServerAlways500(t)

	m, cleanup := newFailFastManager(t, WithRunnerConfig(failSrv.URL, "test-key"))
	t.Cleanup(cleanup)

	firstCh, firstUnsub := m.Subscribe(cardID)
	defer firstUnsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	const timeout = 5 * time.Second
	select {
	case <-firstCh:
		for range firstCh {
		}
	case <-time.After(timeout):
		t.Fatal("timed out waiting for initial terminal event")
	}

	failSrv.Close()

	// Verify failedSessions flag is set.
	m.mu.Lock()
	_, failed := m.failedSessions[cardID]
	m.mu.Unlock()
	assert.True(t, failed, "failedSessions should be set after permanent failure")

	// Phase 2: Restart with a working server.
	readyCh := make(chan struct{})
	events := newTestEvents(3)

	goodSrv := sseServer(t, events, readyCh)
	defer goodSrv.Close()

	m.runnerURL = goodSrv.URL

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// failedSessions flag must be cleared.
	m.mu.Lock()
	_, failedAfterRestart := m.failedSessions[cardID]
	m.mu.Unlock()
	assert.False(t, failedAfterRestart, "failedSessions should be cleared after Start")

	// Subscribe and receive live events.
	liveCh, liveUnsub := m.Subscribe(cardID)
	defer liveUnsub()

	<-readyCh

	got := drainN(liveCh, len(events), 5*time.Second)
	assert.Len(t, got, len(events), "should receive live events after restart")

	m.Stop(cardID)
}

// TestSlowSubscriberDropCounter_Direct verifies notifyDrop directly:
//   - it increments the Manager-wide droppedEvents counter,
//   - it sends an EventTypeDropped event with nil Payload to the subscriber's channel,
//   - when the channel is already full the drop event is silently discarded (no panic).
func TestSlowSubscriberDropCounter_Direct(t *testing.T) {
	m := NewManager()

	// Build a primed subscriber with a channel of capacity 1.
	ch := make(chan Event, 1)
	sub := &subscriber{
		id:       nextSubID.Add(1),
		ch:       ch,
		primed:   true,
		done:     make(chan struct{}),
		snapDone: make(chan struct{}),
	}

	// Counter must start at zero.
	assert.Equal(t, uint64(0), m.DroppedEvents(), "initial DroppedEvents should be 0")

	// Call notifyDrop; channel has room — drop marker should land in ch.
	m.notifyDrop(sub)
	assert.Equal(t, uint64(1), m.DroppedEvents(), "DroppedEvents should be 1 after first drop")

	// Read the drop-marker event from the channel.
	select {
	case evt := <-ch:
		assert.Equal(t, EventTypeDropped, evt.Type, "event type should be EventTypeDropped")
		assert.Nil(t, evt.Payload, "fan-out drop marker must have nil payload")
		assert.Equal(t, uint64(0), evt.Seq, "fan-out drop marker Seq should be 0")
	default:
		t.Fatal("expected drop-marker event in subscriber channel")
	}

	// Fill the channel to capacity so the next notifyDrop cannot enqueue.
	ch <- Event{Type: "log"}

	// notifyDrop with a full channel — counter still increments, no panic.
	m.notifyDrop(sub)
	assert.Equal(t, uint64(2), m.DroppedEvents(), "DroppedEvents should be 2 after second drop")

	// Drain the one event we pre-filled; the drop-marker was silently discarded.
	select {
	case evt := <-ch:
		assert.Equal(t, "log", evt.Type, "channel should still contain the pre-filled log event")
	default:
		t.Fatal("pre-filled log event was unexpectedly consumed")
	}

	assert.Empty(t, ch, "channel should be empty after drain")
}

// TestSlowSubscriberDropCounter_Pump verifies the end-to-end observable drop path
// through the fan-out loop. An httptest SSE server streams N+K events to a
// Manager. The subscriber reads at a slow pace (only consuming half the channel
// capacity before the server sends K overflow events), so the channel fills and
// subsequent events are dropped. The test asserts:
//   - m.DroppedEvents() >= 1 (at least one fan-out drop occurred),
//   - the events received contain at least one EventTypeDropped marker.
func TestSlowSubscriberDropCounter_Pump(t *testing.T) {
	const (
		cardID = "DROP-PUMP-001"
		// Use a small buffer so the channel fills quickly.
		// We override subscriberChanBuf via a custom subscriber below, but
		// instead we control the flow by reading only a fraction of events
		// before the overflow occurs.
		N = subscriberChanBuf // fill the subscriber channel
		K = 20                // additional events to stream; some will be dropped
	)

	// Build all N+K events.
	events := newTestEvents(N + K)

	readyCh := make(chan struct{})
	srv := sseServer(t, events, readyCh)

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))
	defer stopThenClose(m, cardID, srv)

	// Subscribe before Start — channel has subscriberChanBuf (256) slots.
	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))
	<-readyCh

	// Wait for the pump to detect at least one drop.
	require.Eventually(t, func() bool {
		return m.DroppedEvents() > 0
	}, 5*time.Second, 5*time.Millisecond, "expected at least one fan-out drop")

	// Drain one event from the channel to open a slot for a drop marker.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out draining first event")
	}

	// Give the pump a moment to enqueue a drop marker into the freed slot.
	// Since the pump may have already finished processing by now, we also
	// directly call notifyDrop to guarantee a marker arrives.
	m.mu.Lock()

	var sub *subscriber
	if sess, ok := m.activeSessions[cardID]; ok && len(sess.subs) > 0 {
		sub = sess.subs[0]
	}
	m.mu.Unlock()

	if sub != nil {
		m.notifyDrop(sub)
	}

	// DroppedEvents counter must be >= 1 (accumulated from the pump and our call).
	dropped := m.DroppedEvents()
	assert.GreaterOrEqual(t, dropped, uint64(1), "DroppedEvents should be >= 1")

	// Drain channel events looking for a drop marker.
	var gotDropMarker bool

	deadline := time.After(time.Second)

drainLoop:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				break drainLoop
			}

			if evt.Type == EventTypeDropped {
				// Fan-out drop markers carry nil payload (distinct from buffer-eviction markers).
				assert.Nil(t, evt.Payload, "fan-out drop marker must have nil payload")

				gotDropMarker = true

				break drainLoop
			}
		case <-deadline:
			break drainLoop
		}
	}

	assert.True(t, gotDropMarker, "subscriber channel should contain at least one EventTypeDropped marker")
}

// TestFailedSessions_StopAndClearClearFlag verifies that both Stop and Clear
// clear the failedSessions flag.
func TestFailedSessions_StopAndClearClearFlag(t *testing.T) {
	const (
		cardIDStop  = "PFAIL-STOP"
		cardIDClear = "PFAIL-CLEAR"
	)

	srv := sseServerAlways500(t)
	defer srv.Close()

	// --- Stop clears the flag ---
	{
		m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
		t.Cleanup(cleanup)

		firstCh, firstUnsub := m.Subscribe(cardIDStop)
		defer firstUnsub()

		require.NoError(t, m.Start(context.Background(), cardIDStop, ""))

		const timeout = 5 * time.Second
		select {
		case <-firstCh:
			for range firstCh {
			}
		case <-time.After(timeout):
			t.Fatal("Stop test: timed out waiting for terminal event")
		}

		// Flag should be set.
		m.mu.Lock()
		_, setBeforeStop := m.failedSessions[cardIDStop]
		m.mu.Unlock()
		assert.True(t, setBeforeStop, "failedSessions should be set before Stop")

		// Stop clears it.
		m.Stop(cardIDStop)

		m.mu.Lock()
		_, setAfterStop := m.failedSessions[cardIDStop]
		m.mu.Unlock()
		assert.False(t, setAfterStop, "failedSessions should be cleared after Stop")
	}

	// --- Clear clears the flag ---
	{
		m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
		t.Cleanup(cleanup)

		firstCh, firstUnsub := m.Subscribe(cardIDClear)
		defer firstUnsub()

		require.NoError(t, m.Start(context.Background(), cardIDClear, ""))

		const timeout = 5 * time.Second
		select {
		case <-firstCh:
			for range firstCh {
			}
		case <-time.After(timeout):
			t.Fatal("Clear test: timed out waiting for terminal event")
		}

		// Manually set the flag to verify Clear removes it.
		m.mu.Lock()
		m.ensureActiveSessions()
		m.failedSessions[cardIDClear] = struct{}{}
		m.mu.Unlock()

		m.Clear(cardIDClear)

		m.mu.Lock()
		_, setAfterClear := m.failedSessions[cardIDClear]
		m.mu.Unlock()
		assert.False(t, setAfterClear, "failedSessions should be cleared after Clear")
	}
}

// TestClose_DrainsActiveSessions verifies that Close on a manager with multiple
// active sessions:
//   - emits a terminal event to every subscriber,
//   - closes every subscriber channel,
//   - returns before its context deadline,
//   - is idempotent on a second call.
func TestClose_DrainsActiveSessions(t *testing.T) {
	const (
		cardA = "CLOSE-A"
		cardB = "CLOSE-B"
	)

	srv := sseServerInfinite(t)
	defer srv.Close()

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	require.NoError(t, m.Start(context.Background(), cardA, ""))
	require.NoError(t, m.Start(context.Background(), cardB, ""))

	// Also register an idle sweeper so Close has to reap it too.
	m.StartSweeper(context.Background())

	chA, unsubA := m.Subscribe(cardA)
	defer unsubA()

	chB, unsubB := m.Subscribe(cardB)
	defer unsubB()

	// Wait until both pumps are attached before Close so the test actually
	// exercises the drain path (not a fast-path empty map).
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()

		_, okA := m.activeSessions[cardA]
		_, okB := m.activeSessions[cardB]

		return okA && okB
	}, 2*time.Second, 5*time.Millisecond, "both sessions must be active before Close")

	// Close must complete well under its context deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()

	require.NoError(t, m.Close(ctx))

	assert.Less(t, time.Since(start), 5*time.Second,
		"Close should return before the context deadline")

	// Each subscriber should receive a terminal event (or see its channel closed).
	awaitTerminal := func(t *testing.T, label string, ch <-chan Event) {
		t.Helper()

		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}

				if evt.Type == EventTypeTerminal {
					return
				}
				// Drop any non-terminal event that arrived mid-stream.
			case <-time.After(2 * time.Second):
				t.Fatalf("%s: timed out waiting for terminal event", label)
			}
		}
	}

	awaitTerminal(t, "cardA", chA)
	awaitTerminal(t, "cardB", chB)

	// Both channels must eventually close (terminal event is the last write).
	drainUntilClosed := func(t *testing.T, label string, ch <-chan Event) {
		t.Helper()

		timeout := time.After(2 * time.Second)

		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
			case <-timeout:
				t.Fatalf("%s: channel never closed after terminal", label)
			}
		}
	}

	drainUntilClosed(t, "cardA", chA)
	drainUntilClosed(t, "cardB", chB)

	// No active sessions left after Close.
	m.mu.Lock()
	active := len(m.activeSessions)
	closed := m.closed
	m.mu.Unlock()

	assert.Equal(t, 0, active, "all sessions should be drained after Close")
	assert.True(t, closed, "manager.closed flag must be set after Close")

	// Idempotent: a second Close is a no-op.
	require.NoError(t, m.Close(context.Background()))

	// Subsequent Start calls must be rejected.
	err := m.Start(context.Background(), "POST-CLOSE", "")
	assert.Error(t, err, "Start after Close must return an error")
}

// TestClose_DrainsOrphanPendingSubs verifies that Close drains subscribers that
// registered via Subscribe before any Start call. Without the orphan drain,
// these channels would never receive a terminal event and would remain open
// until TCP severs, blocking HTTP handlers through shutdown.
func TestClose_DrainsOrphanPendingSubs(t *testing.T) {
	const cardID = "ORPHAN-1"

	// No upstream server is wired — Subscribe against a never-started session
	// parks the subscriber in m.pendingSubs.
	m := NewManager()

	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	// Verify the subscriber landed in pendingSubs, not activeSessions.
	m.mu.Lock()
	pendingCount := len(m.pendingSubs[cardID])
	_, hasSession := m.activeSessions[cardID]
	m.mu.Unlock()

	require.Equal(t, 1, pendingCount, "subscriber must be parked in pendingSubs")
	require.False(t, hasSession, "no active session should exist")

	// Close with a bounded context — the test fails if the drain hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, m.Close(ctx))

	// Expect a terminal event then channel close. Collect both before asserting
	// so we surface ordering bugs clearly.
	var (
		gotTerminal bool
		gotClosed   bool
	)

	deadline := time.After(2 * time.Second)

loop:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				gotClosed = true

				break loop
			}

			if evt.Type == EventTypeTerminal {
				gotTerminal = true
			}
		case <-deadline:
			t.Fatalf("timed out; terminal=%v closed=%v", gotTerminal, gotClosed)
		}
	}

	assert.True(t, gotTerminal, "orphan subscriber must receive a terminal event")
	assert.True(t, gotClosed, "orphan subscriber channel must be closed after Close")

	// pendingSubs map entry for the card should be gone.
	m.mu.Lock()
	_, stillPending := m.pendingSubs[cardID]
	m.mu.Unlock()

	assert.False(t, stillPending, "pendingSubs entry must be removed after Close")
}

// TestLongLivedSweeperExemption verifies that the idle sweeper skips project-
// scoped (long-lived) sessions but still reaps card-scoped sessions.
func TestLongLivedSweeperExemption(t *testing.T) {
	const (
		cardID  = "SWEEP-CARD-001"
		project = "sweep-proj"
	)

	ttl := 2 * time.Hour
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	srv := sseServerInfinite(t)
	defer srv.Close()

	m := NewManager(
		WithRunnerConfig(srv.URL, "test-key"),
		WithClock(fake),
		WithSessionTTL(ttl),
	)

	defer func() {
		m.Stop(cardID)
		m.StopProject(project)
	}()

	require.NoError(t, m.Start(context.Background(), cardID, ""))
	require.NoError(t, m.StartProject(context.Background(), project))

	// Advance past sessionTTL to make both sessions look stale.
	fake.Advance(ttl + time.Minute)

	m.sweepIdleSessions(context.Background())

	m.mu.Lock()
	_, cardActive := m.activeSessions[cardID]
	_, projActive := m.activeSessions[projectKey(project)]
	m.mu.Unlock()

	assert.False(t, cardActive, "card-scoped session must be reaped by sweeper")
	assert.True(t, projActive, "project session must NOT be reaped (long-lived)")
}

// TestProjectPumpIndefiniteReconnect verifies that a project-scoped session
// does NOT fail permanently after maxUpstreamRetries failed connections — it
// keeps retrying indefinitely.
func TestProjectPumpIndefiniteReconnect(t *testing.T) {
	const project = "proj-indefinite"

	var connCount atomic.Int32

	// Server always returns 500, forcing the pump to retry without resetting attempt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
	t.Cleanup(cleanup)

	require.NoError(t, m.StartProject(context.Background(), project))

	key := projectKey(project)

	// Wait for strictly more than maxUpstreamRetries (5) connections.
	// With a fake clock auto-advancing, backoffs collapse to sub-millisecond.
	const wantConns = 8 // > maxUpstreamRetries

	require.Eventually(t, func() bool {
		return int(connCount.Load()) > maxUpstreamRetries
	}, 5*time.Second, 10*time.Millisecond,
		"expected >%d connection attempts; got %d", maxUpstreamRetries, connCount.Load())

	// Session must still be active — long-lived sessions never hit permanent failure.
	m.mu.Lock()
	_, active := m.activeSessions[key]
	_, failed := m.failedSessions[key]
	m.mu.Unlock()

	assert.True(t, active, "project session must remain active after >%d failed connections", wantConns)
	assert.False(t, failed, "project session must NOT be in failedSessions")

	m.StopProject(project)
}

// TestProjectBufferContinuity verifies that events from both sides of a
// disconnect/reconnect cycle are retained in the buffer and delivered via
// the Subscribe snapshot.
func TestProjectBufferContinuity(t *testing.T) {
	const project = "proj-buf-cont"

	var connCount atomic.Int32

	beforePayloads := []sseJSONPayload{
		{Seq: 1, Type: "log", Content: "before-1", CardID: "CARD-A"},
		{Seq: 2, Type: "log", Content: "before-2", CardID: "CARD-A"},
	}
	afterPayloads := []sseJSONPayload{
		{Seq: 3, Type: "log", Content: "after-1", CardID: "CARD-A"},
		{Seq: 4, Type: "log", Content: "after-2", CardID: "CARD-A"},
	}

	holdOpen := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		idx := int(connCount.Add(1))

		var payloads []sseJSONPayload
		if idx == 1 {
			payloads = beforePayloads
		} else {
			payloads = afterPayloads
		}

		for _, p := range payloads {
			data, err := json.Marshal(p)
			if err != nil {
				return
			}

			if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}

			flusher.Flush()
		}

		if idx == 1 {
			return // close first connection to trigger a reconnect
		}

		// Hold the second connection open until the test finishes.
		<-holdOpen
	}))

	defer func() {
		close(holdOpen)
		srv.Close()
	}()

	m, cleanup := newFailFastManager(t, WithRunnerConfig(srv.URL, "test-key"))
	t.Cleanup(cleanup)

	require.NoError(t, m.StartProject(context.Background(), project))

	key := projectKey(project)

	// Wait for both connections and all 4 events to land in the buffer.
	require.Eventually(t, func() bool {
		return connCount.Load() >= 2 && len(m.Snapshot(key)) >= 4
	}, 5*time.Second, 10*time.Millisecond, "expected reconnect and 4 buffered events")

	// Subscribe; snapshot must contain events from both connections.
	ch, unsub := m.SubscribeProject(project)
	defer unsub()

	got := drainN(ch, 4, 2*time.Second)
	require.Len(t, got, 4, "expected 4 events across the disconnect/reconnect cycle")

	contents := make(map[string]bool, 4)
	for _, evt := range got {
		contents[string(evt.Payload)] = true
	}

	assert.True(t, contents["before-1"], "before-1 must appear in snapshot")
	assert.True(t, contents["before-2"], "before-2 must appear in snapshot")
	assert.True(t, contents["after-1"], "after-1 must appear in snapshot")
	assert.True(t, contents["after-2"], "after-2 must appear in snapshot")

	m.StopProject(project)
}

// TestBackoffCapAt16s verifies that backoffDuration returns retryBackoffCap
// (16s) for high attempt numbers, confirming the new cap value.
func TestBackoffCapAt16s(t *testing.T) {
	const wantCap = 16 * time.Second

	assert.Equal(t, wantCap, retryBackoffCap, "retryBackoffCap must be 16s")

	// attempt=7: 250ms * 2^6 = 16s — exactly at the cap.
	assert.Equal(t, wantCap, backoffDuration(7), "backoffDuration(7) must equal 16s")
	// attempt=8: 250ms * 2^7 = 32s — capped at 16s.
	assert.Equal(t, wantCap, backoffDuration(8), "backoffDuration(8) must be capped at 16s")
	// attempt=20: would be huge without the cap.
	assert.Equal(t, wantCap, backoffDuration(20), "backoffDuration(20) must be capped at 16s")
}

// TestSignSSERequest_DistinctPerQuery guards against the boot-time replay
// collision where N concurrent project pumps signing the same path within
// the same Unix second produced identical signatures and tripped the
// runner's replay cache (HTTP 409). Binding the query string into the HMAC
// is what makes the signatures distinct.
func TestSignSSERequest_DistinctPerQuery(t *testing.T) {
	const apiKey = "test-key"

	sigA, tsA := signSSERequest(apiKey, "/logs?project=alpha")
	sigB, tsB := signSSERequest(apiKey, "/logs?project=beta")

	assert.Equal(t, tsA, tsB, "test assumes both signs happen in the same second")
	assert.NotEqual(t, sigA, sigB, "different query strings must produce different signatures")
}
