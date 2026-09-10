package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix-protocol/selection"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// selectorLadderReader is the read side of the stored per-role ladders. The
// trigger path needs only this; the admin endpoints need selectorAdminStore.
type selectorLadderReader interface {
	SelectorLadders(ctx context.Context) (map[string]map[string]float64, time.Time, error)
}

// selectorAdminStore is the op-store surface the admin selector endpoints
// need. opstore/sqlite.Store implements it.
type selectorAdminStore interface {
	selectorLadderReader
	PutSelectorLadders(ctx context.Context, ladders map[string]map[string]float64) error
}

// selectorAdminHandlers serves /api/admin/selector/*: the stored ladders,
// the candidate catalog behind them, and a preview of what the shared
// selector would pick under a ladder the operator is editing.
type selectorAdminHandlers struct {
	store selectorAdminStore
	// authEnabled mirrors "multi mode": every endpoint then requires an
	// admin session. In none mode they are open, same trust posture as the
	// model-blacklist endpoints.
	authEnabled bool
}

// selectorLaddersResponse is the GET and PUT /api/admin/selector/ladders
// body. Ladders always carries both roles with all four tiers: the built-in
// ladder when nothing is stored (IsDefault true, no UpdatedAt).
type selectorLaddersResponse struct {
	Ladders   map[string]map[string]float64 `json:"ladders"`
	Defaults  map[string]float64            `json:"defaults"`
	IsDefault bool                          `json:"is_default"`
	UpdatedAt string                        `json:"updated_at,omitempty"`
}

type selectorLaddersRequest struct {
	Ladders map[string]map[string]float64 `json:"ladders"`
}

func (h *selectorAdminHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// defaultTierBarsWire is selection.DefaultTierBars keyed by tier name.
func defaultTierBarsWire() map[string]float64 {
	defaults := selection.DefaultTierBars()
	out := make(map[string]float64, len(defaults))

	for tier, bar := range defaults {
		out[string(tier)] = bar
	}

	return out
}

// laddersWire renders ladders as wire maps, both roles, all tiers; a nil
// Ladders reads as the built-in ladder through Bars.
func laddersWire(l selection.Ladders) map[string]map[string]float64 {
	out := make(map[string]map[string]float64, 2)

	for _, role := range []selection.Role{selection.RoleCoder, selection.RoleReviewer} {
		bars := l.Bars(role)
		out[string(role)] = make(map[string]float64, len(bars))

		for tier, bar := range bars {
			out[string(role)][string(tier)] = bar
		}
	}

	return out
}

// storedLadders reads the store and resolves it to a response: the built-in
// ladder for both roles when nothing is stored. The rows were validated on
// write, so a validation failure here is a corrupt store and is an error
// rather than something to paper over with the defaults.
func (h *selectorAdminHandlers) storedLadders(ctx context.Context) (selectorLaddersResponse, error) {
	raw, at, err := h.store.SelectorLadders(ctx)
	if err != nil {
		return selectorLaddersResponse{}, err
	}

	resp := selectorLaddersResponse{Defaults: defaultTierBarsWire(), IsDefault: len(raw) == 0}

	var ladders selection.Ladders

	if len(raw) > 0 {
		ladders, err = sqlite.ValidateSelectorLadders(raw)
		if err != nil {
			return selectorLaddersResponse{}, err
		}

		resp.UpdatedAt = at.UTC().Format(time.RFC3339)
	}

	resp.Ladders = laddersWire(ladders)

	return resp, nil
}

// getLadders handles GET /api/admin/selector/ladders.
func (h *selectorAdminHandlers) getLadders(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	resp, err := h.storedLadders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read selector ladders", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// putLadders handles PUT /api/admin/selector/ladders. Validation runs here
// so a bad ladder is a 422 naming the reason; the store validates again on
// its own write path, so the sentinel is mapped there too.
func (h *selectorAdminHandlers) putLadders(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	var req selectorLaddersRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if _, err := sqlite.ValidateSelectorLadders(req.Ladders); err != nil {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector ladders", ladderDetails(err))

		return
	}

	if err := h.store.PutSelectorLadders(r.Context(), req.Ladders); err != nil {
		if errors.Is(err, sqlite.ErrInvalidLadder) {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector ladders", ladderDetails(err))

			return
		}

		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to store selector ladders", "")

		return
	}

	resp, err := h.storedLadders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read selector ladders", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ladderDetails strips the sentinel prefix so the client sees the reason
// (missing role, non-monotone tier) rather than the wrapper.
func ladderDetails(err error) string {
	return strings.TrimPrefix(err.Error(), sqlite.ErrInvalidLadder.Error()+": ")
}
