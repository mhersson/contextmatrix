package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
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

// runnerHandlers contains handlers for remote execution endpoints.
type runnerHandlers struct {
	svc               *service.CardService
	runner            *runner.Client // nil when runner is disabled
	runnerCfg         config.RunnerConfig
	mcpAPIKey         string
	port              int
	sessionManager    *sessionlog.Manager       // nil when session manager is not configured
	keepaliveInterval time.Duration             // zero → use default (30s)
	runnerEventBuf    *events.RunnerEventBuffer // SSE bus for HITL chat / promotion fan-out
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
	if !card.FeatureBranch {
		fb := true

		pr := true
		if _, patchErr := h.svc.PatchCard(r.Context(), project, id, service.PatchCardInput{
			FeatureBranch: &fb,
			CreatePR:      &pr,
		}); patchErr != nil {
			handleServiceError(w, r, patchErr)

			return
		}
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

	// Resolve task skills: card.Skills > project.DefaultSkills > nil (mount full set).
	var taskSkills *[]string

	switch {
	case card.Skills != nil:
		taskSkills = card.Skills
	case projectCfg.DefaultSkills != nil:
		taskSkills = projectCfg.DefaultSkills
	}

	payload := runner.TriggerPayload{
		CardID:      id,
		Project:     project,
		RepoURL:     projectCfg.Repo,
		MCPAPIKey:   h.mcpAPIKey,
		BaseBranch:  card.BaseBranch,
		Interactive: runBody.Interactive,
		TaskSkills:  taskSkills,
	}
	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.RunnerImage != "" {
		payload.RunnerImage = projectCfg.RemoteExecution.RunnerImage
	}

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

	// Fan out to the SSE bus — this is the orchestrated runner's chat
	// delivery mechanism. The legacy /message stdin webhook is gone;
	// orchestrated workers consume chat input from the SSE stream.
	if h.runnerEventBuf != nil {
		h.runnerEventBuf.Append(id, events.RunnerEvent{
			Type: "chat_input",
			Data: body.Content,
		})
	}

	// Publish to the session log so /api/runner/logs SSE consumers
	// (web UI transcript, integration test transcript) see the human
	// side of the conversation alongside agent events. The frontend's
	// LogEntry type uses "user" for human-typed messages — the chat
	// component renders these as right-aligned bubbles. Publishing as
	// any other type (e.g. "user_chat") would render them with the
	// generic agent-output styling instead.
	if h.sessionManager != nil {
		h.sessionManager.PublishLocal(id, sessionlog.Event{
			Timestamp: time.Now(),
			Type:      "user",
			Payload:   []byte(body.Content),
		})
	}

	writeJSON(w, http.StatusAccepted, messageResponse{OK: true, MessageID: messageID})
}

// promoteCard handles POST /api/projects/{project}/cards/{id}/promote — promote to autonomous.
func (h *runnerHandlers) promoteCard(w http.ResponseWriter, r *http.Request) {
	// Require an explicit human-prefixed X-Agent-ID. Synthesising a
	// "human:api" fallback would let any caller with tunnel access flip
	// any non-terminal card autonomous, defeating the documented
	// human-only gate (CLAUDE.md rule 13). The web UI sets this header
	// from the user's stored agent ID — see useAgentId.
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "X-Agent-ID header is required", "")

		return
	}

	if !strings.HasPrefix(agentID, "human:") {
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

		fbTrue := true

		prTrue := true
		if !card.FeatureBranch || !card.CreatePR {
			card, err = h.svc.PatchCard(r.Context(), project, id, service.PatchCardInput{
				FeatureBranch: &fbTrue,
				CreatePR:      &prTrue,
			})
			if err != nil {
				handleServiceError(w, r, err)

				return
			}
		}

		writeJSON(w, http.StatusAccepted, card)

		return
	}

	// agentID was validated at the top of the handler; a missing or
	// non-human-prefixed value short-circuits before this point.
	// Flip the autonomous flag (idempotent; errors on terminal state).
	updatedCard, err := h.svc.PromoteToAutonomous(r.Context(), project, id, agentID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Also ensure feature_branch and create_pr are enabled for autonomous runs.
	fbTrue := true

	prTrue := true
	if !updatedCard.FeatureBranch || !updatedCard.CreatePR {
		updatedCard, err = h.svc.PatchCard(r.Context(), project, id, service.PatchCardInput{
			FeatureBranch: &fbTrue,
			CreatePR:      &prTrue,
		})
		if err != nil {
			handleServiceError(w, r, err)

			return
		}
	}

	// Fan out to the SSE bus — this is the orchestrated runner's promotion
	// delivery mechanism. The legacy /promote HTTP webhook below is a
	// best-effort interactive-stdin relay for any worker still listening
	// on it; failure is logged but does not fail the request, mirroring
	// the /message endpoint. The autonomous flag is server-authoritative
	// and has already committed above.
	if h.runnerEventBuf != nil {
		h.runnerEventBuf.Append(id, events.RunnerEvent{
			Type: "promotion",
			Data: "{}",
		})
	}

	// Mirror the messageCard hook: surface the promotion in the
	// session log so transcript consumers can see when the human flips
	// the card to autonomous mid-run. Use "system" — the frontend
	// renders system-typed entries with a green accent bar; this is
	// not a user-typed chat message so "user" would mis-style it as a
	// right-aligned bubble. Phrasing matches the activity-log entry
	// produced by CardService.PromoteToAutonomous.
	if h.sessionManager != nil {
		h.sessionManager.PublishLocal(id, sessionlog.Event{
			Timestamp: time.Now(),
			Type:      "system",
			Payload:   []byte("Promoted to autonomous mode"),
		})
	}

	// The legacy /promote stdin webhook is gone; the orchestrated
	// runner observes promotion via the RunnerEventBuffer SSE fan-out
	// above and the autonomous flag is server-authoritative.

	writeJSON(w, http.StatusAccepted, updatedCard)
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

	for _, card := range cards {
		if card.RunnerStatus == "queued" || card.RunnerStatus == "running" {
			_, err := h.svc.UpdateRunnerStatus(r.Context(), project, card.ID, "killed", "stopped by stop-all")
			if err != nil {
				ctxlog.Logger(r.Context()).Error("failed to update runner status during stop-all",
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

	if !strings.HasPrefix(sigHeader, "sha256=") {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "malformed X-Signature-256 header: missing sha256= prefix", "")

		return
	}

	sig := strings.TrimPrefix(sigHeader, "sha256=")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "failed to read request body", "")

		return
	}

	if !runner.VerifySignatureWithTimestamp(h.runnerCfg.APIKey, r.Method, r.URL.Path, sig, tsHeader, body, runner.DefaultMaxClockSkew) {
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

// authenticateRunnerGet verifies an HMAC-SHA256 signature over
// `timestamp + "." + ""` (empty body) on a runner-originated GET. Returns
// true on success; on failure it writes the 403 response and returns false.
func (h *runnerHandlers) authenticateRunnerGet(w http.ResponseWriter, r *http.Request) bool {
	if h.runnerCfg.APIKey == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "runner authentication not configured", "")

		return false
	}

	sigHeader := r.Header.Get("X-Signature-256")
	if sigHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Signature-256 header", "")

		return false
	}

	tsHeader := r.Header.Get("X-Webhook-Timestamp")
	if tsHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Webhook-Timestamp header", "")

		return false
	}

	if !strings.HasPrefix(sigHeader, "sha256=") {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "malformed X-Signature-256 header: missing sha256= prefix", "")

		return false
	}

	sig := strings.TrimPrefix(sigHeader, "sha256=")

	if !runner.VerifySignatureWithTimestamp(h.runnerCfg.APIKey, r.Method, r.URL.Path, sig, tsHeader, nil, runner.DefaultMaxClockSkew) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return false
	}

	return true
}

