package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Error codes for runner-related errors.
//
// The previous ErrCodeRunnerError was emitted with both 409 and 502 — callers
// could not tell an already-running card from an unreachable runner host.
// The codes below split that axis: conflict = 409, unavailable = 502.
const (
	ErrCodeRunnerDisabled    = "RUNNER_DISABLED"
	ErrCodeRunnerConflict    = "RUNNER_CONFLICT"
	ErrCodeRunnerUnavailable = "RUNNER_UNAVAILABLE"
	ErrCodeRunnerNotRunning  = "RUNNER_NOT_RUNNING"
)

// catalogProvider supplies the current auto-selectable model candidates.
// Implemented by modelcatalog.Builder; the interface lives here so the api
// package does not import modelcatalog.
type catalogProvider interface {
	Candidates(ctx context.Context) []protocol.CandidateModel
}

// blacklistReader returns the set of OpenRouter slugs that must never be
// auto-selected. Implemented by opstore/sqlite.Store; lives here for the same
// reason as catalogProvider.
type blacklistReader interface {
	BlacklistedSlugs(ctx context.Context) ([]string, error)
}

// outcomeStatsReader supplies Best-of-N head-to-head aggregates for
// selection. Implemented by opstore/sqlite.Store; lives here for the same
// reason as catalogProvider and blacklistReader.
type outcomeStatsReader interface {
	ModelOutcomeStats(ctx context.Context) ([]sqlite.OutcomeStats, error)
}

// runnerHandlers contains handlers for remote execution endpoints. This
// handler set exists only for the agent backend — the callback endpoints it
// serves mount at config.AgentCallbackPath.
type runnerHandlers struct {
	svc               *service.CardService
	runner            TaskBackend                // nil when no task backend is configured
	backendCfg        *config.AgentBackendConfig // resolved agent entry; never nil (NewRouter normalizes an absent entry to the zero value)
	mcpAPIKey         string
	sessionManager    *sessionlog.Manager // nil when session manager is not configured
	keepaliveInterval time.Duration       // zero → use default (30s)

	taskSkillsDir          string
	taskSkillsGitRemoteURL string

	// catalog and blacklist supply model-selection inputs for agent-backend
	// triggers. Both are nil until T8 wires the real implementations in main.go;
	// runCard guards on catalog != nil before attaching Selection.
	catalog   catalogProvider
	blacklist blacklistReader

	// outcomes supplies Best-of-N head-to-head aggregates attached per-candidate
	// to SelectionContext.Candidates[i].Outcomes. nil disables attachment; a
	// read failure is best-effort (logged, selection proceeds without stats),
	// mirroring the blacklist read above.
	outcomes outcomeStatsReader

	// bestOfN bounds the card-level best_of_n value forwarded to the agent
	// backend (payload.BestOfN = min(card.BestOfN, bestOfN.MaxCandidates)) and
	// supplies SelectionContext.OutcomeFloor. Zero value (MaxCandidates 0)
	// clamps every non-zero card value down to 0, matching RouterConfig.BestOfN's
	// pre-config.Load-defaults zero-value contract.
	bestOfN config.BestOfNConfig

	// replayCache guards the runner-callback authentication path against
	// replayed HMAC signatures. Populated at construction time; non-nil
	// whenever a runner API key is configured.
	replayCache *runner.SignatureCache

	// providerForProject resolves the project-scoped git-token provider used
	// to mint the trigger payload's GitToken: the project's credential
	// binding when set (fail-closed on a broken one), else the instance
	// provider. nil preserves pre-token-authority behavior — no GitToken is
	// attached and the trigger is never rejected on its account.
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error)

	// llmEndpoint is the CM-provisioned inference endpoint attached to every
	// trigger payload. nil when llm_endpoint is unconfigured — backends then
	// fall back to their own local config.
	llmEndpoint *protocol.LLMEndpoint

	// instanceTokenProvider mints the instance-scoped git credential attached
	// to task-skills-source responses. Unlike providerForProject (per-project,
	// fail-closed on a broken binding), this is the flat instance provider —
	// task-skills is never project-scoped, so there is no binding to fail
	// closed on. nil disables the token fields (pre-token-authority behavior).
	instanceTokenProvider githubauth.TokenGenerator

	// healthCache memoises /runner/health responses so concurrent browser tabs
	// don't each fire a fresh probe at the runner — and so a runner outage
	// doesn't cause every refresh to block for the full per-request timeout.
	healthCache healthProbeCache
}

