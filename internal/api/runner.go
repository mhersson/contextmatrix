package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Error codes for runner-related errors.
const (
	ErrCodeRunnerDisabled   = "RUNNER_DISABLED"
	ErrCodeRunnerError      = "RUNNER_ERROR"
	ErrCodeRunnerNotRunning = "RUNNER_NOT_RUNNING"
)

// runnerHandlers contains handlers for remote execution endpoints.
type runnerHandlers struct {
	svc       *service.CardService
	runner    *runner.Client // nil when runner is disabled
	runnerCfg config.RunnerConfig
	mcpAPIKey string
	port      int
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
		handleServiceError(w, err)
		return
	}

	// Validate card is eligible for remote execution.
	if !card.Autonomous {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError,
			"card must have autonomous mode enabled", "")
		return
	}
	if card.State != board.StateTodo {
		writeError(w, http.StatusConflict, ErrCodeInvalidTransition,
			"card must be in todo state to run", fmt.Sprintf("current state: %s", card.State))
		return
	}
	if card.RunnerStatus == "queued" || card.RunnerStatus == "running" {
		writeError(w, http.StatusConflict, ErrCodeRunnerError,
			"card is already being executed by the runner", fmt.Sprintf("runner_status: %s", card.RunnerStatus))
		return
	}

	// Check per-project remote execution setting.
	if !h.isRemoteExecutionEnabled(r, project) {
		writeError(w, http.StatusForbidden, ErrCodeRunnerDisabled,
			"remote execution is disabled for this project", "")
		return
	}

	// Auto-enable feature_branch and create_pr for runner-executed cards.
	// Changes inside a disposable container are lost without a remote branch.
	if !card.FeatureBranch {
		fb := true
		pr := true
		if _, patchErr := h.svc.PatchCard(r.Context(), project, id, service.PatchCardInput{
			FeatureBranch: &fb,
			CreatePR:      &pr,
		}); patchErr != nil {
			handleServiceError(w, patchErr)
			return
		}
	}

	// Get project config to retrieve repo URL and runner image.
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	// Set runner_status to queued.
	card, err = h.svc.UpdateRunnerStatus(r.Context(), project, id, "queued", "task queued for runner")
	if err != nil {
		handleServiceError(w, err)
		return
	}

	// Build trigger payload.
	mcpURL := fmt.Sprintf("%s/mcp", h.runnerCfg.PublicURL)
	payload := runner.TriggerPayload{
		CardID:    id,
		Project:   project,
		RepoURL:   projectCfg.Repo,
		MCPURL:    mcpURL,
		MCPAPIKey: h.mcpAPIKey,
	}
	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.RunnerImage != "" {
		payload.RunnerImage = projectCfg.RemoteExecution.RunnerImage
	}

	// Send trigger webhook.
	if err := h.runner.Trigger(r.Context(), payload); err != nil {
		// Webhook failed — revert status to failed.
		if _, revertErr := h.svc.UpdateRunnerStatus(r.Context(), project, id, "failed",
			fmt.Sprintf("webhook failed: %v", err)); revertErr != nil {
			slog.Error("failed to revert runner status after webhook failure",
				"card_id", id, "project", project, "error", revertErr)
		}
		writeError(w, http.StatusBadGateway, ErrCodeRunnerError,
			"failed to trigger runner", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, card)
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
		handleServiceError(w, err)
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
		writeError(w, http.StatusBadGateway, ErrCodeRunnerError,
			"failed to stop runner task", err.Error())
		return
	}

	card, err = h.svc.UpdateRunnerStatus(r.Context(), project, id, "killed", "task stopped by user")
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// stopAllResponse is the response for the stop-all endpoint.
type stopAllResponse struct {
	AffectedCards []string `json:"affected_cards"`
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
		writeError(w, http.StatusBadGateway, ErrCodeRunnerError,
			"failed to stop all runner tasks", err.Error())
		return
	}

	// Update all active runner cards in this project.
	cards, err := h.svc.ListCards(r.Context(), project, storage.CardFilter{})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	affected := []string{}
	for _, card := range cards {
		if card.RunnerStatus == "queued" || card.RunnerStatus == "running" {
			_, err := h.svc.UpdateRunnerStatus(r.Context(), project, card.ID, "killed", "stopped by stop-all")
			if err != nil {
				slog.Error("failed to update runner status during stop-all",
					"card_id", card.ID, "project", project, "error", err)
				continue
			}
			affected = append(affected, card.ID)
		}
	}

	writeJSON(w, http.StatusOK, stopAllResponse{AffectedCards: affected})
}

// runnerStatusRequest is the JSON body for runner status callbacks.
type runnerStatusRequest struct {
	CardID       string `json:"card_id"`
	Project      string `json:"project"`
	RunnerStatus string `json:"runner_status"`
	Message      string `json:"message,omitempty"`
}

// runnerStatusUpdate handles POST /api/runner/status — runner callback.
func (h *runnerHandlers) runnerStatusUpdate(w http.ResponseWriter, r *http.Request) {
	// Always require HMAC authentication on this endpoint.
	if h.runnerCfg.APIKey == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "runner authentication not configured", "")
		return
	}

	sigHeader := r.Header.Get("X-Signature-256")
	if sigHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Signature-256 header", "")
		return
	}

	tsHeader := r.Header.Get("X-Webhook-Timestamp")
	if tsHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Webhook-Timestamp header", "")
		return
	}

	sig := strings.TrimPrefix(sigHeader, "sha256=")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "failed to read request body", "")
		return
	}

	if !runner.VerifySignatureWithTimestamp(h.runnerCfg.APIKey, sig, tsHeader, body, runner.DefaultMaxClockSkew) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")
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
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// isRemoteExecutionEnabled checks if remote execution is enabled for the given project,
// falling back to the global runner config if not set per-project.
func (h *runnerHandlers) isRemoteExecutionEnabled(r *http.Request, project string) bool {
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		return h.runnerCfg.Enabled
	}
	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.Enabled != nil {
		return *projectCfg.RemoteExecution.Enabled
	}
	return h.runnerCfg.Enabled
}
