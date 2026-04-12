package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/jira"
)

// jiraHandlers contains handlers for Jira integration endpoints.
type jiraHandlers struct {
	importer *jira.Importer
	baseURL  string
}

// jiraStatusResponse is the response for GET /api/jira/status.
type jiraStatusResponse struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"base_url,omitempty"`
}

// status handles GET /api/jira/status.
// Returns whether Jira integration is configured (no secrets exposed).
func (h *jiraHandlers) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jiraStatusResponse{
		Configured: h.importer != nil,
		BaseURL:    h.baseURL,
	})
}

// previewEpic handles GET /api/jira/epic/{epicKey}.
// Fetches the epic and its children from Jira without importing.
func (h *jiraHandlers) previewEpic(w http.ResponseWriter, r *http.Request) {
	if h.importer == nil {
		writeError(w, http.StatusServiceUnavailable, "JIRA_NOT_CONFIGURED", "Jira integration is not configured", "")
		return
	}

	epicKey := r.PathValue("epicKey")
	if epicKey == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "epic key is required", "")
		return
	}

	preview, err := h.importer.PreviewEpic(r.Context(), epicKey)
	if err != nil {
		handleJiraError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, preview)
}

// importEpicRequest is the JSON body for POST /api/jira/import-epic.
type importEpicRequest struct {
	EpicKey      string   `json:"epic_key"`
	Name         string   `json:"name,omitempty"`
	Prefix       string   `json:"prefix,omitempty"`
	SelectedKeys []string `json:"selected_keys,omitempty"`
}

// importEpic handles POST /api/jira/import-epic.
// Creates a CM project from a Jira epic with all child issues as cards.
func (h *jiraHandlers) importEpic(w http.ResponseWriter, r *http.Request) {
	if h.importer == nil {
		writeError(w, http.StatusServiceUnavailable, "JIRA_NOT_CONFIGURED", "Jira integration is not configured", "")
		return
	}

	// Human-only: reject agent requests.
	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "Jira import is a human-only operation", "")
		return
	}

	var req importEpicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", err.Error())
		return
	}

	if req.EpicKey == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "epic_key is required", "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := h.importer.ImportEpic(ctx, jira.ImportEpicInput{
		EpicKey:      req.EpicKey,
		Name:         req.Name,
		Prefix:       req.Prefix,
		SelectedKeys: req.SelectedKeys,
	})
	if err != nil {
		handleJiraError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// handleJiraError maps Jira client errors to HTTP responses.
func handleJiraError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jira.ErrNotFound):
		writeError(w, http.StatusNotFound, "JIRA_NOT_FOUND", "Jira issue not found", err.Error())
	case errors.Is(err, jira.ErrUnauthorized):
		writeError(w, http.StatusBadGateway, "JIRA_UNAUTHORIZED", "Jira authentication failed", err.Error())
	case errors.Is(err, jira.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "JIRA_RATE_LIMITED", "Jira rate limit exceeded", err.Error())
	default:
		handleServiceError(w, err)
	}
}
