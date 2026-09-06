package api

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// runCard handles POST /api/projects/{project}/cards/{id}/run - "Run Now".
func (h *backendHandlers) runCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can trigger remote execution", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	// Checked here as well as inside launch: a disabled backend must answer 503 before the card lookup can answer 404 or 409.
	if h.backend == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), project, id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// A card queued in an active playbook run is started by the run, never
	// by hand; resume from the playbook instead. Checked before the worker
	// gate so the answer names the real reason.
	if lock := card.PlaybookLock; lock.Active() {
		writeError(w, http.StatusConflict, ErrCodePlaybookRunActive,
			"card is queued in playbook "+lock.ID, "resume or stop the playbook run instead")

		return
	}

	// Parse optional JSON body for interactive flag.
	var runBody struct {
		Interactive bool `json:"interactive"`
	}
	if !decodeJSONAllowEmpty(w, r, &runBody) {
		return
	}

	card, err = h.launch(r.Context(), project, id, launchOptions{interactive: runBody.Interactive})
	if err != nil {
		writeLaunchError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, card)
}

// attachMob fills payload.Mob from the card's mob session fields. This
// handler set exists only for the agent backend, so no backend-kind check is
// needed. Participants are re-clamped against the CURRENT config (the
// trigger clamp is authoritative - mob.max_participants may have been
// lowered since the card was written); Rounds carries mob.default_rounds,
// which applyMobDefaults guarantees is within 1..max_rounds. Unknown guest
// names and a blocked "execute" phase degrade with a warning instead of
// failing the trigger - a discussion must never block a run. Mob coding
// takes trigger-time priority over Best-of-N: a live execute phase zeroes
// payload.BestOfN with a warning; with the flag off, execute is dropped
// (also with a warning) and Best-of-N is left untouched.
func (h *backendHandlers) attachMob(ctx context.Context, payload *backend.TriggerPayload, card *board.Card, project, id string) {
	if card.MobParticipants < 2 {
		return
	}

	spec := &protocol.MobSpec{
		Participants:       min(card.MobParticipants, h.mob.MaxParticipants),
		Rounds:             h.mob.DefaultRounds,
		BudgetFactor:       h.mob.BudgetFactor,
		ExecuteCheckpoints: h.mob.ExecuteCheckpoints(),
		CheckpointMinTier:  h.mob.CheckpointMinTier,
		CheckpointRounds:   h.mob.CheckpointRounds,
	}

	// Resolve guest names against the current registry; drop unknown names
	// (the registry may have changed since the card was written).
	byName := make(map[string]protocol.GuestSpec, len(h.mob.Guests))
	for _, g := range h.mob.Guests {
		byName[g.Name] = protocol.GuestSpec{Name: g.Name, URL: g.URL, Token: g.Token}
	}

	for _, name := range card.MobGuests {
		g, ok := byName[name]
		if !ok {
			h.recordMobWarning(ctx, project, id,
				fmt.Sprintf("mob guest %q is not registered; dropped for this run", name))

			continue
		}

		spec.Guests = append(spec.Guests, g)
	}

	// Phases pass through except "execute", which is dropped when the server
	// flag is off. The Best-of-N interaction is resolved AFTER the loop: mob
	// coding wins (the reverse of the original rule).
	for _, phase := range card.MobPhases {
		if phase == "execute" && !h.mob.ExecuteCheckpoints() {
			h.recordMobWarning(ctx, project, id,
				"mob execute checkpoints skipped: mob.execute_checkpoints_enabled is off")

			continue
		}

		spec.Phases = append(spec.Phases, phase)
	}

	// If the card explicitly selected phases but every one was filtered out
	// (e.g. an execute-only card with execute checkpoints off), do NOT attach
	// the spec: the agent expands empty Phases to its "review only" default,
	// which would silently run a discussion the operator never chose.
	// Run solo instead. Cards that never set mob_phases keep that default.
	if len(card.MobPhases) > 0 && len(spec.Phases) == 0 {
		h.recordMobWarning(ctx, project, id,
			"mob skipped: all requested phases are unavailable for this run; proceeding solo")

		return
	}

	// Mob coding takes priority over Best-of-N: a live execute phase zeroes
	// the candidate race. Checkpointing candidates would multiply cost xN
	// and break race independence, so one of the two must win - and the
	// operator's explicit execute selection is the stronger signal.
	if payload.BestOfN >= 2 && slices.Contains(spec.Phases, "execute") {
		h.recordMobWarning(ctx, project, id,
			"best_of_n ignored: mob coding takes priority (mutually exclusive with execute checkpoints)")

		payload.BestOfN = 0
	}

	payload.Mob = spec
}

// recordMobWarning logs a trigger-time mob session degradation and appends a
// best-effort activity entry so the drop is visible on the card - the same
// slog + AddLogEntry mechanism as the run-rejected trace in
// rejectLaunchForCredentialFailure. A failed append never blocks the trigger.
func (h *backendHandlers) recordMobWarning(ctx context.Context, project, id, msg string) {
	ctxlog.Logger(ctx).Warn(msg, "card_id", id, "project", project)

	if _, logErr := h.svc.AddLogEntry(ctx, project, id, board.ActivityEntry{
		Agent:   "system",
		Action:  "mob-warning",
		Message: msg,
	}); logErr != nil {
		ctxlog.Logger(ctx).Error("failed to record mob warning activity entry",
			"card_id", id, "project", project, "error", logErr)
	}
}

// resolveVerify merges a card's verify config over its project's (field-level,
// via board.ResolveVerify) and maps the result to the wire type. Returns nil
// when nothing resolves, so the agent falls back to its own detection.
func resolveVerify(card, project *board.VerifyConfig) *protocol.VerifyConfig {
	merged := board.ResolveVerify(card, project)
	if merged == nil {
		return nil
	}

	return &protocol.VerifyConfig{
		Command:        merged.Command,
		TimeoutSeconds: merged.TimeoutSeconds,
		Env:            merged.Env,
	}
}

// mergeFavorites flattens global+project per-tier favorites into wire rules.
// A project entry for a tier replaces the global entry for that tier.
func mergeFavorites(global, project map[string]board.TierFavorites) []protocol.FavoriteRule {
	merged := make(map[string]board.TierFavorites, len(global)+len(project))
	maps.Copy(merged, global)

	maps.Copy(merged, project)

	var rules []protocol.FavoriteRule

	for tier, f := range merged {
		if len(f.All) > 0 {
			rules = append(rules, protocol.FavoriteRule{Tier: tier, Models: f.All})
		}

		for role, models := range f.ByRole {
			if len(models) > 0 {
				rules = append(rules, protocol.FavoriteRule{Tier: tier, Role: role, Models: models})
			}
		}
	}

	return rules
}
