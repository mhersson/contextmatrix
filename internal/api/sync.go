package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/gitsync"
)

// Syncer defines the sync operations needed by the API layer.
type Syncer interface {
	TriggerSync(ctx context.Context) error
	Status() gitsync.SyncStatus
}

// syncHandlers handles sync API endpoints.
type syncHandlers struct {
	syncer Syncer
	apiKey string
}

// requireAuth checks for a valid Bearer token when an API key is configured.
// Returns true if the request is authorized, false otherwise (error already written).
func (h *syncHandlers) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.apiKey == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		writeError(w, http.StatusUnauthorized, ErrCodeBadRequest, "missing or invalid Authorization header", "")
		return false
	}
	token := strings.TrimPrefix(auth, prefix)
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.apiKey)) != 1 {
		writeError(w, http.StatusForbidden, ErrCodeBadRequest, "invalid API key", "")
		return false
	}
	return true
}

// triggerSync handles POST /api/sync.
func (h *syncHandlers) triggerSync(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) {
		return
	}

	if h.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "SYNC_DISABLED",
			"sync is disabled (no remote configured)", "")
		return
	}

	if err := h.syncer.TriggerSync(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "SYNC_ERROR",
			"sync failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, h.syncer.Status())
}

// getSyncStatus handles GET /api/sync.
func (h *syncHandlers) getSyncStatus(w http.ResponseWriter, _ *http.Request) {
	if h.syncer == nil {
		writeJSON(w, http.StatusOK, gitsync.SyncStatus{Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, h.syncer.Status())
}
