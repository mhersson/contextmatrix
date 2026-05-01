package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

type runnerEventHandlers struct {
	buf    *events.RunnerEventBuffer
	apiKey string
}

func newRunnerEventHandlers(buf *events.RunnerEventBuffer, apiKey string) *runnerEventHandlers {
	return &runnerEventHandlers{buf: buf, apiKey: apiKey}
}

// authorizeBearer verifies the Authorization: Bearer header against the
// configured key (constant-time compare). Returns true if the request is
// allowed to proceed; on failure it has already written a 401 response and
// the caller must return without further writes. Empty apiKey disables auth.
//
// Used by both /api/runner/events (runnerEventHandlers) and /api/runner/logs
// (runnerHandlers) — both stream sensitive container output and must share
// the same Bearer gate.
func authorizeBearer(w http.ResponseWriter, r *http.Request, apiKey string) bool {
	if apiKey == "" {
		return true
	}

	const prefix = "Bearer "

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		writeRunnerEventsAuthFailure(w)

		return false
	}

	token := strings.TrimPrefix(auth, prefix)
	if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
		writeRunnerEventsAuthFailure(w)

		return false
	}

	return true
}

func (h *runnerEventHandlers) authorize(w http.ResponseWriter, r *http.Request) bool {
	return authorizeBearer(w, r, h.apiKey)
}

func writeRunnerEventsAuthFailure(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="contextmatrix"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// handleStream serves runner events for a single card. With ?since=N
// it returns the buffered events past N as JSON (polling fallback).
// Without ?since=, it serves an SSE stream that first replays from
// Last-Event-ID, then live-fans-out subsequent events. Sends a keepalive
// comment every 30s to survive proxies.
func (h *runnerEventHandlers) handleStream(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	cardID := r.URL.Query().Get("card_id")
	if cardID == "" {
		http.Error(w, "card_id required", http.StatusBadRequest)

		return
	}

	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		since, err := strconv.ParseUint(sinceStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid since", http.StatusBadRequest)

			return
		}

		evs := h.buf.Since(cardID, since)
		if evs == nil {
			evs = []events.RunnerEvent{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(evs)

		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)

		return
	}

	// Clear per-connection write deadline so long-lived SSE survives the server's WriteTimeout.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		ctxlog.Logger(r.Context()).Debug("SSE could not clear write deadline", "error", err)
	}

	// Send initial keepalive to fire client onopen immediately.
	flusher.Flush()

	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		ctxlog.Logger(r.Context()).Debug("SSE initial write failed", "error", err)

		return
	}

	flusher.Flush()

	metrics.SSEActiveConnections.Inc()
	defer metrics.SSEActiveConnections.Dec()

	ctxlog.Logger(r.Context()).Info("runner SSE client connected",
		"card_id", cardID,
		"remote_addr", r.RemoteAddr,
	)

	var lastID uint64

	if header := r.Header.Get("Last-Event-ID"); header != "" {
		if id, err := strconv.ParseUint(header, 10, 64); err == nil {
			lastID = id
		}
	}

	// Subscribe BEFORE replay so we don't miss events appended
	// between replay end and subscribe start.
	ch, cancel := h.buf.Subscribe(cardID)
	defer cancel()

	// Replay from buffer.
	for _, ev := range h.buf.Since(cardID, lastID) {
		if err := writeRunnerSSEEvent(w, ev); err != nil {
			return
		}

		lastID = ev.EventID
	}

	flusher.Flush()

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			ctxlog.Logger(r.Context()).Info("runner SSE client disconnected",
				"card_id", cardID,
				"remote_addr", r.RemoteAddr,
			)

			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// Skip duplicates we already replayed.
			if ev.EventID <= lastID {
				continue
			}

			if err := writeRunnerSSEEvent(w, ev); err != nil {
				return
			}

			lastID = ev.EventID

			flusher.Flush()
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}

			flusher.Flush()
		}
	}
}

// writeRunnerSSEEvent formats one RunnerEvent in SSE wire format. The
// `event:` line carries the type and `id:` carries the monotonic
// event_id; the `data:` payload is just the inner Data string so
// consumers (the runner driver, the chat-loop sessions) can use it
// directly without parsing a JSON wrapper.
func writeRunnerSSEEvent(w io.Writer, ev events.RunnerEvent) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.EventID, ev.Type, ev.Data)

	return err
}
