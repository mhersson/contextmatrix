package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	"github.com/mhersson/contextmatrix/internal/config"
)

// makeBackendHandlers returns a backendHandlers wired to the given backend URL and API key.
func makeBackendHandlers(backendURL, apiKey string) *backendHandlers {
	return &backendHandlers{
		backend: backend.NewClient(backendURL, apiKey),
		backendCfg: &config.AgentBackendConfig{
			APIKey: apiKey,
		},
	}
}

// TestStreamWorkerLogs_NoFlusher verifies that a 500 is returned when the
// ResponseWriter does not implement http.Flusher.
func TestStreamWorkerLogs_NoFlusher(t *testing.T) {
	h := makeBackendHandlers("http://127.0.0.1:1", "test-api-key-for-worker-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/worker/logs", nil)
	w := &mockNonFlushingWriter{}

	h.streamWorkerLogs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.code)
	assert.Contains(t, string(w.body), "streaming not supported")
}

// TestStreamProjectSession_NoManager verifies that a 204 is returned for the
// project path when no session manager is configured.
func TestStreamProjectSession_NoManager(t *testing.T) {
	rh := &backendHandlers{sessionManager: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/worker/logs?project=myproject", nil)
	rec := newFlushRecorder()

	rh.streamWorkerLogs(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- X-Accel-Buffering header tests ---

// TestStreamCardSession_XAccelBufferingHeader asserts that X-Accel-Buffering: no
// is set on the card-scoped SSE response.
func TestStreamCardSession_XAccelBufferingHeader(t *testing.T) {
	mgr := sessionlog.NewManager()

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour, // disable keepalive during test
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/worker/logs?card_id=CARD-001&project=p", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	cancel()
}

// TestStreamProjectSession_XAccelBufferingHeader asserts that X-Accel-Buffering: no
// is set on the project-scoped SSE response.
func TestStreamProjectSession_XAccelBufferingHeader(t *testing.T) {
	mgr := sessionlog.NewManager()

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour, // disable keepalive during test
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/worker/logs?project=myproject", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	cancel()
}

// --- Keepalive tests ---

// TestStreamCardSession_Keepalive asserts that a keepalive comment is written
// when no events flow and the tick interval elapses.
func TestStreamCardSession_Keepalive(t *testing.T) {
	mgr := sessionlog.NewManager()

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 50 * time.Millisecond,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/worker/logs?card_id=CARD-002&project=p", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	// Read until we see a keepalive line or timeout.
	found := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "keepalive") {
				close(found)

				return
			}
		}
	}()

	select {
	case <-found:
		// keepalive received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for keepalive frame")
	}

	cancel()
}

// TestStreamProjectSession_Keepalive asserts that a keepalive comment is written
// on the project-scoped path when no events flow.
func TestStreamProjectSession_Keepalive(t *testing.T) {
	mgr := sessionlog.NewManager()

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 50 * time.Millisecond,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/worker/logs?project=proj-keepalive", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	found := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "keepalive") {
				close(found)

				return
			}
		}
	}()

	select {
	case <-found:
		// keepalive received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for keepalive frame on project-scoped handler")
	}

	cancel()
}

// --- Seq payload-shape tests ---

// TestStreamCardSession_WireFramesCarryNoSeq asserts that the JSON payload
// emitted by the card-scoped handler includes a "seq" field and that it is 0
// for wire-sourced live events: the backend's frames carry no seq, and
// nothing assigns a nonzero Seq today.
func TestStreamCardSession_WireFramesCarryNoSeq(t *testing.T) {
	const (
		cardID  = "SEQ-001"
		project = "seqtest"
	)

	upstreamCh := make(chan protocol.LogEntry, 8)
	readyCh := make(chan struct{})

	upstream := fakeBackendServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithBackendConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.Start(context.Background(), cardID, project))
	<-readyCh

	// Emit a true wire frame - no seq field exists on the wire.
	upstreamCh <- protocol.LogEntry{Type: "text", Content: "hello", CardID: cardID}

	// Wait until buffered.
	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardID)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/worker/logs?card_id=" + cardID + "&project=" + project

	dataCh, cancelClient := connectSSEClient(t, clientURL)

	frames := drainNStr(dataCh, 1, 5*time.Second)

	// Stop session and disconnect client before closing servers, so that the
	// upstream fake server's goroutine can exit cleanly.
	cancelClient()
	close(upstreamCh)
	mgr.Stop(cardID)
	srv.Close()
	upstream.Close()

	require.Len(t, frames, 1, "expected one SSE data frame")

	m := parseJSONMap(t, frames[0])
	seqVal, ok := m["seq"]
	require.True(t, ok, "payload must contain 'seq' field")

	// The wire carries no seq; Event.Seq stays 0 for live events.
	// JSON numbers unmarshal as float64 by default.
	assert.EqualValues(t, 0, seqVal, "wire-sourced events must have seq 0")

	// The content must pass through intact.
	assert.Equal(t, "hello", m["content"])
}

