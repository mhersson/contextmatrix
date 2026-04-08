package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/runner"
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

// TestStreamRunnerLogs_ForwardsEvents verifies that SSE data lines from the
// upstream runner are forwarded verbatim to the browser client.
func TestStreamRunnerLogs_ForwardsEvents(t *testing.T) {
	const apiKey = "test-api-key-for-runner-logs-unit-tests-xyz"

	// Build a mock runner SSE server that sends two data events then closes.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify HMAC auth headers are present.
		assert.NotEmpty(t, r.Header.Get("X-Signature-256"), "X-Signature-256 missing")
		assert.NotEmpty(t, r.Header.Get("X-Webhook-Timestamp"), "X-Webhook-Timestamp missing")

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"type\":\"log\",\"content\":\"hello\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"log\",\"content\":\"world\"}\n\n")
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, apiKey)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "data: {\"type\":\"log\",\"content\":\"hello\"}")
	assert.Contains(t, body, "data: {\"type\":\"log\",\"content\":\"world\"}")
	// Each forwarded line must be followed by double newline.
	assert.Contains(t, body, "\n\n")
}

// TestStreamRunnerLogs_VerifiesHMACHeaders checks that the upstream request
// carries valid HMAC signature and timestamp headers.
func TestStreamRunnerLogs_VerifiesHMACHeaders(t *testing.T) {
	const apiKey = "test-api-key-for-runner-logs-unit-tests-xyz"

	var (
		capturedSig string
		capturedTS  string
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSig = r.Header.Get("X-Signature-256")
		capturedTS = r.Header.Get("X-Webhook-Timestamp")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, apiKey)

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	require.NotEmpty(t, capturedSig, "X-Signature-256 must be set on upstream request")
	require.NotEmpty(t, capturedTS, "X-Webhook-Timestamp must be set on upstream request")
	assert.True(t, strings.HasPrefix(capturedSig, "sha256="), "signature must start with sha256=")

	// Verify the signature is cryptographically valid.
	sig := strings.TrimPrefix(capturedSig, "sha256=")
	assert.True(t,
		runner.VerifySignatureWithTimestamp(apiKey, sig, capturedTS, []byte{}, runner.DefaultMaxClockSkew),
		"HMAC signature must verify successfully",
	)
}

// TestStreamRunnerLogs_ProjectQueryParam verifies the project param is forwarded.
func TestStreamRunnerLogs_ProjectQueryParam(t *testing.T) {
	const apiKey = "test-api-key-for-runner-logs-unit-tests-xyz"

	var capturedURL string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, apiKey)

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs?project=myproject", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	assert.Contains(t, capturedURL, "project=myproject", "project param must be forwarded to upstream")
}

// TestStreamRunnerLogs_RunnerUnreachable checks that an SSE error event is
// written when the upstream runner is not available.
func TestStreamRunnerLogs_RunnerUnreachable(t *testing.T) {
	// Point at a port that is not listening.
	h := makeRunnerHandlers("http://127.0.0.1:1", "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "runner unavailable", "must emit error event when runner unreachable")
}

// TestStreamRunnerLogs_UpstreamNon200 checks that a non-200 upstream response
// causes an SSE error event.
func TestStreamRunnerLogs_UpstreamNon200(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "runner unavailable")
}

// TestStreamRunnerLogs_UpstreamCloses checks that when the upstream closes the
// stream an error event is written.
func TestStreamRunnerLogs_UpstreamCloses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"type\":\"log\",\"content\":\"first\"}\n\n")
		flusher.Flush()
		// Close without sending a terminal event — scanner will hit EOF.
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "runner connection lost")
}

// TestStreamRunnerLogs_BrowserDisconnect verifies that cancelling the browser
// context causes the handler to return promptly.
func TestStreamRunnerLogs_BrowserDisconnect(t *testing.T) {
	// Upstream that streams indefinitely until the request context is done.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		// Stream keepalives until the client goes away.
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, "test-api-key-for-runner-logs-unit-tests-xyz")

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.streamRunnerLogs(rec, req)
	}()

	// Let the handler start and receive at least one keepalive.
	time.Sleep(100 * time.Millisecond)

	// Simulate browser disconnect.
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// Good — handler returned after browser disconnect.
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after browser context cancellation")
	}
}

// TestStreamRunnerLogs_SSEHeaders verifies that the correct SSE headers are set.
func TestStreamRunnerLogs_SSEHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
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

// TestStreamRunnerLogs_ForwardsKeepaliveComments ensures that upstream SSE
// comment lines (keepalives) are forwarded alongside data lines.
func TestStreamRunnerLogs_ForwardsKeepaliveComments(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, ": keepalive\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"log\",\"content\":\"msg\"}\n\n")
	}))
	defer upstream.Close()

	h := makeRunnerHandlers(upstream.URL, "test-api-key-for-runner-logs-unit-tests-xyz")

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs", nil)
	rec := newFlushRecorder()

	h.streamRunnerLogs(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, ": keepalive")
	assert.Contains(t, body, "data: {\"type\":\"log\",\"content\":\"msg\"}")
}
