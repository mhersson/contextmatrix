package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Syncer defines the sync operations needed by the API layer: a manual
// trigger of one repo (empty means every enabled repo) and one status per
// configured repo.
type Syncer interface {
	TriggerSync(ctx context.Context, repo string) error
	Statuses() []gitsync.SyncStatus
}

// syncHandlers handles sync API endpoints.
type syncHandlers struct {
	syncer Syncer
}

// triggerSync handles POST /api/sync. The optional repo query names one
// boards repository; without it every enabled repo syncs.
func (h *syncHandlers) triggerSync(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeSyncDisabled,
			"sync is disabled (no remote configured)", "")

		return
	}

	repo := r.URL.Query().Get("repo")

	if err := h.syncer.TriggerSync(r.Context(), repo); err != nil {
		switch {
		case errors.Is(err, gitsync.ErrSyncDisabled):
			writeError(w, http.StatusServiceUnavailable, ErrCodeSyncDisabled,
				"sync is disabled (no remote configured)", repo)
		case errors.Is(err, storage.ErrUnknownRepo):
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "unknown boards repo", repo)
		default:
			// Log the raw error server-side - go-git transport errors
			// typically embed the remote URL and on-disk path. Sanitize
			// before emitting to the client.
			ctxlog.Logger(r.Context()).Error("sync failed", "repo", repo, "error", err.Error())
			writeError(w, http.StatusInternalServerError, ErrCodeSyncError,
				"sync failed", sanitizeErrorDetails(err))
		}

		return
	}

	writeJSON(w, http.StatusOK, h.syncer.Statuses())
}

// getSyncStatus handles GET /api/sync: one status per boards repository.
func (h *syncHandlers) getSyncStatus(w http.ResponseWriter, _ *http.Request) {
	if h.syncer == nil {
		writeJSON(w, http.StatusOK, []gitsync.SyncStatus{})

		return
	}

	writeJSON(w, http.StatusOK, h.syncer.Statuses())
}