// skillEngagedRequest is the JSON body sent by the runner when the agent
// invokes the Skill tool.
type skillEngagedRequest struct {
	CardID    string `json:"card_id"`
	Project   string `json:"project"`
	SkillName string `json:"skill_name"`
}

// handleRunnerSkillEngaged handles POST /api/runner/skill-engaged — runner
// callback notifying CM that the agent has engaged a named skill.
func (h *runnerHandlers) handleRunnerSkillEngaged(w http.ResponseWriter, r *http.Request) {
	body, ok := h.authenticateRunnerPost(w, r)
	if !ok {
		return
	}

	var req skillEngagedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON", "")

		return
	}

	if req.CardID == "" || req.Project == "" || req.SkillName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "card_id, project, skill_name required", "")

		return
	}

	if err := h.svc.RecordSkillEngaged(r.Context(), req.Project, strings.ToUpper(req.CardID), req.SkillName); err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// authenticateRunnerPost reads the request body, verifies the HMAC-SHA256
// signature over method+path+body. Returns (body, true) on success; on
// failure it writes the 403/400 response and returns (nil, false).
func (h *runnerHandlers) authenticateRunnerPost(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if h.runnerCfg.APIKey == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "runner authentication not configured", "")

		return nil, false
	}

	sigHeader := r.Header.Get("X-Signature-256")
	if sigHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Signature-256 header", "")

		return nil, false
	}

	tsHeader := r.Header.Get("X-Webhook-Timestamp")
	if tsHeader == "" {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "missing X-Webhook-Timestamp header", "")

		return nil, false
	}

	if !strings.HasPrefix(sigHeader, "sha256=") {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "malformed X-Signature-256 header: missing sha256= prefix", "")

		return nil, false
	}

	sig := strings.TrimPrefix(sigHeader, "sha256=")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "failed to read request body", "")

		return nil, false
	}

	if !runner.VerifySignatureWithTimestamp(h.runnerCfg.APIKey, r.Method, r.URL.Path, sig, tsHeader, body, runner.DefaultMaxClockSkew) {
		writeError(w, http.StatusForbidden, ErrCodeInvalidSignature, "invalid HMAC signature or expired timestamp", "")

		return nil, false
	}

	return body, true
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
