package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// flushRecorder is an httptest.ResponseRecorder that supports Flush().
// It additionally serialises reads/writes of Body so tests can poll
// bodySnapshot() from another goroutine without triggering -race.
type flushRecorder struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushed int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	f.flushed++
	f.mu.Unlock()
}

func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ResponseRecorder.Write(p)
}

// bodySnapshot returns a point-in-time copy of the body so callers can read
// it without racing against ongoing handler writes.
func (f *flushRecorder) bodySnapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Body.String()
}

// subscribedHook returns a hook/channel pair for eventHandlers.onSubscribed.
// Tests wait on the returned channel to know the SSE handler has registered
// its subscription with the bus and is ready to receive published events.
// Deterministic and avoids the time.Sleep-based readiness pattern.
func subscribedHook() (chan struct{}, func()) {
	ch := make(chan struct{})
	once := sync.Once{}

	return ch, func() { once.Do(func() { close(ch) }) }
}

// waitForLine polls rec.Body (which is appended to by the SSE handler
// goroutine) until it contains the substring needle, or timeout elapses.
// Returns true on success. Uses bodySnapshot() which takes the recorder's
// mutex, so it is safe under -race against ongoing handler writes.
func waitForLine(rec *flushRecorder, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.bodySnapshot(), needle) {
			return true
		}

		// Short real-wallclock poll — SSE writes happen on the handler
		// goroutine; the fake-clock abstraction cannot drive that. 1 ms is
		// well below any realistic event delivery budget.
		time.Sleep(time.Millisecond)
	}

	return strings.Contains(rec.bodySnapshot(), needle)
}

func TestStreamEvents_ReceivesPublishedEvent(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour // Disable keepalive for this test

	subCh, subHook := subscribedHook()
	eh.onSubscribed = subHook

	// Create request with cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	// Run handler in goroutine
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		eh.streamEvents(rec, req)
	}()

	// Wait deterministically for the handler to register its subscription.
	<-subCh

	// Publish an event
	testEvent := events.Event{
		Type:      events.CardCreated,
		Project:   "alpha",
		CardID:    "ALPHA-001",
		Timestamp: time.Now(),
	}
	bus.Publish(testEvent)

	// Poll the recorder until the event's payload lands in the body.
	require.True(t, waitForLine(rec, "ALPHA-001", 2*time.Second),
		"event payload did not reach the recorder within timeout")

	// Cancel context to stop handler
	cancel()
	wg.Wait()

	// Verify headers
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))

	// Parse the recorded body
	body := rec.Body.String()
	lines := strings.Split(body, "\n")

	// Find the data line
	var dataLine string

	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			dataLine = line

			break
		}
	}

	require.NotEmpty(t, dataLine, "should have received data event")

	jsonData := strings.TrimPrefix(dataLine, "data: ")

	var received events.Event

	err := json.Unmarshal([]byte(jsonData), &received)
	require.NoError(t, err)

	assert.Equal(t, events.CardCreated, received.Type)
	assert.Equal(t, "alpha", received.Project)
	assert.Equal(t, "ALPHA-001", received.CardID)
	assert.Positive(t, rec.flushed, "should have called Flush")
}

func TestStreamEvents_FiltersByProject(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour

	subCh, subHook := subscribedHook()
	eh.onSubscribed = subHook

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events?project=alpha", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		eh.streamEvents(rec, req)
	}()

	<-subCh

	// Publish events for different projects
	bus.Publish(events.Event{
		Type:      events.CardCreated,
		Project:   "beta",
		CardID:    "BETA-001",
		Timestamp: time.Now(),
	})
	bus.Publish(events.Event{
		Type:      events.CardCreated,
		Project:   "alpha",
		CardID:    "ALPHA-001",
		Timestamp: time.Now(),
	})

	// Wait for the alpha event to land in the body.  The beta event is
	// filtered out so it never appears.
	require.True(t, waitForLine(rec, "ALPHA-001", 2*time.Second),
		"alpha event did not reach recorder within timeout")

	cancel()
	wg.Wait()

	// Parse body - should only contain alpha event
	body := rec.Body.String()
	lines := strings.Split(body, "\n")

	var receivedEvents []events.Event

	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")

			var ev events.Event
			if err := json.Unmarshal([]byte(jsonData), &ev); err == nil {
				receivedEvents = append(receivedEvents, ev)
			}
		}
	}

	require.Len(t, receivedEvents, 1, "should only receive one event")
	assert.Equal(t, "alpha", receivedEvents[0].Project)
	assert.Equal(t, "ALPHA-001", receivedEvents[0].CardID)
}

