package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
)

// eventHandlers handles SSE streaming endpoints.
type eventHandlers struct {
	bus               *events.Bus
	keepaliveInterval time.Duration
}

// newEventHandlers creates event handlers with default keepalive interval.
func newEventHandlers(bus *events.Bus) *eventHandlers {
	return &eventHandlers{
		bus:               bus,
		keepaliveInterval: 30 * time.Second,
	}
}

// streamEvents handles GET /api/events?project= for SSE streaming.
func (h *eventHandlers) streamEvents(w http.ResponseWriter, r *http.Request) {
	// Extract optional project filter from query params
	projectFilter := r.URL.Query().Get("project")

	// Assert Flusher interface (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "streaming not supported", "")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe to event bus
	ch, unsubscribe := h.bus.Subscribe()
	defer unsubscribe()

	// Start keepalive ticker
	ticker := time.NewTicker(h.keepaliveInterval)
	defer ticker.Stop()

	slog.Info("SSE client connected",
		"project_filter", projectFilter,
		"remote_addr", r.RemoteAddr,
	)

	// Stream events until client disconnects
	for {
		select {
		case <-r.Context().Done():
			slog.Info("SSE client disconnected",
				"project_filter", projectFilter,
				"remote_addr", r.RemoteAddr,
			)
			return

		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				slog.Debug("SSE keepalive write failed", "error", err)
				return
			}
			flusher.Flush()

		case event, ok := <-ch:
			if !ok {
				// Channel closed
				return
			}

			// Filter by project if specified
			if projectFilter != "" && event.Project != projectFilter {
				continue
			}

			if err := writeSSEEvent(w, event); err != nil {
				slog.Debug("SSE event write failed", "error", err)
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a single SSE event in data: {json}\n\n format.
func writeSSEEvent(w io.Writer, event events.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
