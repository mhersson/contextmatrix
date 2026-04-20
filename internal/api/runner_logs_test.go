package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
)

// makeRunnerHandlers returns a runnerHandlers wired to the given runner URL and API key.
func makeRunnerHandlers(runnerURL, apiKey string) *runnerHandlers {
	return &runnerHandlers{
		runner: runner.NewClient(runnerURL, apiKey),
		runnerCfg: config.RunnerConfig{
			URL:    runnerURL,
			APIKey: apiKey,
		},
	}
}

// TestStreamRunnerLogs_NoFlusher verifies that a 500 is returned when the
// ResponseWriter does not implement http.Flusher.
func TestStreamRunnerLogs_NoFlusher(t *testing.T) {
	h := makeRunnerHandlers("http://127.0.0.1:1", "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	w := &mockNonFlushingWriter{}

	h.streamRunnerLogs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.code)
	assert.Contains(t, string(w.body), "streaming not supported")
}

// TestStreamProjectSession_NoManager verifies that a 204 is returned for the
// project path when no session manager is configured.
func TestStreamProjectSession_NoManager(t *testing.T) {
	rh := &runnerHandlers{sessionManager: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs?project=myproject", nil)
	rec := newFlushRecorder()

	rh.streamRunnerLogs(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- X-Accel-Buffering header tests ---

// TestStreamCardSession_XAccelBufferingHeader asserts that X-Accel-Buffering: no
// is set on the card-scoped SSE response.
func TestStreamCardSession_XAccelBufferingHeader(t *testing.T) {
	mgr := sessionlog.NewManager()

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour, // disable keepalive during test
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/runner/logs?card_id=CARD-001&project=p", nil)
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

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour, // disable keepalive during test
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/runner/logs?project=myproject", nil)
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

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 50 * time.Millisecond,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/runner/logs?card_id=CARD-002&project=p", nil)
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

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 50 * time.Millisecond,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/runner/logs?project=proj-keepalive", nil)
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

// --- Seq passthrough tests ---

// TestStreamCardSession_SeqInPayload asserts that the JSON payload emitted by
// the card-scoped handler includes the "seq" field from sessionlog.Event.Seq.
func TestStreamCardSession_SeqInPayload(t *testing.T) {
	const (
		cardID  = "SEQ-001"
		project = "seqtest"
	)

	upstreamCh := make(chan sseTestEvent, 8)
	readyCh := make(chan struct{})

	upstream := fakeRunnerServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.Start(context.Background(), cardID, project))
	<-readyCh

	// Emit an event with a known Seq.
	upstreamCh <- sseTestEvent{Seq: 42, Type: "text", Content: "hello", CardID: cardID}

	// Wait until buffered.
	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardID)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/runner/logs?card_id=" + cardID + "&project=" + project

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

	// JSON numbers unmarshal as float64 by default.
	assert.EqualValues(t, 42, seqVal, "seq must match the emitted event's Seq")
}

// TestStreamProjectSession_SeqInPayload asserts that the JSON payload emitted
// by the project-scoped handler includes the "seq" field.
func TestStreamProjectSession_SeqInPayload(t *testing.T) {
	const (
		cardID  = "SEQ-PROJ-001"
		project = "seqprojtest"
	)

	upstreamCh := make(chan sseTestEvent, 8)
	readyCh := make(chan struct{})

	upstream := fakeRunnerServer(t, upstreamCh, readyCh)

	mgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.StartProject(context.Background(), project))
	<-readyCh

	upstreamCh <- sseTestEvent{Seq: 7, Type: "text", Content: "world", CardID: cardID}

	require.Eventually(t, func() bool {
		return len(mgr.SnapshotProject(project)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	rh := &runnerHandlers{
		sessionManager:    mgr,
		keepaliveInterval: 1 * time.Hour,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))

	clientURL := srv.URL + "/api/runner/logs?project=" + project

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

	assert.EqualValues(t, 7, seqVal, "seq must match the emitted event's Seq")
}