// TestStreamProjectSession_WireFramesCarryNoSeq asserts that the JSON payload
// emitted by the project-scoped handler includes a "seq" field and that it is
// 0 for wire-sourced live events - same reality as the card-scoped path: the
// backend's frames carry no seq.
func TestStreamProjectSession_WireFramesCarryNoSeq(t *testing.T) {
	const (
		cardID  = "SEQ-PROJ-001"
		project = "seqprojtest"
	)

	upstreamCh := make(chan protocol.LogEntry, 8)
	readyCh := make(chan struct{})

	upstream := fakeBackendServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithBackendConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.StartProject(context.Background(), project))
	<-readyCh

	upstreamCh <- protocol.LogEntry{Type: "text", Content: "world", CardID: cardID}

	require.Eventually(t, func() bool {
		return len(mgr.SnapshotProject(project)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/worker/logs?project=" + project

	dataCh, cancelClient := connectSSEClient(t, clientURL)

	frames := drainNStr(dataCh, 1, 5*time.Second)

	// Stop session and disconnect client before closing servers.
	cancelClient()
	close(upstreamCh)
	mgr.StopProject(project)
	srv.Close()
	upstream.Close()

	require.Len(t, frames, 1, "expected one SSE data frame")

	m := parseJSONMap(t, frames[0])
	seqVal, ok := m["seq"]
	require.True(t, ok, "payload must contain 'seq' field")

	// The wire carries no seq; Event.Seq stays 0 for live events.
	assert.EqualValues(t, 0, seqVal, "wire-sourced events must have seq 0")

	// The content and card_id must pass through intact.
	assert.Equal(t, "world", m["content"])
	assert.Equal(t, cardID, m["card_id"])
}

// TestStreamCardSession_AgentFieldOnlyWhenSet asserts the card-scoped SSE
// frames carry "agent" for attributed frames and omit the key entirely for
// ordinary frames (no noisy empty field).
func TestStreamCardSession_AgentFieldOnlyWhenSet(t *testing.T) {
	const (
		cardID  = "AGENT-001"
		project = "agenttest"
	)

	upstreamCh := make(chan protocol.LogEntry, 8)
	readyCh := make(chan struct{})

	upstream := fakeBackendServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithBackendConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.Start(context.Background(), cardID, project))
	<-readyCh

	upstreamCh <- protocol.LogEntry{Type: "text", Content: "[round 0] seat-2: hello", CardID: cardID, Agent: "seat-2"}

	upstreamCh <- protocol.LogEntry{Type: "text", Content: "plain", CardID: cardID}

	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardID)) == 2
	}, 3*time.Second, 10*time.Millisecond)

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/worker/logs?card_id=" + cardID + "&project=" + project

	dataCh, cancelClient := connectSSEClient(t, clientURL)

	frames := drainNStr(dataCh, 2, 5*time.Second)

	cancelClient()
	close(upstreamCh)
	mgr.Stop(cardID)
	srv.Close()
	upstream.Close()

	require.Len(t, frames, 2, "expected two SSE data frames")

	withAgent := parseJSONMap(t, frames[0])
	assert.Equal(t, "seat-2", withAgent["agent"])

	plain := parseJSONMap(t, frames[1])
	_, present := plain["agent"]
	assert.False(t, present, "frames without attribution must omit the agent key entirely")
}

// TestStreamProjectSession_AgentFieldOnlyWhenSet is the project-scoped twin -
// the two writeEvent closures are separate code paths and must both carry
// the field.
func TestStreamProjectSession_AgentFieldOnlyWhenSet(t *testing.T) {
	const (
		cardID  = "AGENT-PROJ-001"
		project = "agentprojtest"
	)

	upstreamCh := make(chan protocol.LogEntry, 8)
	readyCh := make(chan struct{})

	upstream := fakeBackendServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithBackendConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.StartProject(context.Background(), project))
	<-readyCh

	upstreamCh <- protocol.LogEntry{Type: "text", Content: "[round 0] guest-laptop: hi", CardID: cardID, Agent: "guest-laptop"}

	upstreamCh <- protocol.LogEntry{Type: "text", Content: "plain", CardID: cardID}

	require.Eventually(t, func() bool {
		return len(mgr.SnapshotProject(project)) == 2
	}, 3*time.Second, 10*time.Millisecond)

	rh := &backendHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamWorkerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/worker/logs?project=" + project

	dataCh, cancelClient := connectSSEClient(t, clientURL)

	frames := drainNStr(dataCh, 2, 5*time.Second)

	cancelClient()
	close(upstreamCh)
	mgr.StopProject(project)
	srv.Close()
	upstream.Close()

	require.Len(t, frames, 2, "expected two SSE data frames")

	withAgent := parseJSONMap(t, frames[0])
	assert.Equal(t, "guest-laptop", withAgent["agent"])

	plain := parseJSONMap(t, frames[1])
	_, present := plain["agent"]
	assert.False(t, present, "frames without attribution must omit the agent key entirely")
}
