package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
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
}

// triggerSync handles POST /api/sync.
func (h *syncHandlers) triggerSync(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "SYNC_DISABLED",
			"sync is disabled (no remote configured)", "")

		return
	}

	if err := h.syncer.TriggerSync(r.Context()); err != nil {
		// Log the raw error server-side — go-git transport errors typically
		// embed the remote URL and on-disk path. Sanitize before emitting
		// to the client so auth hints / filesystem layout don't leak.
		ctxlog.Logger(r.Context()).Error("sync failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "SYNC_ERROR",
			"sync failed", sanitizeErrorDetails(err))

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
