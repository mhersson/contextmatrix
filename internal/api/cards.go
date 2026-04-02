package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// cardHandlers contains handlers for card-related endpoints.
type cardHandlers struct {
	svc *service.CardService
}

// createCardRequest is the JSON body for creating a card.
type createCardRequest struct {
	Title         string       `json:"title"`
	Type          string       `json:"type"`
	Priority      string       `json:"priority"`
	Labels        []string     `json:"labels"`
	Parent        string       `json:"parent"`
	Body          string       `json:"body"`
	Source        *board.Source `json:"source"`
	Autonomous    bool         `json:"autonomous"`
	FeatureBranch bool         `json:"feature_branch"`
	CreatePR      bool         `json:"create_pr"`
}

// updateCardRequest is the JSON body for full card updates.
// All fields use value types to match PUT's full-replacement semantics.
type updateCardRequest struct {
	Title         string         `json:"title"`
	Type          string         `json:"type"`
	State         string         `json:"state"`
	Priority      string         `json:"priority"`
	Labels        []string       `json:"labels"`
	Parent        string         `json:"parent"`
	Subtasks      []string       `json:"subtasks"`
	DependsOn     []string       `json:"depends_on"`
	Context       []string       `json:"context"`
	Custom        map[string]any `json:"custom"`
	Body          string         `json:"body"`
	Autonomous    bool           `json:"autonomous"`
	FeatureBranch bool           `json:"feature_branch"`
	CreatePR      bool           `json:"create_pr"`
}

// patchCardRequest is the JSON body for partial card updates.
type patchCardRequest struct {
	Title         *string  `json:"title,omitempty"`
	State         *string  `json:"state,omitempty"`
	Priority      *string  `json:"priority,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	Body          *string  `json:"body,omitempty"`
	Autonomous    *bool    `json:"autonomous,omitempty"`
	FeatureBranch *bool    `json:"feature_branch,omitempty"`
	CreatePR      *bool    `json:"create_pr,omitempty"`
}

// isNonHumanAgent returns true if the request has an agent ID that is not a human user.
func isNonHumanAgent(r *http.Request) bool {
	agentID := r.Header.Get("X-Agent-ID")
	return agentID != "" && !strings.HasPrefix(agentID, "human:")
}

// validateAgentOwnership checks if the requesting agent can mutate a claimed card.
// Returns an error message if unauthorized, empty string if allowed.
// Unclaimed cards can be mutated by anyone.
func validateAgentOwnership(r *http.Request, card *board.Card) string {
	if card.AssignedAgent == "" {
		return "" // Unclaimed cards can be mutated by anyone
	}

	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		return "X-Agent-ID header required to modify claimed card"
	}

	if agentID != card.AssignedAgent {
		return "card is claimed by " + card.AssignedAgent
	}

	return ""
}

// listCards handles GET /api/projects/{project}/cards
func (h *cardHandlers) listCards(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	// Build filter from query params
	filter := storage.CardFilter{
		State:         r.URL.Query().Get("state"),
		Type:          r.URL.Query().Get("type"),
		Priority:      r.URL.Query().Get("priority"),
		AssignedAgent: r.URL.Query().Get("agent"),
		Label:         r.URL.Query().Get("label"),
		Parent:        r.URL.Query().Get("parent"),
		ExternalID:    r.URL.Query().Get("external_id"),
	}

	cards, err := h.svc.ListCards(r.Context(), projectName, filter)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, cards)
}

// createCard handles POST /api/projects/{project}/cards
func (h *cardHandlers) createCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	var req createCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "title is required", "")
		return
	}

	// Autonomous fields can only be set by human users (UI), never by agents.
	if isNonHumanAgent(r) && (req.Autonomous || req.FeatureBranch || req.CreatePR) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, feature_branch, and create_pr can only be set via the UI")
		return
	}

	input := service.CreateCardInput{
		Title:         req.Title,
		Type:          req.Type,
		Priority:      req.Priority,
		Labels:        req.Labels,
		Parent:        req.Parent,
		Body:          req.Body,
		Source:        req.Source,
		Autonomous:    req.Autonomous,
		FeatureBranch: req.FeatureBranch,
		CreatePR:      req.CreatePR,
	}

	card, err := h.svc.CreateCard(r.Context(), projectName, input)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, card)
}

// getCard handles GET /api/projects/{project}/cards/{id}
func (h *cardHandlers) getCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	card, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// updateCard handles PUT /api/projects/{project}/cards/{id}
func (h *cardHandlers) updateCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req updateCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}
	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)
		return
	}

	// Autonomous fields can only be changed by human users (UI), never by agents.
	// For PUT semantics, compare against existing values to catch both setting AND clearing.
	if isNonHumanAgent(r) && (req.Autonomous != existingCard.Autonomous ||
		req.FeatureBranch != existingCard.FeatureBranch ||
		req.CreatePR != existingCard.CreatePR) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, feature_branch, and create_pr can only be changed via the UI")
		return
	}

	input := service.UpdateCardInput{
		Title:           req.Title,
		Type:            req.Type,
		State:           req.State,
		Priority:        req.Priority,
		Labels:          req.Labels,
		Parent:          req.Parent,
		Subtasks:        req.Subtasks,
		DependsOn:       req.DependsOn,
		Context:         req.Context,
		Custom:          req.Custom,
		Body:            req.Body,
		ImmediateCommit: existingCard.AssignedAgent == "",
		Autonomous:      req.Autonomous,
		FeatureBranch:   req.FeatureBranch,
		CreatePR:        req.CreatePR,
	}

	card, err := h.svc.UpdateCard(r.Context(), projectName, cardID, input)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// patchCard handles PATCH /api/projects/{project}/cards/{id}
func (h *cardHandlers) patchCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	var req patchCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", err.Error())
		return
	}

	// Autonomous fields can only be set by human users (UI), never by agents.
	if isNonHumanAgent(r) && (req.Autonomous != nil || req.FeatureBranch != nil || req.CreatePR != nil) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, feature_branch, and create_pr can only be set via the UI")
		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}
	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)
		return
	}

	input := service.PatchCardInput{
		Title:           req.Title,
		State:           req.State,
		Priority:        req.Priority,
		Labels:          req.Labels,
		Body:            req.Body,
		ImmediateCommit: existingCard.AssignedAgent == "",
		Autonomous:      req.Autonomous,
		FeatureBranch:   req.FeatureBranch,
		CreatePR:        req.CreatePR,
	}

	card, err := h.svc.PatchCard(r.Context(), projectName, cardID, input)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// deleteCard handles DELETE /api/projects/{project}/cards/{id}
func (h *cardHandlers) deleteCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")
		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, err)
		return
	}
	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)
		return
	}

	if err := h.svc.DeleteCard(r.Context(), projectName, cardID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
