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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
)

// flushRecorder is an httptest.ResponseRecorder that supports Flush().
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (f *flushRecorder) Flush() {
	f.flushed++
}

func TestStreamEvents_ReceivesPublishedEvent(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour // Disable keepalive for this test

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

	// Give handler time to start and subscribe
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	testEvent := events.Event{
		Type:      events.CardCreated,
		Project:   "alpha",
		CardID:    "ALPHA-001",
		Timestamp: time.Now(),
	}
	bus.Publish(testEvent)

	// Give event time to be written
	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)
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

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)
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

	// Wait for keepalive
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()

	body := rec.Body.String()
	assert.Contains(t, body, ": keepalive\n")
}

func TestStreamEvents_ClientDisconnect(t *testing.T) {
	bus := events.NewBus()
	eh := newEventHandlers(bus)
	eh.keepaliveInterval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})

	go func() {
		eh.streamEvents(rec, req)
		close(done)
	}()

	// Give handler time to start
	time.Sleep(50 * time.Millisecond)

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
