package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// agentHandlers contains handlers for agent-related endpoints.
type agentHandlers struct {
	svc *service.CardService
}

// agentRequest is the JSON body for claim, release, and heartbeat operations.
// The body is currently empty — agent identity comes from X-Agent-ID.
type agentRequest struct{}

// addLogRequest is the JSON body for adding a log entry.
type addLogRequest struct {
	Action  string `json:"action"`
	Message string `json:"message"`
}

// claimCard handles POST /api/projects/{project}/cards/{id}/claim.
func (h *agentHandlers) claimCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	card, err := h.svc.ClaimCard(r.Context(), projectName, cardID, agentID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// releaseCard handles POST /api/projects/{project}/cards/{id}/release.
func (h *agentHandlers) releaseCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	card, err := h.svc.ReleaseCard(r.Context(), projectName, cardID, agentID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// heartbeatCard handles POST /api/projects/{project}/cards/{id}/heartbeat.
func (h *agentHandlers) heartbeatCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	if err := h.svc.HeartbeatCard(r.Context(), projectName, cardID, agentID); err != nil {
		handleServiceError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// addLogEntry handles POST /api/projects/{project}/cards/{id}/log.
func (h *agentHandlers) addLogEntry(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req addLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	if req.Action == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "action required", "")

		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "message required", "")

		return
	}

	entry := board.ActivityEntry{
		Agent:   agentID,
		Action:  req.Action,
		Message: req.Message,
		// Timestamp is set by service layer if zero
	}

	if err := h.svc.AddLogEntry(r.Context(), projectName, cardID, entry); err != nil {
		handleServiceError(w, r, err)

		return
	}

	// Return the updated card so caller can see the new activity log
	card, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// getCardContext handles GET /api/projects/{project}/cards/{id}/context.
func (h *agentHandlers) getCardContext(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	ctx, err := h.svc.GetCardContext(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, ctx)
}

// reportUsageRequest is the JSON body for reporting token usage.
type reportUsageRequest struct {
	Model            string `json:"model"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// reportUsage handles POST /api/projects/{project}/cards/{id}/usage.
func (h *agentHandlers) reportUsage(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req reportUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	card, err := h.svc.ReportUsage(r.Context(), projectName, cardID, service.ReportUsageInput{
		AgentID:          agentID,
		Model:            req.Model,
		PromptTokens:     req.PromptTokens,
		CompletionTokens: req.CompletionTokens,
	})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// reportPushRequest is the JSON body for reporting a git push.
type reportPushRequest struct {
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch"`
	PRUrl  string `json:"pr_url,omitempty"`
}

// reportPush handles POST /api/projects/{project}/cards/{id}/report-push
// Validates the branch is not main/master and records the push on the card.
func (h *agentHandlers) reportPush(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req reportPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "X-Agent-ID header is required")

		return
	}

	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "branch required", "")

		return
	}

	card, err := h.svc.RecordPush(r.Context(), projectName, cardID, agentID, req.Repo, branch, req.PRUrl)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// extractAgentID returns the trimmed X-Agent-ID header. It is the sole source
// of agent identity on agent endpoints. The previous body-field fallback was
// removed because it bypassed the human:-prefix gate enforced elsewhere
// (cards.go:isNonHumanAgent / validateAgentOwnership read only the header):
// a request with no header but agent_id="human:alice" in body would claim as
// Alice while later mutation checks would reject the same caller.
func extractAgentID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Agent-ID"))
}
