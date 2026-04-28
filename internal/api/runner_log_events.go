package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
)

type runnerLogEventHandlers struct {
	bus *events.Bus
}

func newRunnerLogEventHandlers(bus *events.Bus) *runnerLogEventHandlers {
	return &runnerLogEventHandlers{bus: bus}
}

type runnerLogEventInput struct {
	CardID string          `json:"card_id"`
	Kind   string          `json:"kind"` // text, thinking, tool_call, tool_result, system
	Text   string          `json:"text,omitempty"`
	Extra  json.RawMessage `json:"extra,omitempty"`
}

// handleEmit accepts a single live console event from the runner and
// fans it out to web-UI SSE subscribers via the existing event Bus.
// Live-only: not persisted to the card's activity log.
func (h *runnerLogEventHandlers) handleEmit(w http.ResponseWriter, r *http.Request) {
	var in runnerLogEventInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)

		return
	}

	if in.CardID == "" {
		http.Error(w, "card_id required", http.StatusBadRequest)

		return
	}

	data := map[string]any{
		"kind": in.Kind,
		"text": in.Text,
	}
	if len(in.Extra) > 0 {
		// Preserve raw JSON so SSE consumers can re-emit verbatim.
		data["extra"] = in.Extra
	}

	h.bus.Publish(events.Event{
		Type:      events.RunnerLogEvent,
		CardID:    in.CardID,
		Timestamp: time.Now(),
		Data:      data,
	})

	w.WriteHeader(http.StatusNoContent)
}
