package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
)

// sseHTTPClient is a dedicated client for SSE upstream proxying.
// Timeout must be zero so that long-lived streaming connections are not
// terminated by a per-request deadline.
var sseHTTPClient = &http.Client{Timeout: 0}

// streamRunnerLogs handles GET /api/runner/logs
//
// Two code paths, selected by query parameters:
//
//   - card_id present → card-scoped path: subscribe to the session manager,
//     replay snapshot, tail live events, handle disconnect.
//   - only project present → legacy proxy path: forward the raw SSE stream
//     from the runner unchanged (used by the Runner Console / ProjectShell).
func (h *runnerHandlers) streamRunnerLogs(w http.ResponseWriter, r *http.Request) {
	// 1. Assert Flusher — required for SSE.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "streaming not supported", "")
		return
	}

	cardID := r.URL.Query().Get("card_id")
	project := r.URL.Query().Get("project")

	// 3. Clear write deadline — SSE connections are long-lived and must survive
	// past the server's WriteTimeout (see events.go for the full explanation).
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("runner SSE could not clear write deadline", "error", err)
	}

	if cardID != "" {
		h.streamCardSession(w, r, flusher, cardID, project)
		return
	}

	h.streamProjectProxy(w, r, flusher, project)
}

// streamCardSession is the card-scoped SSE path.  It subscribes to the session
// manager, replays the snapshot, then tails the live event channel until the
// session terminates or the client disconnects.
func (h *runnerHandlers) streamCardSession(w http.ResponseWriter, r *http.Request, flusher http.Flusher, cardID, project string) {
	if h.sessionManager == nil {
		// Session manager not configured — return 204 so the frontend can retry.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Set SSE headers before subscribing so that the first flush can go out.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch, unsub := h.sessionManager.Subscribe(cardID)
	defer unsub()

	slog.Info("runner SSE session connected",
		"card_id", cardID,
		"project", project,
		"remote_addr", r.RemoteAddr,
	)

	// writeEvent formats a sessionlog.Event as an SSE data frame and flushes.
	writeEvent := func(evt sessionlog.Event) bool {
		var payload map[string]any
		switch evt.Type {
		case sessionlog.EventTypeTerminal:
			payload = map[string]any{"type": "terminal"}
		case sessionlog.EventTypeDropped:
			payload = map[string]any{"type": "dropped"}
		default:
			payload = map[string]any{
				"type":    evt.Type,
				"content": string(evt.Payload),
				"card_id": cardID,
				"ts":      evt.Timestamp.UTC().Format(time.RFC3339Nano),
			}
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for {
		select {
		case <-r.Context().Done():
			slog.Info("runner SSE session: client disconnected",
				"card_id", cardID,
				"remote_addr", r.RemoteAddr,
			)
			return
		case evt, open := <-ch:
			if !open {
				// Channel closed by manager (stop or unsubscribe).
				return
			}
			if !writeEvent(evt) {
				return
			}
			if evt.Type == sessionlog.EventTypeTerminal {
				return
			}
		}
	}
}

// streamProjectProxy is the legacy project-scoped proxy path used by the
// Runner Console (ProjectShell / useRunnerLogs without a cardId).  It forwards
// the raw SSE stream from the runner unchanged.
func (h *runnerHandlers) streamProjectProxy(w http.ResponseWriter, r *http.Request, flusher http.Flusher, project string) {
	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// writeSSEError writes a terminal SSE error event and flushes.
	writeSSEError := func(msg string) {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"content\":%q}\n\n", msg)
		flusher.Flush()
	}

	// Build upstream URL.
	upstreamURL := h.runnerCfg.URL + "/logs"
	if project != "" {
		upstreamURL += "?project=" + url.QueryEscape(project)
	}

	// Build HMAC-signed GET request.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		slog.Error("runner logs: failed to create upstream request", "error", err)
		writeSSEError("runner unavailable")
		return
	}
	sigHeader, tsHeader := runner.SignRequestHeaders(h.runnerCfg.APIKey, []byte{})
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	// Use dedicated zero-timeout client; issue request using browser context.
	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		slog.Error("runner logs: upstream request failed", "error", err)
		writeSSEError("runner unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Non-200 upstream → error event and close.
	if resp.StatusCode != http.StatusOK {
		slog.Warn("runner logs: upstream returned non-200", "status", resp.StatusCode)
		writeSSEError("runner unavailable")
		return
	}

	slog.Info("runner SSE proxy connected",
		"project", project,
		"remote_addr", r.RemoteAddr,
	)

	// Read upstream body line-by-line with a 1MB scanner buffer.
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)

	// Forward SSE data and keepalive comment lines; flush after each.
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") || strings.HasPrefix(line, ":") {
			if _, err := fmt.Fprintf(w, "%s\n\n", line); err != nil {
				slog.Debug("runner SSE: browser write failed", "error", err)
				return
			}
			flusher.Flush()
		}

		// Check browser disconnect between lines.
		select {
		case <-r.Context().Done():
			slog.Info("runner SSE proxy: browser disconnected",
				"project", project,
				"remote_addr", r.RemoteAddr,
			)
			return
		default:
		}
	}

	// Scanner stopped — upstream closed or errored.
	if err := scanner.Err(); err != nil {
		// Context cancellation from browser disconnect causes the body read to
		// return an error. That is handled above by the select on r.Context().Done()
		// between scan iterations; if we reach here after ctx is done, it is benign.
		if r.Context().Err() != nil {
			slog.Info("runner SSE proxy: browser disconnected",
				"project", project,
				"remote_addr", r.RemoteAddr,
			)
			return
		}
		slog.Error("runner logs: upstream scanner error", "error", err)
	}
	writeSSEError("runner connection lost")
}
