package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// outcomeAdminStore is the op-store surface the admin model-outcomes
// endpoints need. Deliberately a separate, wider interface from
// outcomeStatsReader (backend_handlers.go): that one is read-only because runCard's
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
	store outcomeAdminStore
	// authEnabled mirrors "multi mode": when true, both endpoints require an
	// admin session. In none mode they are open, same trust posture as
	// project management.
	authEnabled bool
}

// modelOutcomeStatsResponse is the GET /api/admin/model-outcomes body. The
// stats are an observability ledger only - selection is priors-based and
// never reads them.
type modelOutcomeStatsResponse struct {
	TotalSamples int                      `json:"total_samples"`
	Models       []modelOutcomeStatsEntry `json:"models"`
}

// modelOutcomeStatsEntry is one model's recorded history, split by kind:
// race rows are Best-of-N head-to-head results, solo rows are single-model
// runs where only a failure carries signal. The two are never summed into
// one win-rate - a solo completion is not a win over anything.
type modelOutcomeStatsEntry struct {
	Model        string  `json:"model"`
	RaceSamples  int     `json:"race_samples"`
	RaceWins     int     `json:"race_wins"`
	RaceWinRate  float64 `json:"race_win_rate"`
	SoloSamples  int     `json:"solo_samples"`
	SoloFailures int     `json:"solo_failures"`
	TotalCostUSD float64 `json:"total_cost_usd"`
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
		Models: make([]modelOutcomeStatsEntry, 0, len(stats)),
	}

	for _, s := range stats {
		entry := modelOutcomeStatsEntry{
			Model:        s.Model,
			RaceSamples:  s.RaceSamples,
			RaceWins:     s.RaceWins,
			SoloSamples:  s.SoloSamples,
			SoloFailures: s.SoloFailures,
			TotalCostUSD: s.TotalCostUSD,
		}
		if s.RaceSamples > 0 {
			entry.RaceWinRate = float64(s.RaceWins) / float64(s.RaceSamples)
		}

		resp.TotalSamples += s.RaceSamples + s.SoloSamples
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
