package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
)

// streamRunnerLogs handles GET /api/runner/logs
//
// Two code paths, selected by query parameters:
//
//   - card_id present → card-scoped path: subscribe to the session manager,
//     replay snapshot, tail live events, handle disconnect.
//   - only project present → project-scoped path: subscribe to the project
//     session manager, replay snapshot, tail live events, handle disconnect.
func (h *runnerHandlers) streamRunnerLogs(w http.ResponseWriter, r *http.Request) {
	// Assert Flusher — required for SSE.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "streaming not supported", "")

		return
	}

	cardID := r.URL.Query().Get("card_id")
	project := r.URL.Query().Get("project")

	// Clear write deadline — SSE connections are long-lived and must survive
	// past the server's WriteTimeout (see events.go for the full explanation).
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("runner SSE could not clear write deadline", "error", err)
	}

	if cardID != "" {
		h.streamCardSession(w, r, flusher, cardID, project)

		return
	}

	h.streamProjectSession(w, r, flusher, project)
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

// streamProjectSession is the project-scoped SSE path.  It subscribes to the
// session manager for the project, replays the snapshot, then tails the live
// event channel until the session terminates or the client disconnects.
func (h *runnerHandlers) streamProjectSession(w http.ResponseWriter, r *http.Request, flusher http.Flusher, project string) {
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

	// StartProject is idempotent — safe to call on every connection.
	if err := h.sessionManager.StartProject(r.Context(), project); err != nil {
		slog.Error("runner SSE: failed to start project session", "project", project, "error", err)

		_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"content\":\"session unavailable\"}\n\n")

		flusher.Flush()

		return
	}

	ch, unsub := h.sessionManager.SubscribeProject(project)
	defer unsub()

	slog.Info("runner SSE project session connected",
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
				"card_id": evt.CardID,
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
			slog.Info("runner SSE project session: client disconnected",
				"project", project,
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
