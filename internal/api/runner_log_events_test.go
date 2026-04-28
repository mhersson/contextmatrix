package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
)

func TestRunnerLogEventFansOutToBus(t *testing.T) {
	bus := events.NewBus()
	h := newRunnerLogEventHandlers(bus)

	srv := httptest.NewServer(http.HandlerFunc(h.handleEmit))
	defer srv.Close()

	ch, unsub := bus.Subscribe()
	defer unsub()

	body, err := json.Marshal(map[string]any{
		"card_id": "card1",
		"kind":    "tool_call",
		"text":    "Read /workspace/README.md",
	})
	require.NoError(t, err)

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	select {
	case ev := <-ch:
		require.Equal(t, events.RunnerLogEvent, ev.Type)
		require.Equal(t, "card1", ev.CardID)
		require.Equal(t, "tool_call", ev.Data["kind"])
		require.Equal(t, "Read /workspace/README.md", ev.Data["text"])
	case <-time.After(time.Second):
		t.Fatal("event not delivered to bus")
	}
}

func TestRunnerLogEventRejectsMissingCardID(t *testing.T) {
	bus := events.NewBus()
	h := newRunnerLogEventHandlers(bus)

	srv := httptest.NewServer(http.HandlerFunc(h.handleEmit))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{"kind":"x"}`)))
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRunnerLogEventRejectsBadJSON(t *testing.T) {
	bus := events.NewBus()
	h := newRunnerLogEventHandlers(bus)

	srv := httptest.NewServer(http.HandlerFunc(h.handleEmit))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader([]byte(`not json`)))
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRunnerLogEventIncludesExtra(t *testing.T) {
	bus := events.NewBus()
	h := newRunnerLogEventHandlers(bus)

	srv := httptest.NewServer(http.HandlerFunc(h.handleEmit))
	defer srv.Close()

	ch, unsub := bus.Subscribe()
	defer unsub()

	body := []byte(`{"card_id":"c1","kind":"tool_result","extra":{"file":"foo.go","lines":42}}`)
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	select {
	case ev := <-ch:
		require.Equal(t, "c1", ev.CardID)
		require.Equal(t, "tool_result", ev.Data["kind"])
		// Extra is delivered as raw JSON; verify the JSON content survived
		// rather than just asserting non-nil.
		require.NotNil(t, ev.Data["extra"])

		extraRaw, ok := ev.Data["extra"].(json.RawMessage)
		require.True(t, ok, "expected json.RawMessage, got %T", ev.Data["extra"])
		require.JSONEq(t, `{"file":"foo.go","lines":42}`, string(extraRaw))
	case <-time.After(time.Second):
		t.Fatal("event not delivered to bus")
	}
}
