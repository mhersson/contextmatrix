package api

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
)

// sseTestEvent is a helper carrying the fields the fake runner server emits.
type sseTestEvent struct {
	Seq     uint64
	Type    string
	Content string
	CardID  string // used for cross-card filter tests
}

// fakeRunnerServer creates a fake runner SSE server that streams events from
// the provided channel until it is closed, then holds the connection open until
// the client disconnects.  readyCh is closed once the HTTP headers are sent.
func fakeRunnerServer(t *testing.T, eventCh <-chan sseTestEvent, readyCh chan struct{}) *httptest.Server {
	t.Helper()
	var once sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		once.Do(func() { close(readyCh) })

		for evt := range eventCh {
			payload := map[string]any{
				"seq":     evt.Seq,
				"type":    evt.Type,
				"content": evt.Content,
				"card_id": evt.CardID,
			}
			b, err := json.Marshal(payload)
			if err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		}
		// Channel closed — hold the connection until the client disconnects.
		<-r.Context().Done()
	}))
}

// connectSSEClient opens an SSE GET connection to url and returns a channel of
// raw JSON strings from each "data:" line, plus a cancel function to disconnect.
func connectSSEClient(t *testing.T, url string) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if raw != "" {
					select {
					case ch <- raw:
					default:
					}
				}
			}
		}
	}()
	return ch, cancel
}

// drainNStr reads at most n strings from ch within timeout.
func drainNStr(ch <-chan string, n int, timeout time.Duration) []string {
	deadline := time.After(timeout)
	var out []string
	for len(out) < n {
		select {
		case s, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, s)
		case <-deadline:
			return out
		}
	}
	return out
}

// parseJSONMap unmarshals a raw JSON string into a map for field assertions.
func parseJSONMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	return m
}

// --- Tests ---

// TestStreamCardSession_SnapshotAndLive covers the core lifecycle:
//  1. Start a session via manager, emit a question event for card X.
//  2. Client A connects, receives the question, disconnects.
//  3. Emit more events for card X while no client is attached.
//  4. Client B connects — asserts it receives ALL buffered events (snapshot replay).
//  5. Stop the session. Client B receives a terminal event and the channel closes.
//  6. After Stop, Snapshot is empty.
func TestStreamCardSession_SnapshotAndLive(t *testing.T) {
	const cardID = "SESS-001"
	const project = "alpha"

	upstreamCh := make(chan sseTestEvent, 32)
	readyCh := make(chan struct{})
	upstream := fakeRunnerServer(t, upstreamCh, readyCh)
	defer upstream.Close()

	mgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(upstream.URL, "test-key"),
	)

	// Start the session (mirrors what UpdateRunnerStatus does on → running).
	require.NoError(t, mgr.Start(context.Background(), cardID, project))

	// Wait for the pump goroutine to connect to the upstream server.
	<-readyCh

	// Emit one question event for card X.
	upstreamCh <- sseTestEvent{Seq: 1, Type: "user", Content: "question from agent", CardID: cardID}

	// Wait until the buffer holds the event.
	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardID)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	// Wire the handler and expose it via an httptest server.
	rh := &runnerHandlers{sessionManager: mgr}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	defer apiServer.Close()

	clientURL := apiServer.URL + fmt.Sprintf("/api/runner/logs?card_id=%s&project=%s", cardID, project)

	// Client A connects, receives the 1 buffered event (snapshot), then disconnects.
	chA, cancelA := connectSSEClient(t, clientURL)
	gotA := drainNStr(chA, 1, 5*time.Second)
	cancelA()
	require.Len(t, gotA, 1, "client A should receive the question event")
	m := parseJSONMap(t, gotA[0])
	assert.Equal(t, "user", m["type"])
	assert.Equal(t, "question from agent", m["content"])

	// Emit 2 more events while no client is attached.
	upstreamCh <- sseTestEvent{Seq: 2, Type: "text", Content: "thinking…", CardID: cardID}
	upstreamCh <- sseTestEvent{Seq: 3, Type: "tool_call", Content: "tool X", CardID: cardID}

	// Wait until all 3 events are buffered.
	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardID)) == 3
	}, 3*time.Second, 10*time.Millisecond)

	// Client B connects — should replay the full 3-event snapshot.
	chB, cancelB := connectSSEClient(t, clientURL)
	defer cancelB()
	gotB := drainNStr(chB, 3, 5*time.Second)
	require.Len(t, gotB, 3, "client B should replay all 3 buffered events")

	// Verify snapshot order: first event must still be the question.
	m0 := parseJSONMap(t, gotB[0])
	assert.Equal(t, "user", m0["type"])
	assert.Equal(t, "question from agent", m0["content"])

	// Stop the session (mirrors UpdateRunnerStatus → terminal).
	close(upstreamCh)
	mgr.Stop(cardID)

	// Client B should receive a terminal signal (terminal event or channel close).
	select {
	case raw, ok := <-chB:
		if ok {
			m := parseJSONMap(t, raw)
			assert.Equal(t, "terminal", m["type"], "expected terminal event type")
		}
		// ok==false means the channel was closed, also acceptable.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal event on client B")
	}

	// Snapshot must be cleared after Stop.
	assert.Empty(t, mgr.Snapshot(cardID), "snapshot must be cleared after Stop")
}

