package sessionlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	const cardID = "MGR-002"
	const numSubs = 5
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
	const cardID = "SWEEP-001"
	const ttl = 100 * time.Millisecond

	srv := sseServerInfinite(t)
	defer srv.Close()

	m := NewManager(
		WithRunnerConfig(srv.URL, "test-key"),
		WithSessionTTL(ttl),
	)

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// Wait past TTL, then trigger sweeper directly (avoids waiting for ticker).
	time.Sleep(ttl + 20*time.Millisecond)
	m.sweepIdleSessions()

	// Session should be gone.
	m.mu.Lock()
	_, running := m.activeSessions[cardID]
	m.mu.Unlock()
	assert.False(t, running, "session should have been swept")
}

// TestUpstreamRetryAndError verifies that after maxUpstreamRetries failed
// connections the session is removed and subscribers receive a terminal event.
func TestUpstreamRetryAndError(t *testing.T) {
	const cardID = "RETRY-001"

	// Server that immediately returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := NewManager(WithRunnerConfig(srv.URL, "test-key"))

	ch, unsub := m.Subscribe(cardID)
	defer unsub()

	require.NoError(t, m.Start(context.Background(), cardID, ""))

	// The pump retries with backoffs: 250ms, 500ms, 1s, 2s (4 retries = ~3.75s).
	// Use a generous timeout to avoid flakiness.
	var gotTerminal bool
	select {
	case evt, ok := <-ch:
		if !ok || evt.Type == EventTypeTerminal {
			gotTerminal = true
		}
	case <-time.After(20 * time.Second):
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
			var prevSeq uint64
			var seenLive bool
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
		cardID     = "TAIL-001"
		numEvents  = 1000
		readDelay  = 100 * time.Microsecond
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

// TestBackoffDuration spot-checks the exponential back-off helper.
func TestBackoffDuration(t *testing.T) {
	cases := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, retryBackoffBase},     // 250ms
		{2, 2 * retryBackoffBase}, // 500ms
		{3, 4 * retryBackoffBase}, // 1s
		{4, 8 * retryBackoffBase}, // 2s
		{5, retryBackoffCap},      // 16*250ms=4s, capped
		{10, retryBackoffCap},     // still capped
	}
	for _, tc := range cases {
		got := backoffDuration(tc.attempt)
		assert.Equal(t, tc.expected, got, "attempt %d", tc.attempt)
	}
}
