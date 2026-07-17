package api

import (
	"context"
	"net/http"
)

// modelCatalogHandlers serves GET /api/models - the pin-picker model source.
// Pins are an agent-backend concern, so this list must be reachable in every
// chat-mode combination (including config-mode or disabled chat), which is why
// it is separate from GET /api/chats/models.
type modelCatalogHandlers struct {
	served func(context.Context) []ServedModelView
	source string // "openrouter" or "endpoint"; "" when no catalog is wired
}

type modelCatalogEntry struct {
	ID        string `json:"id"`
	MaxTokens int64  `json:"max_tokens"`
}

func (h *modelCatalogHandlers) listModels(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Source string              `json:"source"`
		Models []modelCatalogEntry `json:"models"`
	}

	if h.served == nil {
		writeJSON(w, http.StatusOK, response{Source: "none", Models: []modelCatalogEntry{}})

		return
	}

	views := h.served(r.Context())
	models := make([]modelCatalogEntry, len(views))

	for i, v := range views {
		models[i] = modelCatalogEntry{ID: v.ID, MaxTokens: int64(v.ContextWindow)}
	}

	writeJSON(w, http.StatusOK, response{Source: h.source, Models: models})
}
