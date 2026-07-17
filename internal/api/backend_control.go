package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// maxMessageContentSize is the maximum allowed byte length for a human message.
const maxMessageContentSize = 8192

// messageResponse is the response body for the message endpoint.
type messageResponse struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"message_id"`
}

// messageCard handles POST /api/projects/{project}/cards/{id}/message - send a human message.
func (h *backendHandlers) messageCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can send messages", "")

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

	if card.WorkerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeWorkerNotRunning,
			"card is not currently running",
			fmt.Sprintf("worker_status: %q", card.WorkerStatus))

		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
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
	if err := h.backend.Message(r.Context(), backend.MessagePayload{
		CardID:    id,
		Project:   project,
		MessageID: messageID,
		Content:   body.Content,
	}); err != nil {
		ctxlog.Logger(r.Context()).Error("backend message webhook failed", "card_id", id, "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "failed to send message to the backend", "")

		return
	}

	writeJSON(w, http.StatusAccepted, messageResponse{OK: true, MessageID: messageID})
}

// promoteCard handles POST /api/projects/{project}/cards/{id}/promote - promote to autonomous.
func (h *backendHandlers) promoteCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can promote cards", "")

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

	if card.WorkerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeWorkerNotRunning,
			"card is not currently running",
			fmt.Sprintf("worker_status: %q", card.WorkerStatus))

		return
	}

	// Idempotency guard: if the card is already autonomous, skip the outbound webhook.
	// This prevents infinite recursion when a backend that verifies promotion by re-POSTing
	// to this endpoint triggers a second outbound webhook, which the backend would then
	// re-verify again, and so on.
	if card.Autonomous {
		ctxlog.Logger(r.Context()).Debug("promote short-circuit: card already autonomous, skipping backend webhook",
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

	if err := h.backend.Promote(r.Context(), backend.PromotePayload{
		CardID:  id,
		Project: project,
	}); err != nil {
		ctxlog.Logger(r.Context()).Error("backend promote webhook failed", "card_id", id, "project", project, "error", err)

		// Revert the autonomous/feature_branch/create_pr changes so the card's
		// declared mode matches the agent's actual mode inside the container.
		// Without this rollback, the card is marked autonomous but the agent
		// is still in HITL mode, which produces a silent contract violation
		// (the backend's /promote handler fail-closes when it can't deliver the
		// canned stdin message, leaving the agent unaware of the promotion).
		//
		// Detached context: callers that timed out / disconnected must not
		// strand the rollback - mirror the runCard revert pattern.
		h.revertPromote(r.Context(), project, id, agentID, hadFeatureBranch)

		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "failed to promote backend task", "")

		return
	}

	writeJSON(w, http.StatusAccepted, updatedCard)
}

// revertPromote rolls back the field changes promoteCard made when the backend
// /promote webhook subsequently fails. Mirrors the runCard revert pattern: a
// detached context (context.WithoutCancel) is used so a caller disconnect
// cannot strand the rollback mid-flight. Failure to revert is logged but does
// not change the response to the original caller, who already got 502.
func (h *backendHandlers) revertPromote(ctx context.Context, project, id, agentID string, hadFeatureBranch bool) {
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
		Message: "Reverted autonomous mode: backend /promote webhook failed",
	}); err != nil {
		logger.Error("failed to record promote-webhook-failed activity entry",
			"card_id", id, "project", project, "error", err)
	}
}

// stopCard handles POST /api/projects/{project}/cards/{id}/stop - "Stop".
func (h *backendHandlers) stopCard(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can stop worker tasks", "")

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

	if card.WorkerStatus != "queued" && card.WorkerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeWorkerNotRunning,
			"card is not being executed by a worker",
			fmt.Sprintf("worker_status: %q", card.WorkerStatus))

		return
	}

	if err := h.backend.Kill(r.Context(), backend.KillPayload{CardID: id, Project: project}); err != nil {
		ctxlog.Logger(r.Context()).Error("backend kill webhook failed", "card_id", id, "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable,
			"failed to stop backend task", "")

		return
	}

	card, err = h.svc.UpdateWorkerStatus(r.Context(), project, id, "killed", "task stopped by user")
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, card)
}

// stopAllResponse is the response for the stop-all endpoint.
//
// FailedToUpdate is a parallel list of card IDs for which the backend kill
// webhook succeeded but the subsequent CM-side UpdateWorkerStatus call
// failed. The backend has stopped the container, but CM's view of the card
// has drifted from reality - callers should treat these as "manual
// reconciliation required". Empty when all updates succeeded.
type stopAllResponse struct {
	AffectedCards  []string `json:"affected_cards"`
	FailedToUpdate []string `json:"failed_to_update,omitempty"`
}

// stopAll handles POST /api/projects/{project}/stop-all - "Stop All".
func (h *backendHandlers) stopAll(w http.ResponseWriter, r *http.Request) {
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "only humans can stop worker tasks", "")

		return
	}

	project := r.PathValue("project")

	if h.backend == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")

		return
	}

	// Send stop-all webhook.
	if err := h.backend.StopAll(r.Context(), backend.StopAllPayload{Project: project}); err != nil {
		ctxlog.Logger(r.Context()).Error("backend stop-all webhook failed", "project", project, "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable,
			"failed to stop all backend tasks", "")

		return
	}

	// Update all cards with an active worker in this project.
	cards, err := h.svc.ListCards(r.Context(), project, storage.CardFilter{})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	affected := []string{}
	failed := []string{}

	for _, card := range cards {
		if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
			_, err := h.svc.UpdateWorkerStatus(r.Context(), project, card.ID, "killed", "stopped by stop-all")
			if err != nil {
				// Backend already received the kill webhook above; only CM's view of this
				// card failed to update. Surface the drift in the response so the caller
				// can reconcile rather than silently dropping it from affected_cards.
				ctxlog.Logger(r.Context()).Error("failed to update worker_status during stop-all",
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
