package api

import (
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
	if !decodeJSONAllowEmpty(w, r, &req) {
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
	if !decodeJSONAllowEmpty(w, r, &req) {
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

// forceReleaseCard handles POST /api/projects/{project}/cards/{id}/force-release.
// Human-only: clears another agent's claim when the worker has crashed or is
// wedged and cannot release it itself.
func (h *agentHandlers) forceReleaseCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"only humans can force-release a claim", "agent_id must start with \"human:\"")

		return
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		agentID = "human:api"
	}

	card, err := h.svc.ForceReleaseCard(r.Context(), projectName, cardID, agentID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// extractAgentID returns the caller identity. In multi-user mode the session
// middleware stamps "human:<username>" into the request context and that
// ALWAYS wins - a browser cannot claim a different identity via header. The
// X-Agent-ID header remains the sole source on machine channels and in
// single-user mode (where no session middleware runs).
func extractAgentID(r *http.Request) string {
	if id, ok := sessionIdentity(r.Context()); ok {
		return id
	}

	return strings.TrimSpace(r.Header.Get("X-Agent-ID"))
}
