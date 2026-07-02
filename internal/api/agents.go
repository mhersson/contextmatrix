package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/service"
)

// agentHandlers contains handlers for agent-related endpoints.
type agentHandlers struct {
	svc *service.CardService
}

// agentRequest is the JSON body for claim and release operations.
type agentRequest struct{}

// claimCard handles POST /api/projects/{project}/cards/{id}/claim.
func (h *agentHandlers) claimCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
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

// extractAgentID returns the trimmed X-Agent-ID header. It is the sole source
// of agent identity on agent endpoints. The previous body-field fallback was
// removed because it bypassed the human:-prefix gate enforced elsewhere
// (cards.go callers read only the header): a request with no header but
// agent_id="human:alice" in body would claim as Alice while later mutation
// checks would reject the same caller.
func extractAgentID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Agent-ID"))
}