func TestStreamEvents_NoFilterReceivesAll(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour

	subCh, subHook := subscribedHook()
	eh.onSubscribed = subHook

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		eh.streamEvents(rec, req)
	}()

	<-subCh

	// Publish events for different projects
	projects := []string{"alpha", "beta", "gamma"}
	for _, proj := range projects {
		bus.Publish(events.Event{
			Type:      events.CardCreated,
			Project:   proj,
			CardID:    proj + "-001",
			Timestamp: time.Now(),
		})
	}

	// Wait for the last-published event to appear in the body. Bus fan-out
	// is ordered, so seeing gamma-001 implies alpha and beta are already
	// buffered.
	require.True(t, waitForLine(rec, "gamma-001", 2*time.Second),
		"last event did not reach recorder within timeout")

	cancel()
	wg.Wait()

	// Parse body - should contain all events
	body := rec.Body.String()
	lines := strings.Split(body, "\n")

	var receivedProjects []string

	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")

			var ev events.Event
			if err := json.Unmarshal([]byte(jsonData), &ev); err == nil {
				receivedProjects = append(receivedProjects, ev.Project)
			}
		}
	}

	assert.ElementsMatch(t, projects, receivedProjects)
}

func TestStreamEvents_Keepalive(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 50 * time.Millisecond // Short interval for testing

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		eh.streamEvents(rec, req)
	}()

	// Poll for the keepalive line rather than sleeping a fixed duration.
	require.True(t, waitForLine(rec, ": keepalive\n", 2*time.Second),
		"keepalive did not appear in body within timeout")

	cancel()
	wg.Wait()
}

func TestStreamEvents_ClientDisconnect(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour

	subCh, subHook := subscribedHook()
	eh.onSubscribed = subHook

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})

	go func() {
		eh.streamEvents(rec, req)
		close(done)
	}()

	// Wait for handler to subscribe before cancelling.
	<-subCh

	// Cancel context (simulates client disconnect)
	cancel()

	// Wait for handler to return
	select {
	case <-done:
		// Success - handler returned after context cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancellation")
	}
}

// mockNonFlushingWriter doesn't implement http.Flusher.
type mockNonFlushingWriter struct {
	header http.Header
	code   int
	body   []byte
}

func (m *mockNonFlushingWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}

	return m.header
}

func (m *mockNonFlushingWriter) Write(b []byte) (int, error) {
	m.body = append(m.body, b...)

	return len(b), nil
}

func (m *mockNonFlushingWriter) WriteHeader(code int) {
	m.code = code
}

func TestStreamEvents_NoFlusher(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w := &mockNonFlushingWriter{}

	eh.streamEvents(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.code)
	assert.Contains(t, string(w.body), "streaming not supported")
}

// TestStreamEvents_SurvivesWriteTimeout verifies that the SSE handler survives
// past the server's WriteTimeout by clearing the per-connection write deadline
// via http.ResponseController before entering the event loop.
func TestStreamEvents_SurvivesWriteTimeout(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	// Use a keepalive shorter than the write timeout so we can receive data
	// after the original deadline would have expired.
	eh.keepaliveInterval = 50 * time.Millisecond

	// Build a real httptest.Server with a very short WriteTimeout.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(eh.streamEvents))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)

	// Connect to the SSE endpoint over a real TCP connection.
	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), 2*time.Second)
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	// Send a minimal HTTP/1.1 GET request.
	_, err = conn.Write([]byte("GET /api/events HTTP/1.1\r\nHost: localhost\r\nAccept: text/event-stream\r\n\r\n"))
	require.NoError(t, err)

	// Read response lines until we collect at least 2 SSE comment lines
	// (": connected" and at least one ": keepalive"), or until timeout.
	// The second keepalive must arrive after the original 100ms WriteTimeout
	// would have killed the connection, proving the deadline was cleared.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))

	scanner := bufio.NewScanner(conn)

	var sseComments []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ": ") {
			sseComments = append(sseComments, line)
			// Once we have the initial ": connected" plus at least one ": keepalive"
			// after 100ms+ has passed, the test is satisfied.
			if len(sseComments) >= 2 {
				break
			}
		}
	}

	require.GreaterOrEqual(t, len(sseComments), 2,
		"expected at least 2 SSE comment lines (connected + keepalive); connection may have been killed by WriteTimeout")
	assert.Equal(t, ": connected", sseComments[0])
	assert.Equal(t, ": keepalive", sseComments[1])
}

// TestStreamEvents_MetricsTracked verifies that SSEActiveConnections is
// incremented when a client connects and decremented when it disconnects.
func TestStreamEvents_MetricsTracked(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour

	baseline := testutil.ToFloat64(metrics.SSEActiveConnections)

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		eh.streamEvents(rec, req)
	}()

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(metrics.SSEActiveConnections) >= baseline+1
	}, 2*time.Second, 1*time.Millisecond,
		"SSEActiveConnections did not increment on connect")

	cancel()
	wg.Wait()

	after := testutil.ToFloat64(metrics.SSEActiveConnections)
	assert.InDelta(t, baseline, after, 0.01, "SSEActiveConnections should return to baseline after disconnect")
}
