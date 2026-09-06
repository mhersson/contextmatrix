package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// launchOptions carries the per-launch inputs the HTTP handler and the
// playbook runner supply differently: the handler may ask for a HITL run,
// the runner asks the backend to create the playbook base branch.
type launchOptions struct {
	interactive      bool
	createBaseBranch bool
	baseBranchFrom   string
}

// launchFailure is a refusal with the HTTP shape the handlers write and the
// text the playbook runner records as a run's waiting reason. Service errors
// (unknown card, store failures) are returned as-is, not wrapped in this.
type launchFailure struct {
	status  int
	code    string
	message string
	details string
}

func (f *launchFailure) Error() string {
	if f.details == "" {
		return f.message
	}

	return f.message + ": " + f.details
}

func launchRefused(status int, code, message, details string) *launchFailure {
	return &launchFailure{status: status, code: code, message: message, details: details}
}

// writeLaunchError writes a launch or stop error: typed refusals keep their
// status and code, everything else goes through the service error mapping.
func writeLaunchError(w http.ResponseWriter, r *http.Request, err error) {
	var lf *launchFailure
	if errors.As(err, &lf) {
		writeError(w, lf.status, lf.code, lf.message, lf.details)

		return
	}

	handleServiceError(w, r, err)
}

// launch is the one trigger path: it validates the card, marks it queued,
// builds the trigger payload and sends it, reverting to failed when the
// credential or the webhook fails. Both the run endpoint and the playbook
// runner call it; neither builds a payload of its own.
func (h *backendHandlers) launch(ctx context.Context, project, id string, opts launchOptions) (*board.Card, error) {
	if h.backend == nil {
		return nil, launchRefused(http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")
	}

	card, err := h.svc.GetCard(ctx, project, id)
	if err != nil {
		return nil, err
	}

	if card.State != board.StateTodo {
		return nil, launchRefused(http.StatusConflict, ErrCodeInvalidTransition,
			"card must be in todo state to run", fmt.Sprintf("current state: %s", card.State))
	}

	if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
		return nil, launchRefused(http.StatusConflict, ErrCodeWorkerConflict,
			"card is already being executed by a worker", fmt.Sprintf("worker_status: %s", card.WorkerStatus))
	}

	// Get project config to retrieve repo URL and worker image.
	projectCfg, err := h.svc.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}

	card, err = h.svc.UpdateWorkerStatus(ctx, project, id, "queued", "task queued for worker")
	if err != nil {
		return nil, err
	}

	// Build trigger payload. Model is the backend's default_model - per-card
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
	// so force it off for autonomous cards - CM owns this invariant server-side
	// rather than trusting the client flag (defense in depth: a stray trigger
	// cannot push an autonomous card down the HITL path).
	interactive := opts.interactive && !card.Autonomous

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

	// Clamp Best-of-N against the configured max - the stored card value can
	// exceed it if max_candidates was lowered after the card was set, since
	// the REST PATCH/PUT validation only checks the max in effect at write
	// time.
	if card.BestOfN >= 2 {
		payload.BestOfN = min(card.BestOfN, h.bestOfN.MaxCandidates)
	}

	// MaxCapability: copy the card-level flag so the agent backend can
	// honour it (ignores cost, picks most capable in the tier).
	payload.MaxCapability = card.MaxCapability

	h.attachMob(ctx, &payload, card, project, id)

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
			// but it is logged - a silent miss would let the agent re-select a
			// known-incapable model with no trace.
			var blErr error
			if bl, blErr = h.blacklist.BlacklistedSlugs(ctx); blErr != nil {
				ctxlog.Logger(ctx).Warn("failed to read model blacklist; proceeding without it",
					"card_id", id, "project", project, "error", blErr)
			}
		}

		// Clone: the catalog returns its shared cached slice; a defensive
		// copy keeps the payload independent of the cache's backing array.
		payload.Selection = &protocol.SelectionContext{
			Candidates: slices.Clone(h.catalog.Candidates(ctx)),
			Favorites:  mergeFavorites(h.backendCfg.Favorites, projectCfg.Favorites),
			Blacklist:  bl,
		}
	}

	payload.CreateBaseBranch = opts.createBaseBranch
	payload.BaseBranchFrom = opts.baseBranchFrom

	// Mint the project-scoped git token. Fail closed: a broken binding
	// rejects the run - never the instance credential by accident.
	if h.providerForProject != nil {
		provider, _, providerErr := h.providerForProject(ctx, project)
		if providerErr != nil {
			return nil, h.rejectLaunchForCredentialFailure(ctx, project, id, providerErr)
		}

		token, expiresAt, tokenErr := provider.GenerateToken(ctx)
		if tokenErr != nil {
			return nil, h.rejectLaunchForCredentialFailure(ctx, project, id, tokenErr)
		}

		payload.GitToken = token
		payload.GitTokenExpiresAt = tokenExpiryString(expiresAt)
	}

	payload.LLMEndpoint = h.llmEndpoint

	if err := h.backend.Trigger(ctx, payload); err != nil {
		ctxlog.Logger(ctx).Error("backend webhook failed", "card_id", id, "project", project, "error", err)
		// Webhook failed - revert status to failed.
		// Use context.WithoutCancel so the revert succeeds even when the HTTP client
		// has already disconnected and r.Context() is cancelled.
		revertCtx := context.WithoutCancel(ctx)
		if _, revertErr := h.svc.UpdateWorkerStatus(revertCtx, project, id, "failed",
			"webhook trigger failed"); revertErr != nil {
			ctxlog.Logger(ctx).Error("failed to revert worker_status after webhook failure",
				"card_id", id, "project", project, "error", revertErr)
		}

		return nil, launchRefused(http.StatusBadGateway, ErrCodeBackendUnavailable, "failed to trigger backend task", "")
	}

	return card, nil
}

