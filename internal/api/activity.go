package api

import (
	"net/http"
	"strconv"

	"github.com/mhersson/contextmatrix/internal/service"
)

type activityHandlers struct {
	svc *service.CardService
}

// activityResponse is the envelope returned by GET /api/projects/{project}/activity.
// Uses `items` to match the project's other list endpoints (e.g. /cards).
type activityResponse struct {
	Items []service.ActivityFeedEntry `json:"items"`
}

// getActivity handles GET /api/projects/{project}/activity?limit=N.
// Returns the most-recent activity-log entries across all cards in the
// project, newest first. `limit` defaults to 50 and is capped at 500. The
// feed is rolling (no cursor): callers receive at most `limit` entries
// and refresh by re-fetching.
func (h *activityHandlers) getActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	project := r.PathValue("project")

	if project == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project", "")

		return
	}

	limit := 50

	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid limit", "")

			return
		}

		limit = n
	}

	items, err := h.svc.ListActivity(ctx, project, limit)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, activityResponse{Items: items})
}