// healthProbeCache is a small TTL cache for runner /health responses.
// Both successes and failures are cached so an outage doesn't produce a
// thundering-herd of blocked goroutines hitting a dead runner.
//
// Concurrent callers arriving during a cold-or-expired window are
// coalesced via singleflight, so exactly one upstream probe runs per
// TTL window regardless of inbound concurrency. The probe itself uses
// a detached context so a single caller cancelling mid-probe cannot
// poison the cache for the rest of the TTL window.
type healthProbeCache struct {
	mu      sync.Mutex
	expires time.Time
	info    runner.HealthInfo
	err     error

	flight singleflight.Group
}

// runnerHealthCacheTTL is how long a /runner/health probe result is reused.
// Short enough that operators see capacity changes promptly, long enough to
// dampen multi-tab refresh storms.
const runnerHealthCacheTTL = 2 * time.Second

// runnerHealthProbeTimeout is the per-probe timeout for upstream /health
// calls. Tighter than the runner client's default 10s so a hung runner
// doesn't pin every browser tab for that long.
const runnerHealthProbeTimeout = 3 * time.Second

// enableFeatureBranchAndPR is a workflow invariant: every "Run now" trigger
// and every promote-to-autonomous flow gets feature_branch=true + create_pr=true.
// Returns the refreshed card on success.
//
// Skip condition matches the original inline blocks: feature_branch=true is
// treated as the existing "branch+PR pipeline already configured" signal, so
// CreatePR is intentionally left untouched in that case. Existing tests rely
// on this behaviour (HITL "Run now" on a card with feature_branch=true must
// not implicitly flip create_pr).
func (h *runnerHandlers) enableFeatureBranchAndPR(ctx context.Context, project, id string, card *board.Card) (*board.Card, error) {
	if card.FeatureBranch {
		return card, nil
	}

	return h.svc.PatchCard(ctx, project, id, service.PatchCardInput{
		FeatureBranch: new(true),
		CreatePR:      new(true),
	})
}

// runCard handles POST /api/projects/{project}/cards/{id}/run — "Run Now".
func (h *runnerHandlers) runCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can trigger remote execution", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

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

	if card.RunnerStatus == "queued" || card.RunnerStatus == "running" {
		writeError(w, http.StatusConflict, ErrCodeRunnerConflict,
			"card is already being executed by the runner", fmt.Sprintf("runner_status: %s", card.RunnerStatus))

		return
	}

	// Check per-project remote execution setting.
	if !h.isRemoteExecutionEnabled(r, project) {
		writeError(w, http.StatusForbidden, ErrCodeRunnerDisabled,
			"remote execution is disabled for this project", "")

		return
	}

	// Auto-enable feature_branch and create_pr for all "Run now" triggers —
	// both autonomous and HITL (interactive) runs get a feature branch and PR.
	// The patched card is intentionally discarded here: UpdateRunnerStatus
	// a few lines below refreshes `card` from disk, so any flags set above
	// are observable on the returned card. Keep this in mind when adding
	// fields whose stale value would matter before that refresh.
	if _, patchErr := h.enableFeatureBranchAndPR(r.Context(), project, id, card); patchErr != nil {
		handleServiceError(w, r, patchErr)

		return
	}

	// Get project config to retrieve repo URL and runner image.
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Set runner_status to queued.
	card, err = h.svc.UpdateRunnerStatus(r.Context(), project, id, "queued", "task queued for runner")
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
	// FSM; the runner's autonomous workflow). interactive is a HITL-only mode,
	// so force it off for autonomous cards — CM owns this invariant server-side
	// rather than trusting the client flag (defense in depth: a stray trigger
	// cannot push an autonomous card down the HITL path).
	interactive := runBody.Interactive && !card.Autonomous

	payload := runner.TriggerPayload{
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

	// Verify: CM resolves card-over-project and sends it so the agent's
	// verify gate uses the operator-declared command.
	payload.Verify = resolveVerify(card.Verify, projectCfg.Verify)

	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.RunnerImage != "" {
		payload.RunnerImage = projectCfg.RemoteExecution.RunnerImage
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
	if err := h.runner.Trigger(r.Context(), payload); err != nil {
		ctxlog.Logger(r.Context()).Error("runner webhook failed", "card_id", id, "project", project, "error", err)
		// Webhook failed — revert status to failed.
		// Use context.WithoutCancel so the revert succeeds even when the HTTP client
		// has already disconnected and r.Context() is cancelled.
		revertCtx := context.WithoutCancel(r.Context())
		if _, revertErr := h.svc.UpdateRunnerStatus(revertCtx, project, id, "failed",
			"webhook trigger failed"); revertErr != nil {
			ctxlog.Logger(r.Context()).Error("failed to revert runner status after webhook failure",
				"card_id", id, "project", project, "error", revertErr)
		}

		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable,
			"failed to trigger runner", "")

		return
	}

	writeJSON(w, http.StatusAccepted, card)
}

