package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

const ErrCodeModelNotBlacklisted = "MODEL_NOT_BLACKLISTED"

// blacklistAdminStore is the op-store surface the admin model-blacklist
// endpoints need. Deliberately separate from blacklistReader
// (backend_handlers.go): that one is slug-only because runCard's trigger
// path never needs reasons or deletion, and widening it would force every
// consumer and test double of the narrow surface to grow methods it never
// calls. opstore/sqlite.Store implements both.
type blacklistAdminStore interface {
	BlacklistEntries(ctx context.Context) ([]sqlite.BlacklistEntry, error)
	DeleteBlacklistEntry(ctx context.Context, slug string) (bool, error)
}

// blacklistAdminHandlers serves GET /api/admin/model-blacklist and
// DELETE /api/admin/model-blacklist/{slug...}.
type blacklistAdminHandlers struct {
	store blacklistAdminStore
	// authEnabled mirrors "multi mode": when true, both endpoints require an
	// admin session. In none mode they are open, same trust posture as the
	// model-outcomes endpoints.
	authEnabled bool
}

// modelBlacklistResponse is the GET /api/admin/model-blacklist body.
type modelBlacklistResponse struct {
	Models []modelBlacklistEntry `json:"models"`
}

// modelBlacklistEntry is one blacklisted model. Timestamps are unix seconds.
type modelBlacklistEntry struct {
	Slug       string `json:"slug"`
	Reason     string `json:"reason"`
	SampleCard string `json:"sample_card,omitempty"`
	ReportedBy string `json:"reported_by"`
	FirstSeen  int64  `json:"first_seen"`
	LastSeen   int64  `json:"last_seen"`
}

func (h *blacklistAdminHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// list handles GET /api/admin/model-blacklist.
func (h *blacklistAdminHandlers) list(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	entries, err := h.store.BlacklistEntries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read model blacklist", "")

		return
	}

	resp := modelBlacklistResponse{Models: make([]modelBlacklistEntry, 0, len(entries))}

	for _, e := range entries {
		resp.Models = append(resp.Models, modelBlacklistEntry{
			Slug:       e.Slug,
			Reason:     e.Reason,
			SampleCard: e.SampleCard,
			ReportedBy: e.ReportedBy,
			FirstSeen:  e.FirstSeen,
			LastSeen:   e.LastSeen,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// delist handles DELETE /api/admin/model-blacklist/{slug...}. The rest
// wildcard is required: model slugs contain a slash (z-ai/glm-5.2).
func (h *blacklistAdminHandlers) delist(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	slug := r.PathValue("slug")

	deleted, err := h.store.DeleteBlacklistEntry(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to delete blacklist entry", "")

		return
	}

	if !deleted {
		writeError(w, http.StatusNotFound, ErrCodeModelNotBlacklisted, "model is not blacklisted", slug)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": slug})
}
