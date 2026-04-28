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

func TestRunnerEventsSSEStreamsAppends(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf)

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

	var sawData bool

	deadline := time.Now().Add(2 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			sawData = true

			require.Contains(t, line, "chat_input")
			require.Contains(t, line, "hi")

			break
		}
	}

	require.True(t, sawData, "expected at least one data: frame")
}

func TestRunnerEventsSSEReplaysLastEventID(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	buf.Append("c1", events.RunnerEvent{Type: "a"})
	buf.Append("c1", events.RunnerEvent{Type: "b"})
	buf.Append("c1", events.RunnerEvent{Type: "c"})

	h := newRunnerEventHandlers(buf)

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
	require.NotContains(t, body, `"type":"a"`)
	require.Contains(t, body, `"type":"b"`)
	require.Contains(t, body, `"type":"c"`)
}

func TestRunnerEventsSSERequiresCardID(t *testing.T) {
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	h := newRunnerEventHandlers(buf)

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

	h := newRunnerEventHandlers(buf)

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
	h := newRunnerEventHandlers(buf)

	srv := httptest.NewServer(http.HandlerFunc(h.handleStream))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?card_id=c1&since=abc")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