// rejectRunForCredentialFailure writes the fail-closed 409 response for a
// broken or unresolvable project git-token provider (either providerForProject
// itself failed, or the resolved provider's GenerateToken call did).
//
// runCard has already set runner_status to "queued" by this point, so the
// rejection first reverts it to "failed" — mirroring the webhook-failure
// revert below (context.WithoutCancel so a client disconnect cannot strand
// the rollback). Without the revert, the already-queued guard at the top of
// runCard would 409 every future trigger of this card until a manual stop.
// The revert runs before the activity append so the run-rejected trace stays
// the most recent entry (UpdateRunnerStatus appends its own runner_status
// entry). Both writes are best-effort: failures are logged but never change
// the 409 response, since the caller has already been told the run was
// rejected.
//
// err is only ever a credential-resolution error from internal/auth (embeds
// the credential/project name, never secret material) or a githubauth
// provider error (JWT/HTTP-status class messages, never the token or key);
// sanitizeErrorDetails additionally scrubs any transport/filesystem-path
// leakage, so it is safe to surface as the error's details field here.
func (h *runnerHandlers) rejectRunForCredentialFailure(w http.ResponseWriter, r *http.Request, project, id string, err error) {
	revertCtx := context.WithoutCancel(r.Context())
	if _, revertErr := h.svc.UpdateRunnerStatus(revertCtx, project, id, "failed",
		"trigger rejected: project credential unavailable"); revertErr != nil {
		ctxlog.Logger(r.Context()).Error("failed to revert runner status after credential failure",
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

// maxMessageContentSize is the maximum allowed byte length for a human message.
const maxMessageContentSize = 8192

// messageResponse is the response body for the message endpoint.
type messageResponse struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"message_id"`
}

// messageCard handles POST /api/projects/{project}/cards/{id}/message — send a human message.
func (h *runnerHandlers) messageCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can send messages", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), project, id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if card.RunnerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeRunnerNotRunning,
			"card is not currently running",
			fmt.Sprintf("runner_status: %q", card.RunnerStatus))

		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	if body.Content == "" {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "content must not be empty", "")

		return
	}

	if len(body.Content) > maxMessageContentSize {
		writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
			fmt.Sprintf("content exceeds %d bytes", maxMessageContentSize), "")

		return
	}

	messageID := uuid.New().String()
	if err := h.runner.Message(r.Context(), runner.MessagePayload{
		CardID:    id,
		Project:   project,
		MessageID: messageID,
		Content:   body.Content,
	}); err != nil {
		ctxlog.Logger(r.Context()).Error("runner message webhook failed", "card_id", id, "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable, "failed to send message to runner", "")

		return
	}

	writeJSON(w, http.StatusAccepted, messageResponse{OK: true, MessageID: messageID})
}

// promoteCard handles POST /api/projects/{project}/cards/{id}/promote — promote to autonomous.
func (h *runnerHandlers) promoteCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can promote cards", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), project, id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if card.RunnerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeRunnerNotRunning,
			"card is not currently running",
			fmt.Sprintf("runner_status: %q", card.RunnerStatus))

		return
	}

	// Idempotency guard: if the card is already autonomous, skip the outbound webhook.
	// This prevents infinite recursion when a runner that verifies promotion by re-POSTing
	// to this endpoint triggers a second outbound webhook, which the runner would then
	// re-verify again, and so on.
	if card.Autonomous {
		ctxlog.Logger(r.Context()).Debug("promote short-circuit: card already autonomous, skipping runner webhook",
			"card_id", id, "project", project)

		card, err = h.enableFeatureBranchAndPR(r.Context(), project, id, card)
		if err != nil {
			handleServiceError(w, r, err)

			return
		}

		writeJSON(w, http.StatusAccepted, card)

		return
	}

	// Extract agent identity from header. Fall back to "human:api" so the
	// service-layer human-only gate passes when the header is absent (e.g. web UI).
	agentID := extractAgentID(r)
	if agentID == "" {
		agentID = "human:api"
	}

	// Capture the pre-promote feature-branch state so the rollback path below
	// only reverts fields this handler actually changed. If FeatureBranch was
	// already true on entry, enableFeatureBranchAndPR is a no-op and the
	// revert must leave it alone (or it would clobber pre-existing config).
	hadFeatureBranch := card.FeatureBranch

	// Flip the autonomous flag (idempotent; errors on terminal state).
	updatedCard, err := h.svc.PromoteToAutonomous(r.Context(), project, id, agentID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Also ensure feature_branch and create_pr are enabled for autonomous runs.
	updatedCard, err = h.enableFeatureBranchAndPR(r.Context(), project, id, updatedCard)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Send promote webhook to runner.
	if err := h.runner.Promote(r.Context(), runner.PromotePayload{
		CardID:  id,
		Project: project,
	}); err != nil {
		ctxlog.Logger(r.Context()).Error("runner promote webhook failed", "card_id", id, "project", project, "error", err)

		// Revert the autonomous/feature_branch/create_pr changes so the card's
		// declared mode matches the agent's actual mode inside the container.
		// Without this rollback, the card is marked autonomous but the agent
		// is still in HITL mode, which produces a silent contract violation
		// (the runner's /promote handler fail-closes when it can't deliver the
		// canned stdin message, leaving the agent unaware of the promotion).
		//
		// Detached context: callers that timed out / disconnected must not
		// strand the rollback — mirror the runCard revert pattern.
		h.revertPromote(r.Context(), project, id, agentID, hadFeatureBranch)

		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable, "failed to promote runner task", "")

		return
	}

	writeJSON(w, http.StatusAccepted, updatedCard)
}

// revertPromote rolls back the field changes promoteCard made when the runner
// /promote webhook subsequently fails. Mirrors the runCard revert pattern: a
// detached context (context.WithoutCancel) is used so a caller disconnect
// cannot strand the rollback mid-flight. Failure to revert is logged but does
// not change the response to the original caller, who already got 502.
func (h *runnerHandlers) revertPromote(ctx context.Context, project, id, agentID string, hadFeatureBranch bool) {
	revertCtx := context.WithoutCancel(ctx)
	logger := ctxlog.Logger(ctx)

	falseVal := false

	patch := service.PatchCardInput{
		Autonomous: &falseVal,
	}

	// Only revert feature_branch/create_pr if this handler set them; pre-existing
	// values must not be clobbered.
	if !hadFeatureBranch {
		patch.FeatureBranch = &falseVal
		patch.CreatePR = &falseVal
	}

	if _, err := h.svc.PatchCard(revertCtx, project, id, patch); err != nil {
		logger.Error("failed to revert autonomous flag after promote webhook failure",
			"card_id", id, "project", project, "error", err)

		return
	}

	// Record an explicit activity-log entry so operators reconciling a
	// half-promoted card can see the cause without having to grep server logs.
	if _, err := h.svc.AddLogEntry(revertCtx, project, id, board.ActivityEntry{
		Agent:   agentID,
		Action:  "promote-webhook-failed",
		Message: "Reverted autonomous mode: runner /promote webhook failed",
	}); err != nil {
		logger.Error("failed to record promote-webhook-failed activity entry",
			"card_id", id, "project", project, "error", err)
	}
}

// stopCard handles POST /api/projects/{project}/cards/{id}/stop — "Stop".
func (h *runnerHandlers) stopCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can stop runner tasks", "")

		return
	}

	project := r.PathValue("project")
	id := strings.ToUpper(r.PathValue("id"))

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), project, id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if card.RunnerStatus != "queued" && card.RunnerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeRunnerNotRunning,
			"card is not being executed by the runner",
			fmt.Sprintf("runner_status: %q", card.RunnerStatus))

		return
	}

	// Send kill webhook.
	if err := h.runner.Kill(r.Context(), runner.KillPayload{CardID: id, Project: project}); err != nil {
		ctxlog.Logger(r.Context()).Error("runner kill webhook failed", "card_id", id, "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable,
			"failed to stop runner task", "")

		return
	}

	card, err = h.svc.UpdateRunnerStatus(r.Context(), project, id, "killed", "task stopped by user")
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, card)
}

// stopAllResponse is the response for the stop-all endpoint.
//
// FailedToUpdate is a parallel list of card IDs for which the runner kill
// webhook succeeded but the subsequent CM-side UpdateRunnerStatus call
// failed. The runner has stopped the container, but CM's view of the card
// has drifted from reality — callers should treat these as "manual
// reconciliation required". Empty when all updates succeeded.
type stopAllResponse struct {
	AffectedCards  []string `json:"affected_cards"`
	FailedToUpdate []string `json:"failed_to_update,omitempty"`
}

// stopAll handles POST /api/projects/{project}/stop-all — "Stop All".
func (h *runnerHandlers) stopAll(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can stop runner tasks", "")

		return
	}

	project := r.PathValue("project")

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

		return
	}

	// Send stop-all webhook.
	if err := h.runner.StopAll(r.Context(), runner.StopAllPayload{Project: project}); err != nil {
		ctxlog.Logger(r.Context()).Error("runner stop-all webhook failed", "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable,
			"failed to stop all runner tasks", "")

		return
	}

	// Update all active runner cards in this project.
	cards, err := h.svc.ListCards(r.Context(), project, storage.CardFilter{})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	affected := []string{}
	failed := []string{}

	for _, card := range cards {
		if card.RunnerStatus == "queued" || card.RunnerStatus == "running" {
			_, err := h.svc.UpdateRunnerStatus(r.Context(), project, card.ID, "killed", "stopped by stop-all")
			if err != nil {
				// Runner already received the kill webhook above; only CM's view of this
				// card failed to update. Surface the drift in the response so the caller
				// can reconcile rather than silently dropping it from affected_cards.
				ctxlog.Logger(r.Context()).Error("failed to update runner status during stop-all",
					"card_id", card.ID, "project", project, "error", err)

				failed = append(failed, card.ID)

				continue
			}

			affected = append(affected, card.ID)
		}
	}

	// 207 Multi-Status when both partial-success and failures exist; 200 otherwise.
	status := http.StatusOK
	if len(failed) > 0 && len(affected) > 0 {
		status = http.StatusMultiStatus
	}

	writeJSON(w, status, stopAllResponse{AffectedCards: affected, FailedToUpdate: failed})
}

// Callback request bodies are protocol-owned; aliased so handlers keep their local names.
type runnerStatusRequest = protocol.StatusCallbackPayload

// runnerStatusUpdate handles POST /api/runner/status — runner callback.
func (h *runnerHandlers) runnerStatusUpdate(w http.ResponseWriter, r *http.Request) {
	body, ok := h.authenticateRunnerPost(w, r)
	if !ok {
		return
	}

	var req runnerStatusRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON", "")

		return
	}

	// Validate that the callback only sets runner-allowed statuses.
	v := board.NewValidator()
	if err := v.ValidateRunnerCallbackStatus(req.RunnerStatus); err != nil {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError,
			"invalid runner callback status", err.Error())

		return
	}

	card, err := h.svc.UpdateRunnerStatus(r.Context(), req.Project, strings.ToUpper(req.CardID), req.RunnerStatus, req.Message)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// cardAutonomousResponse is the minimal read-only shape returned to the
// runner's VerifyAutonomous call. Deliberately narrow — only the boolean
// is needed, and a runner-facing endpoint must not leak unrelated card
// fields.
type cardAutonomousResponse struct {
	Autonomous bool `json:"autonomous"`
}

// getCardAutonomous handles GET /api/v1/cards/{project}/{id}/autonomous.
// The runner calls this during /promote to fail-closed confirm the card's
// autonomous flag before writing the canned stdin message.
func (h *runnerHandlers) getCardAutonomous(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRunnerGet(w, r) {
		return
	}

	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), projectName, strings.ToUpper(cardID))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, cardAutonomousResponse{Autonomous: card.Autonomous})
}

// getTaskSkillsSource serves GET /api/<backend>/task-skills-source — the agent
// backend fetches this {git_remote_url, ref} pointer and clones the task-skills
// repo itself. Signed-GET like getCardAutonomous.
func (h *runnerHandlers) getTaskSkillsSource(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRunnerGet(w, r) {
		return
	}

	url, ref := taskSkillsSource(h.taskSkillsDir, h.taskSkillsGitRemoteURL)
	token, tokenExpiresAt := mintInstanceToken(r.Context(), h.instanceTokenProvider)

	writeJSON(w, http.StatusOK, taskSkillsSourceResponse{
		GitRemoteURL: url, Ref: ref, Token: token, TokenExpiresAt: tokenExpiresAt,
	})
}

// mintInstanceToken best-effort mints an instance-scoped git token for a
// task-skills-source response. See the asymmetry comment on
// taskSkillsSourceResponse: unlike getGitCredentials (fail-closed on a broken
// project binding), this never fails the request — a nil provider or a mint
// error just returns empty strings, and the caller falls back to its own
// configured credential during the compat window.
//
// err is only ever a githubauth provider error (JWT/HTTP-status class
// messages, never the token itself), so logging "error", err here is safe —
// mirrors the class-only logging already used for provider errors elsewhere
// in this file.
func mintInstanceToken(ctx context.Context, provider githubauth.TokenGenerator) (token, expiresAt string) {
	if provider == nil {
		return "", ""
	}

	tok, exp, err := provider.GenerateToken(ctx)
	if err != nil {
		ctxlog.Logger(ctx).Warn("failed to mint instance token for task-skills-source; continuing without it",
			"error", err)

		return "", ""
	}

	return tok, tokenExpiryString(exp)
}

// tokenExpiryString formats a minted token's expiry for the wire. Zero and
// far-future sentinel expiries (githubauth's PATProvider reports year 9999 —
// a PAT has no server-managed TTL) are omitted entirely: an absent expiry
// means "do not schedule a refresh", which is exactly the PAT semantic.
func tokenExpiryString(t time.Time) string {
	if t.IsZero() || t.Year() >= 9000 {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

// chatBackendHandlers serves the HMAC-signed callbacks the dedicated chat
// service makes back to CM. Today that is only the task-skills git pointer; the
// chat worker clones it and exposes the skills via its Skill tool. It closes
// over the chat backend's HMAC key + its own replay cache so it verifies
// independently of the runner/agent task backend.
type chatBackendHandlers struct {
	apiKey                 string
	replayCache            *runner.SignatureCache
	taskSkillsDir          string
	taskSkillsGitRemoteURL string

	// instanceTokenProvider mirrors runnerHandlers.instanceTokenProvider —
	// same instance-scoped, best-effort mint for the chat variant's
	// task-skills-source response.
	instanceTokenProvider githubauth.TokenGenerator
}

// getTaskSkillsSource serves GET /api/chat/task-skills-source — the chat service
// fetches this {git_remote_url, ref} pointer and clones the task-skills repo
// itself, mirroring the agent backend. CM stays the single source of truth.
func (h *chatBackendHandlers) getTaskSkillsSource(w http.ResponseWriter, r *http.Request) {
	if !authenticateBackendGet(w, r, h.apiKey, h.replayCache) {
		return
	}

	url, ref := taskSkillsSource(h.taskSkillsDir, h.taskSkillsGitRemoteURL)
	token, tokenExpiresAt := mintInstanceToken(r.Context(), h.instanceTokenProvider)

	writeJSON(w, http.StatusOK, taskSkillsSourceResponse{
		GitRemoteURL: url, Ref: ref, Token: token, TokenExpiresAt: tokenExpiresAt,
	})
}

// getGitCredentials handles GET /api/<backend>/git-credentials — re-mints the
// project-scoped git token for a running card. Long runs outlive ~1h GitHub
// App installation tokens, so the backend calls this mid-run to refresh.
// HMAC-signed like every backend callback.
//
// Fail-closed on the project binding, mirroring rejectRunForCredentialFailure:
// a broken/unresolvable providerForProject NEVER falls back to the instance
// credential — unlike task-skills-source (mintInstanceToken), which is
// deliberately best-effort because it has no binding to be wrong about.
func (h *runnerHandlers) getGitCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRunnerGet(w, r) {
		return
	}

	project := r.URL.Query().Get("project")
	cardID := r.URL.Query().Get("card_id")

	if project == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card_id required", "")

		return
	}

	// No free token faucet: the card must exist and be actively running.
	card, err := h.svc.GetCard(r.Context(), project, strings.ToUpper(cardID))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if card.RunnerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "card is not running", "")

		return
	}

	if h.providerForProject == nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "project credential unavailable", "")

		return
	}

	provider, _, err := h.providerForProject(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "project credential unavailable", sanitizeErrorDetails(err))

		return
	}

	token, expiresAt, err := provider.GenerateToken(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, ErrCodeInternalError, "token mint failed", sanitizeErrorDetails(err))

		return
	}

	resp := map[string]string{"token": token}
	if s := tokenExpiryString(expiresAt); s != "" {
		resp["expires_at"] = s
	}

	writeJSON(w, http.StatusOK, resp)
}

// extractBackendSignature performs shared header validation for backend HMAC
// authentication (runner, agent, or chat). It checks that an API key is
// configured and that the X-Signature-256 / X-Webhook-Timestamp headers are
// present and well-formed, returning the trimmed signature hex and timestamp on
// success. On failure it writes the 403 response and returns ok=false.
//
// Body reading and the actual HMAC verification are left to the caller so GET
// (no body) and POST (body in the signed payload) handlers can share this
// prefix without duplicating the differing tails.
func extractBackendSignature(w http.ResponseWriter, r *http.Request, apiKey string) (sig, ts string, ok bool) {
	if apiKey == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "backend authentication not configured", "")

		return "", "", false
	}

	sigHeader := r.Header.Get(protocol.SignatureHeader)
	if sigHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Signature-256 header", "")

		return "", "", false
	}

	tsHeader := r.Header.Get(protocol.TimestampHeader)
	if tsHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Webhook-Timestamp header", "")

		return "", "", false
	}

	if !strings.HasPrefix(sigHeader, "sha256=") {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "malformed X-Signature-256 header: missing sha256= prefix", "")

		return "", "", false
	}

	return strings.TrimPrefix(sigHeader, "sha256="), tsHeader, true
}

// authenticateBackendGet verifies an HMAC-SHA256 signature over method + uri
// with an empty body on a backend-originated GET, using the supplied key and
// replay cache. Shared by the runner/agent and chat callback handlers. Returns
// true on success; on failure it writes the 403 response and returns false.
func authenticateBackendGet(w http.ResponseWriter, r *http.Request, apiKey string, replayCache protocol.ReplayCache) bool {
	sig, ts, ok := extractBackendSignature(w, r, apiKey)
	if !ok {
		return false
	}

	if !protocol.VerifySignatureWithTimestamp(apiKey, r.Method, r.URL.RequestURI(), sig, ts, nil, protocol.DefaultMaxClockSkew, replayCache) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return false
	}

	return true
}

// extractRunnerSignature is the runner/agent backend's signature-header check,
// keyed by its configured backend HMAC key.
func (h *runnerHandlers) extractRunnerSignature(w http.ResponseWriter, r *http.Request) (sig, ts string, ok bool) {
	return extractBackendSignature(w, r, h.backendCfg.APIKey)
}

// authenticateRunnerGet verifies an HMAC-SHA256 signature over
// `timestamp + "." + ""` (empty body) on a runner-originated GET. Returns
// true on success; on failure it writes the 403 response and returns false.
func (h *runnerHandlers) authenticateRunnerGet(w http.ResponseWriter, r *http.Request) bool {
	return authenticateBackendGet(w, r, h.backendCfg.APIKey, h.replayCache)
}

// authenticateRunnerPost reads the request body, verifies the HMAC-SHA256
// signature over method+path+body. Returns (body, true) on success; on
// failure it writes the 403/400 response and returns (nil, false).
func (h *runnerHandlers) authenticateRunnerPost(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	sig, tsHeader, ok := h.extractRunnerSignature(w, r)
	if !ok {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "failed to read request body", "")

		return nil, false
	}

	if !protocol.VerifySignatureWithTimestamp(h.backendCfg.APIKey, r.Method, r.URL.RequestURI(), sig, tsHeader, body, protocol.DefaultMaxClockSkew, h.replayCache) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return nil, false
	}

	return body, true
}

// runnerHealthResponse is the wire shape for GET /api/runner/health.
type runnerHealthResponse struct {
	OK                bool `json:"ok"`
	RunningContainers int  `json:"running_containers"`
	MaxConcurrent     int  `json:"max_concurrent"`
}

// getRunnerHealth handles GET /api/runner/health by proxying to the runner's
// /health endpoint and returning the parsed shape. The UI reads max_concurrent
// from here to render the NowRail capacity meter — it's the runner-global cap,
// not a per-project value. Returns 503 when no task backend is configured and 502
// when the runner is unreachable; callers should fail soft (hide capacity).
//
// Probe results are cached for runnerHealthCacheTTL so concurrent tabs and
// rapid refreshes don't tie up the runner with redundant probes, and a runner
// outage doesn't make every browser request block for the full timeout.
// Upstream errors are sanitized (raw err details never leave the server)
// to match how every other runner endpoint in this file handles them.
func (h *runnerHandlers) getRunnerHealth(w http.ResponseWriter, r *http.Request) {
	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled, "runner is not configured", "")

		return
	}

	info, err := h.healthCache.get(r.Context(), h.runner)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("runner health probe failed", "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable, "runner health probe failed", "")

		return
	}

	writeJSON(w, http.StatusOK, runnerHealthResponse{
		OK:                info.OK,
		RunningContainers: info.RunningContainers,
		MaxConcurrent:     info.MaxConcurrent,
	})
}

// get returns a (possibly cached) runner /health response. The cache TTL
// (runnerHealthCacheTTL) is short so operator changes propagate quickly,
// but covers both success and failure so a runner outage cannot cause
// every concurrent caller to issue a fresh probe.
//
// On a cold or expired cache, concurrent callers are coalesced via
// singleflight — exactly one upstream probe runs per TTL window. The
// probe runs against a detached context (`context.WithoutCancel`) so a
// single caller's cancellation (browser tab closed mid-probe) cannot
// write a transient `context.Canceled` into the cache and poison every
// other caller's read for the rest of the TTL window.
//
// `ctx` is only consulted to abandon the wait when the caller goes
// away; the in-flight probe continues so the result still lands in
// the cache for the next caller.
func (c *healthProbeCache) get(ctx context.Context, client TaskBackend) (runner.HealthInfo, error) {
	c.mu.Lock()
	if time.Now().Before(c.expires) {
		info, err := c.info, c.err
		c.mu.Unlock()

		return info, err
	}
	c.mu.Unlock()

	ch := c.flight.DoChan("probe", func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runnerHealthProbeTimeout)
		defer cancel()

		info, err := client.Health(probeCtx)

		c.mu.Lock()
		c.info = info
		c.err = err
		c.expires = time.Now().Add(runnerHealthCacheTTL)
		c.mu.Unlock()

		return info, err
	})

	select {
	case res := <-ch:
		info, _ := res.Val.(runner.HealthInfo)

		return info, res.Err
	case <-ctx.Done():
		// Caller went away; let the singleflight probe keep running so
		// the next caller benefits from the cached result.
		return runner.HealthInfo{}, fmt.Errorf("runner health probe: %w", ctx.Err())
	}
}

// isRemoteExecutionEnabled checks if remote execution is enabled for the given project,
// falling back to whether a task backend is configured when not set per-project.
func (h *runnerHandlers) isRemoteExecutionEnabled(r *http.Request, project string) bool {
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		return h.runner != nil
	}

	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.Enabled != nil {
		return *projectCfg.RemoteExecution.Enabled
	}

	return h.runner != nil
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
