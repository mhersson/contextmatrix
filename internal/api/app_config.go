package api

import (
	"net/http"

	"github.com/mhersson/contextmatrix/internal/board"
)

type appConfigHandlers struct {
	theme       string
	version     string
	taskBackend string
	// favorites holds operator-configured preferred slugs per tier, extracted
	// from the agent backend's TierFavorites.All lists. Key = tier name,
	// value = slug list. Nil / empty = no favorites configured.
	favorites map[string][]string
}

type appConfigResponse struct {
	Theme       string              `json:"theme"`
	Version     string              `json:"version"`
	TaskBackend string              `json:"task_backend"`
	Favorites   map[string][]string `json:"favorites,omitempty"`
}

// extractFavorites flattens TierFavorites.All slugs from the backend's per-tier
// favorites map into a plain map[tier][]slugs, skipping tiers with no All list.
// Returns nil when the input is empty (omitempty suppresses the JSON field).
func extractFavorites(src map[string]board.TierFavorites) map[string][]string {
	if len(src) == 0 {
		return nil
	}

	out := make(map[string][]string, len(src))

	for tier, tf := range src {
		if len(tf.All) > 0 {
			out[tier] = tf.All
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func (h *appConfigHandlers) getAppConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, appConfigResponse{
		Theme:       h.theme,
		Version:     h.version,
		TaskBackend: h.taskBackend,
		Favorites:   h.favorites,
	})
}
