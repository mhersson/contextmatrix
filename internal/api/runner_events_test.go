package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
)

// TestRunnerEventsSSE_RequiresBearer enforces the auth contract: without
// a matching Authorization: Bearer header the SSE handler must 401 rather
// than fan out card chat events. The runner already sends this header;
// the previous handler ignored it and let any caller with tunnel access
// subscribe to a card's chat.
func TestRunnerEventsSSE_RequiresBearer(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)

	const apiKey = "test-secret-key"

	h := newRunnerEventHandlers(buf, apiKey)

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	cases := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
		{"wrong token", "Bearer not-the-key", http.StatusUnauthorized},
		{"correct", "Bearer test-secret-key", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", srv.URL+"?card_id=c1", nil)
			require.NoError(t, err)

			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			require.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantStatus == http.StatusUnauthorized {
				require.Equal(t,
					`Bearer realm="contextmatrix"`,
					resp.Header.Get("WWW-Authenticate"),
				)
			}
		})
	}
}

func TestRunnerEventsSSEStreamsAppends(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL+"?card_id=c1", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	go func() {
		time.Sleep(50 * time.Millisecond)
		buf.Append("c1", events.RunnerEvent{Type: "chat_input", Data: "hi"})
	}()

	sc := bufio.NewScanner(resp.Body)

	var (
		sawEventType bool
		sawData      bool
	)

	deadline := time.Now().Add(2 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		if strings.HasPrefix(line, "event: chat_input") {
			sawEventType = true
		}

		if strings.HasPrefix(line, "data: ") {
			sawData = true

			require.Contains(t, line, "hi", "data line carries the inner Data field directly")

			break
		}
	}

	require.True(t, sawEventType, "expected event: line to carry the chat_input type")
	require.True(t, sawData, "expected at least one data: frame")
}

func TestRunnerEventsSSEReplaysLastEventID(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	buf.Append("c1", events.RunnerEvent{Type: "a"})
	buf.Append("c1", events.RunnerEvent{Type: "b"})
	buf.Append("c1", events.RunnerEvent{Type: "c"})

	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"?card_id=c1", nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)

	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	body := strings.Join(lines, "\n")
	require.NotContains(t, body, "event: a")
	require.Contains(t, body, "event: b")
	require.Contains(t, body, "event: c")
}

func TestRunnerEventsSSERequiresCardID(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRunnerEventsPollFallback(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	buf.Append("c1", events.RunnerEvent{Type: "a"})
	buf.Append("c1", events.RunnerEvent{Type: "b"})

	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?card_id=c1&since=1")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"type":"b"`)
	require.NotContains(t, string(body), `"type":"a"`)
}

func TestRunnerEventsPollInvalidSince(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?card_id=c1&since=abc")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestRunnerEventsSSEPreservesNewlinesInData locks in the SSE contract for
// multi-line chat content. The producer must split Data on \n and emit
// one data: line per fragment per the SSE spec; the consumer rejoins with \n.
// Without this, user-typed multi-line chat is silently truncated and may
// corrupt subsequent events on the wire.
func TestRunnerEventsSSEPreservesNewlinesInData(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf, "")

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"?card_id=c1", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	const multiline = "line one\nline two\n\nline four after blank"

	go func() {
		time.Sleep(50 * time.Millisecond)
		buf.Append("c1", events.RunnerEvent{Type: "chat_input", Data: multiline})
	}()

	sc := bufio.NewScanner(resp.Body)

	var (
		sawType   bool
		dataLines []string
		eventDone bool
	)

	deadline := time.Now().Add(2 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) && !eventDone {
		line := sc.Text()

		switch {
		case strings.HasPrefix(line, "event: chat_input"):
			sawType = true
		case sawType && strings.HasPrefix(line, "data: "):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		case sawType && line == "" && len(dataLines) > 0:
			eventDone = true

			cancel()
		}
	}

	require.True(t, sawType, "expected event: chat_input frame")
	require.True(t, eventDone, "expected event terminator")
	require.Equal(t, multiline, strings.Join(dataLines, "\n"),
		"all newline-separated content must round-trip across the SSE boundary")
}
