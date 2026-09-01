package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// modelCatalogHandlers serves GET /api/models - the pin-picker model source.
// Pins are an agent-backend concern, so this list must be reachable in every
// chat-mode combination (including config-mode or disabled chat), which is why
// it is separate from GET /api/chats/models.
type modelCatalogHandlers struct {
	served func(context.Context) []ServedModelView
	source string // "openrouter" or "endpoint"; "" when no catalog is wired
	// blacklist, when non-nil, marks entries reported incapable by the
	// agent backend (best-effort: a read failure serves the list without
	// flags). nil omits the field entirely.
	blacklist blacklistReader
}

type modelCatalogEntry struct {
	ID        string `json:"id"`
	MaxTokens int64  `json:"max_tokens"`
	// Blacklisted is true only for models in the opstore blacklist; omitempty
	// keeps the key off the wire for every other model.
	Blacklisted bool `json:"blacklisted,omitempty"`
}

// blacklistedSet does a best-effort blacklist read for the picker payloads.
// A nil reader or a read failure yields nil (no flags) - the pickers must
// never fail or lose models because the blacklist is unavailable; a failure
// is logged so the silent miss is traceable.
func blacklistedSet(ctx context.Context, reader blacklistReader) map[string]bool {
	if reader == nil {
		return nil
	}

	slugs, err := reader.BlacklistedSlugs(ctx)
	if err != nil {
		ctxlog.Logger(ctx).Warn("failed to read model blacklist; serving picker without flags",
			"error", err)

		return nil
	}

	set := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		set[s] = true
	}

	return set
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

	flagged := blacklistedSet(r.Context(), h.blacklist)

	views := h.served(r.Context())
	models := make([]modelCatalogEntry, len(views))

	for i, v := range views {
		models[i] = modelCatalogEntry{ID: v.ID, MaxTokens: int64(v.ContextWindow), Blacklisted: flagged[v.ID]}
	}

	writeJSON(w, http.StatusOK, response{Source: h.source, Models: models})
}
