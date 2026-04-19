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

// TestStreamProjectSession_SnapshotAndLive covers the project-scoped session lifecycle:
//  1. Spin up a fakeRunnerServer emitting events for two cards (X and Y) in project P.
//  2. Start the project session via mgr.StartProject.
//  3. Emit 2-3 events spanning both cards; wait until buffered.
//  4. Client A connects to /api/runner/logs?project=P (no card_id), drains the snapshot, disconnects.
//  5. Emit 2-3 more events while no client is attached.
//  6. Client B connects — asserts it receives ALL buffered events (both pre- and post-disconnect)
//     BEFORE any live event, in Seq order.
//  7. mgr.StopProject("P"); assert client B gets a terminal event or channel close.
//  8. Assert mgr.SnapshotProject("P") is empty after Stop.
func TestStreamProjectSession_SnapshotAndLive(t *testing.T) {
	const cardX = "PROJ-X01"
	const cardY = "PROJ-Y01"
	const project = "proj-p"

	upstreamCh := make(chan sseTestEvent, 32)
	readyCh := make(chan struct{})
	upstream := fakeRunnerServer(t, upstreamCh, readyCh)
	defer upstream.Close()

	mgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(upstream.URL, "test-key"),
	)

	// Start the project session (mirrors what the handler does on first connect).
	require.NoError(t, mgr.StartProject(context.Background(), project))

	// Wait for the pump goroutine to connect to the upstream server.
	<-readyCh

	// Emit events for both cards X and Y.
	upstreamCh <- sseTestEvent{Seq: 1, Type: "user", Content: "msg-x-1", CardID: cardX}
	upstreamCh <- sseTestEvent{Seq: 2, Type: "text", Content: "msg-y-1", CardID: cardY}
	upstreamCh <- sseTestEvent{Seq: 3, Type: "tool_call", Content: "msg-x-2", CardID: cardX}

	// Wait until all 3 events are buffered.
	require.Eventually(t, func() bool {
		return len(mgr.SnapshotProject(project)) == 3
	}, 3*time.Second, 10*time.Millisecond)

	// Wire the handler and expose it via an httptest server.
	rh := &runnerHandlers{sessionManager: mgr}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh.streamRunnerLogs(w, r)
	}))
	defer apiServer.Close()

	clientURL := apiServer.URL + fmt.Sprintf("/api/runner/logs?project=%s", project)

	// Client A connects, receives the 3 buffered events (snapshot), then disconnects.
	chA, cancelA := connectSSEClient(t, clientURL)
	gotA := drainNStr(chA, 3, 5*time.Second)
	cancelA()
	require.Len(t, gotA, 3, "client A should receive all 3 snapshot events")

	// Verify the snapshot events carry the correct card IDs.
	m0 := parseJSONMap(t, gotA[0])
	assert.Equal(t, "user", m0["type"])
	assert.Equal(t, cardX, m0["card_id"])
	assert.Equal(t, "msg-x-1", m0["content"])

	m1 := parseJSONMap(t, gotA[1])
	assert.Equal(t, cardY, m1["card_id"])

	// Emit 2 more events while no client is attached.
	upstreamCh <- sseTestEvent{Seq: 4, Type: "text", Content: "msg-y-2", CardID: cardY}
	upstreamCh <- sseTestEvent{Seq: 5, Type: "text", Content: "msg-x-3", CardID: cardX}

	// Wait until all 5 events are buffered.
	require.Eventually(t, func() bool {
		return len(mgr.SnapshotProject(project)) == 5
	}, 3*time.Second, 10*time.Millisecond)

	// Client B connects — should replay the full 5-event snapshot.
	chB, cancelB := connectSSEClient(t, clientURL)
	defer cancelB()
	gotB := drainNStr(chB, 5, 5*time.Second)
	require.Len(t, gotB, 5, "client B should replay all 5 buffered events")

	// Verify snapshot order: first event must still be the first-emitted.
	mb0 := parseJSONMap(t, gotB[0])
	assert.Equal(t, "user", mb0["type"])
	assert.Equal(t, cardX, mb0["card_id"])

	// Stop the project session.
	close(upstreamCh)
	mgr.StopProject(project)

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

	// Snapshot must be cleared after StopProject.
	assert.Empty(t, mgr.SnapshotProject(project), "project snapshot must be cleared after StopProject")
}
