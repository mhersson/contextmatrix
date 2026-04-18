package sessionlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