// rejectLaunchForCredentialFailure writes the fail-closed 409 refusal for a
// broken or unresolvable project git-token provider (either providerForProject
// itself failed, or the resolved provider's GenerateToken call did).
//
// launch has already set worker_status to "queued" by this point, so the
// rejection first reverts it to "failed" - mirroring the webhook-failure
// revert above (context.WithoutCancel so a client disconnect cannot strand
// the rollback). Without the revert, the already-queued guard at the top of
// launch would 409 every future trigger of this card until a manual stop.
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
func (h *backendHandlers) rejectLaunchForCredentialFailure(ctx context.Context, project, id string, err error) *launchFailure {
	revertCtx := context.WithoutCancel(ctx)
	if _, revertErr := h.svc.UpdateWorkerStatus(revertCtx, project, id, "failed",
		"trigger rejected: project credential unavailable"); revertErr != nil {
		ctxlog.Logger(ctx).Error("failed to revert worker_status after credential failure",
			"card_id", id, "project", project, "error", revertErr)
	}

	if _, logErr := h.svc.AddLogEntry(revertCtx, project, id, board.ActivityEntry{
		Agent:   "system",
		Action:  "run-rejected",
		Message: fmt.Sprintf("run rejected: project credential unavailable (%s)", project),
	}); logErr != nil {
		ctxlog.Logger(ctx).Error("failed to record run-rejected activity entry",
			"card_id", id, "project", project, "error", logErr)
	}

	return launchRefused(http.StatusConflict, ErrCodeValidationError, "project credential unavailable", sanitizeErrorDetails(err))
}

// stop is the one kill path: refuses a card another instance owns or one
// with no worker in flight, sends the kill webhook, then marks the card
// killed. The webhook fires before the status write so a failed webhook
// leaves worker_status untouched.
func (h *backendHandlers) stop(ctx context.Context, project, id string) (*board.Card, error) {
	if h.backend == nil {
		return nil, launchRefused(http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")
	}

	card, err := h.svc.GetCard(ctx, project, id)
	if err != nil {
		return nil, err
	}

	// A worker another instance started reports to that instance. Killing it
	// from here would leave the two boards disagreeing about the run.
	if h.svc.ClaimedElsewhere(card) {
		return nil, launchRefused(http.StatusForbidden, ErrCodeAgentMismatch,
			"card is running on another instance", "claimed via instance "+card.ClaimedVia)
	}

	if card.WorkerStatus != "queued" && card.WorkerStatus != "running" {
		return nil, launchRefused(http.StatusConflict, ErrCodeWorkerNotRunning,
			"card is not being executed by a worker", fmt.Sprintf("worker_status: %q", card.WorkerStatus))
	}

	if err := h.backend.Kill(ctx, backend.KillPayload{CardID: id, Project: project}); err != nil {
		ctxlog.Logger(ctx).Error("backend kill webhook failed", "card_id", id, "project", project, "error", err)

		return nil, launchRefused(http.StatusBadGateway, ErrCodeBackendUnavailable, "failed to stop backend task", "")
	}

	return h.svc.UpdateWorkerStatus(ctx, project, id, "killed", "task stopped by user")
}
