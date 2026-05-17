package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type activityHandlers struct {
	svc *service.CardService
}

type activityEntryDTO struct {
	Agent   string    `json:"agent"`
	Action  string    `json:"action"`
	Message string    `json:"message,omitempty"`
	CardID  string    `json:"card_id"`
	TS      time.Time `json:"ts"`
}

type activityResponse struct {
	Entries []activityEntryDTO `json:"entries"`
}

// getActivity handles GET /api/projects/{project}/activity?limit=N.
// Flattens per-card activity_log into a single chronological feed (newest first).
func (h *activityHandlers) getActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	project := r.PathValue("project")

	if project == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project", "")

		return
	}

	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}

	if limit > 500 {
		limit = 500
	}

	cards, err := h.svc.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	out := make([]activityEntryDTO, 0, limit)

	for _, c := range cards {
		for _, e := range c.ActivityLog {
			out = append(out, activityEntryDTO{
				Agent:   e.Agent,
				Action:  e.Action,
				Message: e.Message,
				CardID:  c.ID,
				TS:      e.Timestamp,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })

	if len(out) > limit {
		out = out[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(activityResponse{Entries: out})
}
