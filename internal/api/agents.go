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
type agentRequest struct {
	AgentID string `json:"agent_id"`
}

// addLogRequest is the JSON body for adding a log entry.
type addLogRequest struct {
	AgentID string `json:"agent_id"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// claimCard handles POST /api/projects/{project}/cards/{id}/claim
func (h *agentHandlers) claimCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	agentID := extractAgentID(r, req.AgentID)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "provide X-Agent-ID header or agent_id in body")
		return
	}

	card, err := h.svc.ClaimCard(r.Context(), projectName, cardID, agentID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// releaseCard handles POST /api/projects/{project}/cards/{id}/release
func (h *agentHandlers) releaseCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	agentID := extractAgentID(r, req.AgentID)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "provide X-Agent-ID header or agent_id in body")
		return
	}

	card, err := h.svc.ReleaseCard(r.Context(), projectName, cardID, agentID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// heartbeatCard handles POST /api/projects/{project}/cards/{id}/heartbeat
func (h *agentHandlers) heartbeatCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	agentID := extractAgentID(r, req.AgentID)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "provide X-Agent-ID header or agent_id in body")
		return
	}

	if err := h.svc.HeartbeatCard(r.Context(), projectName, cardID, agentID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// addLogEntry handles POST /api/projects/{project}/cards/{id}/log
func (h *agentHandlers) addLogEntry(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req addLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	agentID := extractAgentID(r, req.AgentID)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "agent_id required", "provide X-Agent-ID header or agent_id in body")
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
		handleServiceError(w, err)
		return
	}

	// Return the updated card so caller can see the new activity log
	card, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// getCardContext handles GET /api/projects/{project}/cards/{id}/context
func (h *agentHandlers) getCardContext(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	ctx, err := h.svc.GetCardContext(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ctx)
}

// extractAgentID gets the agent ID from X-Agent-ID header with fallback to body value.
// The result is trimmed of whitespace.
func extractAgentID(r *http.Request, bodyAgentID string) string {
	if headerID := strings.TrimSpace(r.Header.Get("X-Agent-ID")); headerID != "" {
		return headerID
	}
	return strings.TrimSpace(bodyAgentID)
}
