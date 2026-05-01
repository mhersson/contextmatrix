package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
)

// defaultRunnerKeepaliveInterval is the keepalive period for runner SSE streams.
const defaultRunnerKeepaliveInterval = 30 * time.Second

// streamRunnerLogs handles GET /api/runner/logs
//
// Auth posture: this is a browser-bound SSE endpoint consumed by
// web/src/hooks/useRunnerLogs.ts. EventSource cannot attach an Authorization
// header, so Bearer auth would break the only legitimate consumer; instead,
// the deployment relies on the upstream zero-trust front door (Cloudflare
// Tunnel / Access) for identity. Container transcripts may include secrets,
// so the front door is load-bearing — operators MUST NOT expose the main
// HTTP port directly to the public internet without an equivalent gate. The
// sibling /api/runner/events endpoint, which is server-to-runner only and
// not browser-reachable, IS Bearer-gated (commit 63dfea6).
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

	metrics.SSEActiveConnections.Inc()
	defer metrics.SSEActiveConnections.Dec()

	// Clear write deadline — SSE connections are long-lived and must survive
	// past the server's WriteTimeout (see events.go for the full explanation).
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		ctxlog.Logger(r.Context()).Debug("runner SSE could not clear write deadline", "error", err)
	}

	if cardID != "" {
		h.streamCardSession(w, r, flusher, cardID, project)

		return
	}

	h.streamProjectSession(w, r, flusher, project)
}

// runnerKeepaliveInterval returns the configured keepalive interval, falling
// back to the default when the field is zero.
func (h *runnerHandlers) runnerKeepaliveInterval() time.Duration {
	if h.keepaliveInterval > 0 {
		return h.keepaliveInterval
	}

	return defaultRunnerKeepaliveInterval
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
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	ch, unsub := h.sessionManager.Subscribe(cardID)
	defer unsub()

	ctxlog.Logger(r.Context()).Info("runner SSE session connected",
		"card_id", cardID,
		"project", project,
		"remote_addr", r.RemoteAddr,
	)

	// writeEvent formats a sessionlog.Event as an SSE data frame and flushes.
	writeEvent := func(evt sessionlog.Event) bool {
		var payload map[string]any

		switch evt.Type {
		case sessionlog.EventTypeTerminal:
			payload = map[string]any{"type": "terminal", "seq": evt.Seq}
		case sessionlog.EventTypeDropped:
			payload = map[string]any{
				"type":  "dropped",
				"seq":   evt.Seq,
				"count": sessionlog.DroppedMarkerCount(evt),
			}
		default:
			payload = map[string]any{
				"type":    evt.Type,
				"content": string(evt.Payload),
				"card_id": cardID,
				"ts":      evt.Timestamp.UTC().Format(time.RFC3339Nano),
				"seq":     evt.Seq,
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

	ticker := time.NewTicker(h.runnerKeepaliveInterval())
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			ctxlog.Logger(r.Context()).Info("runner SSE session: client disconnected",
				"card_id", cardID,
				"remote_addr", r.RemoteAddr,
			)

			return

		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				ctxlog.Logger(r.Context()).Debug("runner SSE card keepalive write failed", "error", err)

				return
			}

			flusher.Flush()

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
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// StartProject is idempotent — safe to call on every connection.
	if err := h.sessionManager.StartProject(r.Context(), project); err != nil {
		ctxlog.Logger(r.Context()).Error("runner SSE: failed to start project session", "project", project, "error", err)

		_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"content\":\"session unavailable\"}\n\n")

		flusher.Flush()

		return
	}

	ch, unsub := h.sessionManager.SubscribeProject(project)
	defer unsub()

	ctxlog.Logger(r.Context()).Info("runner SSE project session connected",
		"project", project,
		"remote_addr", r.RemoteAddr,
	)

	// writeEvent formats a sessionlog.Event as an SSE data frame and flushes.
	writeEvent := func(evt sessionlog.Event) bool {
		var payload map[string]any

		switch evt.Type {
		case sessionlog.EventTypeTerminal:
			payload = map[string]any{"type": "terminal", "seq": evt.Seq}
		case sessionlog.EventTypeDropped:
			payload = map[string]any{
				"type":  "dropped",
				"seq":   evt.Seq,
				"count": sessionlog.DroppedMarkerCount(evt),
			}
		default:
			payload = map[string]any{
				"type":    evt.Type,
				"content": string(evt.Payload),
				"card_id": evt.CardID,
				"ts":      evt.Timestamp.UTC().Format(time.RFC3339Nano),
				"seq":     evt.Seq,
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

	ticker := time.NewTicker(h.runnerKeepaliveInterval())
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			ctxlog.Logger(r.Context()).Info("runner SSE project session: client disconnected",
				"project", project,
				"remote_addr", r.RemoteAddr,
			)

			return

		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				ctxlog.Logger(r.Context()).Debug("runner SSE project keepalive write failed", "error", err)

				return
			}

			flusher.Flush()

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
