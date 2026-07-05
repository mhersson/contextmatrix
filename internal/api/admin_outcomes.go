package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// outcomeAdminStore is the op-store surface the admin model-outcomes
// endpoints need. Deliberately a separate, wider interface from
// outcomeStatsReader (runner.go): that one is read-only because runCard's
// selection path never resets recorded outcomes, and widening it to add
// ResetModelOutcomes would force every consumer (and test double) of the
// read-only surface to grow a method it never calls.
// opstore/sqlite.Store implements both.
type outcomeAdminStore interface {
	ModelOutcomeStats(ctx context.Context) ([]sqlite.OutcomeStats, error)
	ResetModelOutcomes(ctx context.Context) (int64, error)
}

// outcomeAdminHandlers serves GET/DELETE /api/admin/model-outcomes.
type outcomeAdminHandlers struct {
	store        outcomeAdminStore
	outcomeFloor int
	// authEnabled mirrors "multi mode": when true, both endpoints require an
	// admin session. In none mode they are open, same trust posture as
	// project management.
	authEnabled bool
}

// modelOutcomeStatsResponse is the GET /api/admin/model-outcomes body.
type modelOutcomeStatsResponse struct {
	OutcomeFloor int                      `json:"outcome_floor"`
	TotalSamples int                      `json:"total_samples"`
	Models       []modelOutcomeStatsEntry `json:"models"`
}

// modelOutcomeStatsEntry is one model's aggregated Best-of-N record.
type modelOutcomeStatsEntry struct {
	Model        string  `json:"model"`
	Samples      int     `json:"samples"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"win_rate"`
	ExpectedWins float64 `json:"expected_wins"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	// Active reports whether Samples has reached the configured
	// outcome_floor — below it, win-rate is too noisy to act on.
	Active bool `json:"active"`
}

// gate enforces the admin role in multi mode; a no-op (always true) in none
// mode. requireAdmin writes the 403 itself on failure.
func (h *outcomeAdminHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// getStats handles GET /api/admin/model-outcomes.
func (h *outcomeAdminHandlers) getStats(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	stats, err := h.store.ModelOutcomeStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read model outcomes", "")

		return
	}

	resp := modelOutcomeStatsResponse{
		OutcomeFloor: h.outcomeFloor,
		Models:       make([]modelOutcomeStatsEntry, 0, len(stats)),
	}

	for _, s := range stats {
		entry := modelOutcomeStatsEntry{
			Model:        s.Model,
			Samples:      s.Samples,
			Wins:         s.Wins,
			ExpectedWins: s.ExpectedWins,
			TotalCostUSD: s.TotalCostUSD,
			Active:       s.Samples >= h.outcomeFloor,
		}
		if s.Samples > 0 {
			entry.WinRate = float64(s.Wins) / float64(s.Samples)
		}

		resp.TotalSamples += s.Samples
		resp.Models = append(resp.Models, entry)
	}

	writeJSON(w, http.StatusOK, resp)
}

// reset handles DELETE /api/admin/model-outcomes.
func (h *outcomeAdminHandlers) reset(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	n, err := h.store.ResetModelOutcomes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to reset model outcomes", "")

		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