// TestStreamCardSession_CrossCardFilter verifies that events for a different
// card ID (Y) are NOT buffered into card X's session.
func TestStreamCardSession_CrossCardFilter(t *testing.T) {
	const cardX = "SESS-X01"
	const cardY = "SESS-Y01"
	const project = "beta"

	upstreamCh := make(chan sseTestEvent, 32)
	readyCh := make(chan struct{})
	upstream := fakeRunnerServer(t, upstreamCh, readyCh)
	defer upstream.Close()

	mgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(upstream.URL, "test-key"),
	)

	require.NoError(t, mgr.Start(context.Background(), cardX, project))
	<-readyCh

	// Emit event for card Y — must NOT appear in card X's buffer.
	upstreamCh <- sseTestEvent{Seq: 1, Type: "text", Content: "for Y only", CardID: cardY}
	// Emit event for card X — must appear.
	upstreamCh <- sseTestEvent{Seq: 2, Type: "text", Content: "for X", CardID: cardX}

	// Wait until card X's buffer holds exactly 1 event (not 2).
	require.Eventually(t, func() bool {
		return len(mgr.Snapshot(cardX)) == 1
	}, 3*time.Second, 10*time.Millisecond)

	snap := mgr.Snapshot(cardX)
	require.Len(t, snap, 1, "only card X's event should be buffered")
	assert.Equal(t, "for X", string(snap[0].Payload))

	// Stop the manager session before closing the server so the pump goroutine
	// exits cleanly and the server's handler can unblock from <-r.Context().Done().
	mgr.Stop(cardX)
	close(upstreamCh)
}

// TestStreamCardSession_NoManager returns 204 when no session manager is wired.
func TestStreamCardSession_NoManager(t *testing.T) {
	rh := &runnerHandlers{sessionManager: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/runner/logs?card_id=X-001&project=p", nil)
	rec := newFlushRecorder()

	rh.streamRunnerLogs(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// TestStreamProjectProxy_LegacyPathUnchanged verifies that the project-only
// (no card_id) path still proxies the runner stream directly and does NOT send
// a card_id query parameter to the upstream.
func TestStreamProjectProxy_LegacyPathUnchanged(t *testing.T) {
	const apiKey = "proxy-test-key"
	var (
		mu           sync.Mutex
		capturedPath string
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedPath = r.URL.String()
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"type\":\"log\",\"card_id\":\"P-001\",\"content\":\"hello\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold until client disconnects.
		<-r.Context().Done()
	}))
	defer upstream.Close()

	// Session manager present, but card_id absent → legacy proxy path.
	mgr := sessionlog.NewManager(sessionlog.WithRunnerConfig(upstream.URL, apiKey))

	rh := &runnerHandlers{
		runnerCfg:      config.RunnerConfig{URL: upstream.URL, APIKey: apiKey},
		sessionManager: mgr,
	}

	// Expose the handler via a real httptest.Server so the client and handler
	// run in separate goroutines without sharing the httptest.ResponseRecorder.
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	defer apiServer.Close()

	ch, cancel := connectSSEClient(t, apiServer.URL+"/api/runner/logs?project=myproject")
	defer cancel()

	got := drainNStr(ch, 1, 5*time.Second)
	require.Len(t, got, 1, "legacy proxy must deliver at least one event")
	assert.Contains(t, got[0], "hello", "legacy proxy must forward upstream events unchanged")

	cancel() // disconnect the browser

	mu.Lock()
	path := capturedPath
	mu.Unlock()
	assert.NotEmpty(t, path)
	assert.NotContains(t, path, "card_id", "legacy proxy must not forward card_id to upstream")
	assert.Contains(t, path, "project=myproject")
}
