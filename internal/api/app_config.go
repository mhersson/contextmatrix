package api

import (
	"net/http"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
)

type appConfigHandlers struct {
	theme       string
	version     string
	taskBackend string
	// favorites holds operator-configured preferred slugs per tier, extracted
	// from the agent backend's TierFavorites.All lists. Key = tier name,
	// value = slug list. Nil / empty = no favorites configured.
	favorites map[string][]string
	// authMode is "multi" or "none" ("" reported as "none"). In multi mode
	// the full payload requires a session; unauthenticated callers get only
	// what the login page needs.
	authMode string
	// bestOfNMax/bestOfNDefault surface config.BestOfNConfig's UI-facing
	// bounds (full payload only — see appConfigSlimResponse).
	bestOfNMax     int
	bestOfNDefault int
	// coopMaxParticipants/coopDefaultParticipants/coopGuestNames surface the
	// coop block's UI-facing bounds and the registry guest NAMES (never URLs
	// or tokens). Full payload only, like the best_of_n fields.
	coopMaxParticipants     int
	coopDefaultParticipants int
	coopGuestNames          []string
}

type appConfigResponse struct {
	Theme                   string              `json:"theme"`
	Version                 string              `json:"version"`
	AuthMode                string              `json:"auth_mode"`
	TaskBackend             string              `json:"task_backend"`
	Favorites               map[string][]string `json:"favorites,omitempty"`
	BestOfNMax              int                 `json:"best_of_n_max,omitempty"`
	BestOfNDefault          int                 `json:"best_of_n_default,omitempty"`
	CoopMaxParticipants     int                 `json:"coop_max_participants,omitempty"`
	CoopDefaultParticipants int                 `json:"coop_default_participants,omitempty"`
	CoopGuestNames          []string            `json:"coop_guest_names,omitempty"`
}

// appConfigSlimResponse is served to unauthenticated callers in multi mode:
// only what the login page needs. task_backend and favorites must be
// genuinely absent from the JSON, not just empty — a shared struct with an
// `omitempty` tag on TaskBackend can't distinguish "not configured" (still
// full payload, e.g. none mode with no backend wired) from "not permitted to
// see" (slim payload), since both collapse to the zero value.
type appConfigSlimResponse struct {
	Theme    string `json:"theme"`
	Version  string `json:"version"`
	AuthMode string `json:"auth_mode"`
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

func (h *appConfigHandlers) getAppConfig(w http.ResponseWriter, r *http.Request) {
	mode := h.authMode
	if mode == "" {
		mode = "none"
	}

	// Multi mode without a session: the full payload (backend, favorites)
	// requires a session. The guard soft-resolves sessions on this exempt
	// path, so the context tells us whether the caller is logged in.
	if mode == "multi" && sessionUserFromContext(r.Context()) == nil {
		writeJSON(w, http.StatusOK, appConfigSlimResponse{Theme: h.theme, Version: h.version, AuthMode: mode})

		return
	}

	// None mode, or an authenticated caller in multi mode: full, as always.
	writeJSON(w, http.StatusOK, appConfigResponse{
		Theme:                   h.theme,
		Version:                 h.version,
		AuthMode:                mode,
		TaskBackend:             h.taskBackend,
		Favorites:               h.favorites,
		BestOfNMax:              h.bestOfNMax,
		BestOfNDefault:          h.bestOfNDefault,
		CoopMaxParticipants:     h.coopMaxParticipants,
		CoopDefaultParticipants: h.coopDefaultParticipants,
		CoopGuestNames:          h.coopGuestNames,
	})
}

// coopGuestNames extracts just the registry names for the UI guest
// multi-select — URLs and bearer tokens never leave the server.
func coopGuestNames(guests []config.CoopGuest) []string {
	if len(guests) == 0 {
		return nil
	}

	names := make([]string, len(guests))
	for i, g := range guests {
		names[i] = g.Name
	}

	return names
}
