package api

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

// enableFeatureBranchAndPR is a workflow invariant: every "Run now" trigger
// and every promote-to-autonomous flow gets feature_branch=true + create_pr=true.
// Returns the refreshed card on success.
//
// Skip condition matches the original inline blocks: feature_branch=true is
// treated as the existing "branch+PR pipeline already configured" signal, so
// CreatePR is intentionally left untouched in that case. Existing tests rely
// on this behaviour (HITL "Run now" on a card with feature_branch=true must
// not implicitly flip create_pr).
func (h *backendHandlers) enableFeatureBranchAndPR(ctx context.Context, project, id string, card *board.Card) (*board.Card, error) {
	if card.FeatureBranch {
		return card, nil
	}

	return h.svc.PatchCard(ctx, project, id, service.PatchCardInput{
		FeatureBranch: new(true),
		CreatePR:      new(true),
	})
}

// runCard handles POST /api/projects/{project}/cards/{id}/run — "Run Now".
func (h *backendHandlers) runCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can trigger remote execution", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	if h.backend == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), project, id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Parse optional JSON body for interactive flag.
	var runBody struct {
		Interactive bool `json:"interactive"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		// Tolerate empty body — only parse when there's content.
		if decodeErr := json.NewDecoder(r.Body).Decode(&runBody); decodeErr != nil {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

			return
		}
	}

	if card.State != board.StateTodo {
		writeError(w, http.StatusConflict, ErrCodeInvalidTransition,
			"card must be in todo state to run", fmt.Sprintf("current state: %s", card.State))

		return
	}

	if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
		writeError(w, http.StatusConflict, ErrCodeWorkerConflict,
			"card is already being executed by a worker", fmt.Sprintf("worker_status: %s", card.WorkerStatus))

		return
	}

	// Check per-project remote execution setting.
	if !h.isRemoteExecutionEnabled(r, project) {
		writeError(w, http.StatusForbidden, ErrCodeBackendDisabled,
			"remote execution is disabled for this project", "")

		return
	}

	// Auto-enable feature_branch and create_pr for all "Run now" triggers —
	// both autonomous and HITL (interactive) runs get a feature branch and PR.
	// The patched card is intentionally discarded here: UpdateWorkerStatus
	// a few lines below refreshes `card` from disk, so any flags set above
	// are observable on the returned card. Keep this in mind when adding
	// fields whose stale value would matter before that refresh.
	if _, patchErr := h.enableFeatureBranchAndPR(r.Context(), project, id, card); patchErr != nil {
		handleServiceError(w, r, patchErr)

		return
	}

	// Get project config to retrieve repo URL and worker image.
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Set worker_status to queued.
	card, err = h.svc.UpdateWorkerStatus(r.Context(), project, id, "queued", "task queued for worker")
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Build trigger payload. Model is the backend's default_model — per-card
	// pin overrides are resolved agent-side.
	model := h.backendCfg.DefaultModel

	// Resolve task skills: card.Skills > project.DefaultSkills > nil (mount full set).
	var taskSkills *[]string

	switch {
	case card.Skills != nil:
		taskSkills = card.Skills
	case projectCfg.DefaultSkills != nil:
		taskSkills = projectCfg.DefaultSkills
	}

	// Autonomous cards always run the backend's autonomous path (the agent
	// FSM). interactive is a HITL-only mode,
	// so force it off for autonomous cards — CM owns this invariant server-side
	// rather than trusting the client flag (defense in depth: a stray trigger
	// cannot push an autonomous card down the HITL path).
	interactive := runBody.Interactive && !card.Autonomous

	payload := backend.TriggerPayload{
		CardID:      id,
		Project:     project,
		RepoURL:     projectCfg.Repo,
		MCPAPIKey:   h.mcpAPIKey,
		BaseBranch:  card.BaseBranch,
		Interactive: interactive,
		Model:       model,
		TaskSkills:  taskSkills,
	}

	// Clamp Best-of-N against the configured max — the stored card value can
	// exceed it if max_candidates was lowered after the card was set, since
	// the REST PATCH/PUT validation only checks the max in effect at write
	// time.
	if card.BestOfN >= 2 {
		payload.BestOfN = min(card.BestOfN, h.bestOfN.MaxCandidates)
	}

	h.attachCoop(r.Context(), &payload, card, project, id)

	// Verify: CM resolves card-over-project and sends it so the agent's
	// verify gate uses the operator-declared command.
	payload.Verify = resolveVerify(card.Verify, projectCfg.Verify)

	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.WorkerImage != "" {
		payload.WorkerImage = projectCfg.RemoteExecution.WorkerImage
	}

	if h.catalog != nil {
		var bl []string

		if h.blacklist != nil {
			// Best-effort: a blacklist read failure must not block the trigger,
			// but it is logged — a silent miss would let the agent re-select a
			// known-incapable model with no trace.
			var blErr error
			if bl, blErr = h.blacklist.BlacklistedSlugs(r.Context()); blErr != nil {
				ctxlog.Logger(r.Context()).Warn("failed to read model blacklist; proceeding without it",
					"card_id", id, "project", project, "error", blErr)
			}
		}

		// Clone: the catalog returns its shared cached slice, and the
		// outcomes-attach loop below writes Candidates[i].Outcomes in place.
		// Without a defensive copy that write would alias the catalog's
		// backing array - racing concurrent runCard requests and leaking
		// stale Outcomes pointers into the cache past a model-outcomes reset.
		payload.Selection = &protocol.SelectionContext{
			Candidates: slices.Clone(h.catalog.Candidates(r.Context())),
			Favorites:  mergeFavorites(h.backendCfg.Favorites, projectCfg.Favorites),
			Blacklist:  bl,
		}

		payload.Selection.OutcomeFloor = h.bestOfN.OutcomeFloor

		if h.outcomes != nil {
			// Best-effort, mirroring the blacklist read above: a stats read
			// failure must not block the trigger, but it is logged so a silent
			// miss doesn't let selection quietly run unbiased with no trace.
			stats, statsErr := h.outcomes.ModelOutcomeStats(r.Context())
			if statsErr != nil {
				ctxlog.Logger(r.Context()).Warn("failed to read model outcomes; selection proceeds without them",
					"card_id", id, "project", project, "error", statsErr)
			} else if len(stats) > 0 {
				byModel := make(map[string]sqlite.OutcomeStats, len(stats))
				for _, st := range stats {
					byModel[st.Model] = st
				}

				for i, c := range payload.Selection.Candidates {
					if st, ok := byModel[c.Slug]; ok {
						payload.Selection.Candidates[i].Outcomes = &protocol.OutcomeStats{
							Samples: st.Samples, Wins: st.Wins, ExpectedWins: st.ExpectedWins,
						}
					}
				}
			}
		}
	}

	// Mint the project-scoped git token. Fail closed: a broken binding
	// rejects the run — never the instance credential by accident.
	if h.providerForProject != nil {
		provider, _, providerErr := h.providerForProject(r.Context(), project)
		if providerErr != nil {
			h.rejectRunForCredentialFailure(w, r, project, id, providerErr)

			return
		}

		token, expiresAt, tokenErr := provider.GenerateToken(r.Context())
		if tokenErr != nil {
			h.rejectRunForCredentialFailure(w, r, project, id, tokenErr)

			return
		}

		payload.GitToken = token
		payload.GitTokenExpiresAt = tokenExpiryString(expiresAt)
	}

	payload.LLMEndpoint = h.llmEndpoint

	// Send trigger webhook.
	if err := h.backend.Trigger(r.Context(), payload); err != nil {
		ctxlog.Logger(r.Context()).Error("backend webhook failed", "card_id", id, "project", project, "error", err)
		// Webhook failed — revert status to failed.
		// Use context.WithoutCancel so the revert succeeds even when the HTTP client
		// has already disconnected and r.Context() is cancelled.
		revertCtx := context.WithoutCancel(r.Context())
		if _, revertErr := h.svc.UpdateWorkerStatus(revertCtx, project, id, "failed",
			"webhook trigger failed"); revertErr != nil {
			ctxlog.Logger(r.Context()).Error("failed to revert worker_status after webhook failure",
				"card_id", id, "project", project, "error", revertErr)
		}

		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable,
			"failed to trigger backend task", "")

		return
	}

	writeJSON(w, http.StatusAccepted, card)
}

// rejectRunForCredentialFailure writes the fail-closed 409 response for a
// broken or unresolvable project git-token provider (either providerForProject
// itself failed, or the resolved provider's GenerateToken call did).
//
// runCard has already set worker_status to "queued" by this point, so the
// rejection first reverts it to "failed" — mirroring the webhook-failure
// revert below (context.WithoutCancel so a client disconnect cannot strand
// the rollback). Without the revert, the already-queued guard at the top of
// runCard would 409 every future trigger of this card until a manual stop.
// The revert runs before the activity append so the run-rejected trace stays
// the most recent entry (UpdateWorkerStatus appends its own worker_status
// entry). Both writes are best-effort: failures are logged but never change
// the 409 response, since the caller has already been told the run was
// rejected.
//
// err is only ever a credential-resolution error from internal/auth (embeds
// the credential/project name, never secret material) or a githubauth
// provider error (JWT/HTTP-status class messages, never the token or key);
// sanitizeErrorDetails additionally scrubs any transport/filesystem-path
// leakage, so it is safe to surface as the error's details field here.
func (h *backendHandlers) rejectRunForCredentialFailure(w http.ResponseWriter, r *http.Request, project, id string, err error) {
	revertCtx := context.WithoutCancel(r.Context())
	if _, revertErr := h.svc.UpdateWorkerStatus(revertCtx, project, id, "failed",
		"trigger rejected: project credential unavailable"); revertErr != nil {
		ctxlog.Logger(r.Context()).Error("failed to revert worker_status after credential failure",
			"card_id", id, "project", project, "error", revertErr)
	}

	if _, logErr := h.svc.AddLogEntry(revertCtx, project, id, board.ActivityEntry{
		Agent:   "system",
		Action:  "run-rejected",
		Message: fmt.Sprintf("run rejected: project credential unavailable (%s)", project),
	}); logErr != nil {
		ctxlog.Logger(r.Context()).Error("failed to record run-rejected activity entry",
			"card_id", id, "project", project, "error", logErr)
	}

	writeError(w, http.StatusConflict, ErrCodeValidationError,
		"project credential unavailable", sanitizeErrorDetails(err))
}

// attachCoop fills payload.Coop from the card's co-op fields. This handler
// set exists only for the agent backend, so no backend-kind check is needed.
// Participants are re-clamped against the CURRENT config (the trigger clamp
// is authoritative — coop.max_participants may have been lowered since the
// card was written); Rounds carries coop.default_rounds, which
// applyCoopDefaults guarantees is within 1..max_rounds. Unknown guest names
// and a blocked "execute" phase degrade with a warning instead of failing
// the trigger — a discussion must never block a run.
func (h *backendHandlers) attachCoop(ctx context.Context, payload *backend.TriggerPayload, card *board.Card, project, id string) {
	if card.CoopParticipants < 2 {
		return
	}

	spec := &protocol.CoopSpec{
		Participants:       min(card.CoopParticipants, h.coop.MaxParticipants),
		Rounds:             h.coop.DefaultRounds,
		BudgetFactor:       h.coop.BudgetFactor,
		ExecuteCheckpoints: h.coop.ExecuteCheckpointsEnabled,
		CheckpointMinTier:  h.coop.CheckpointMinTier,
	}

	// Resolve guest names against the current registry; drop unknown names
	// (the registry may have changed since the card was written).
	byName := make(map[string]protocol.GuestSpec, len(h.coop.Guests))
	for _, g := range h.coop.Guests {
		byName[g.Name] = protocol.GuestSpec{Name: g.Name, URL: g.URL, Token: g.Token}
	}

	for _, name := range card.CoopGuests {
		g, ok := byName[name]
		if !ok {
			h.recordCoopWarning(ctx, project, id,
				fmt.Sprintf("co-op guest %q is not registered; dropped for this run", name))

			continue
		}

		spec.Guests = append(spec.Guests, g)
	}

	// Phases pass through except "execute": dropped when the server flag is
	// off, or when the run races Best-of-N candidates (checkpoints and BoN
	// are mutually exclusive; BoN wins).
	for _, phase := range card.CoopPhases {
		if phase == "execute" {
			switch {
			case payload.BestOfN >= 2:
				h.recordCoopWarning(ctx, project, id,
					"co-op execute checkpoints skipped: best_of_n >= 2 (mutually exclusive; Best-of-N wins)")

				continue
			case !h.coop.ExecuteCheckpointsEnabled:
				h.recordCoopWarning(ctx, project, id,
					"co-op execute checkpoints skipped: coop.execute_checkpoints_enabled is off")

				continue
			}
		}

		spec.Phases = append(spec.Phases, phase)
	}

	payload.Coop = spec
}

// recordCoopWarning logs a trigger-time co-op degradation and appends a
// best-effort activity entry so the drop is visible on the card — the same
// slog + AddLogEntry mechanism as the run-rejected trace in
// rejectRunForCredentialFailure. A failed append never blocks the trigger.
func (h *backendHandlers) recordCoopWarning(ctx context.Context, project, id, msg string) {
	ctxlog.Logger(ctx).Warn(msg, "card_id", id, "project", project)

	if _, logErr := h.svc.AddLogEntry(ctx, project, id, board.ActivityEntry{
		Agent:   "system",
		Action:  "coop-warning",
		Message: msg,
	}); logErr != nil {
		ctxlog.Logger(ctx).Error("failed to record co-op warning activity entry",
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
