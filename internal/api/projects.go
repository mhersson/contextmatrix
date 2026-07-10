package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// createProjectRequest is the JSON body for POST /api/projects.
type createProjectRequest struct {
	Name        string              `json:"name"`
	DisplayName string              `json:"display_name,omitempty"`
	Prefix      string              `json:"prefix"`
	Repo        string              `json:"repo,omitempty"`
	States      []string            `json:"states"`
	Types       []string            `json:"types"`
	Priorities  []string            `json:"priorities"`
	Transitions map[string][]string `json:"transitions"`
}

// updateProjectRequest is the JSON body for PUT /api/projects/{project}.
type updateProjectRequest struct {
	Repo             string                    `json:"repo,omitempty"`
	States           []string                  `json:"states"`
	Types            []string                  `json:"types"`
	Priorities       []string                  `json:"priorities"`
	Transitions      map[string][]string       `json:"transitions"`
	GitHub           *board.GitHubImportConfig `json:"github,omitempty"`
	DefaultSkills    *[]string                 `json:"default_skills,omitempty"`
	GitHubCredential *string                   `json:"github_credential"`
	RemoteExecution  *remoteExecutionUpdate    `json:"remote_execution,omitempty"`
	// Verify replaces the whole struct: omitting it preserves the current
	// config; a present object replaces it (zero value clears it on the server).
	Verify *board.VerifyConfig `json:"verify,omitempty"`
}

// remoteExecutionUpdate is the field-level merge shape for remote_execution on
// PUT /api/projects/{project}. Each pointer is applied independently: nil
// preserves the current subfield, non-nil sets it (runner_image "" clears the
// image). Omitting the whole object preserves the existing config.
type remoteExecutionUpdate struct {
	Enabled     *bool   `json:"enabled"`
	RunnerImage *string `json:"runner_image"`
}

// projectHandlers contains handlers for project-related endpoints.
type projectHandlers struct {
	svc           *service.CardService
	runnerEnabled bool
	taskSkills    *taskSkillsLister
	// authEnabled mirrors NewRouter's cfg.AuthService != nil signal — the
	// existing multi-vs-none-mode distinction, not a new one. When false,
	// github_credential bindings are rejected outright (fail-closed).
	authEnabled bool
	// credentialExists looks up a name in the instance credential pool; nil
	// in none mode (authEnabled is false, so it is never called).
	credentialExists func(ctx context.Context, name string) (bool, error)
}

// effectiveRemoteExecution returns a cloned project config with remote_execution.enabled
// forced to false when the runner is globally disabled. This ensures the frontend sees
// the effective state rather than the raw per-project configuration.
func (h *projectHandlers) effectiveRemoteExecution(cfg board.ProjectConfig) board.ProjectConfig {
	if h.runnerEnabled {
		if cfg.RemoteExecution == nil {
			enabled := true
			cfg.RemoteExecution = &board.RemoteExecutionConfig{Enabled: &enabled}
		} else if cfg.RemoteExecution.Enabled == nil {
			re := *cfg.RemoteExecution
			enabled := true
			re.Enabled = &enabled
			cfg.RemoteExecution = &re
		}

		return cfg
	}
	// Runner is globally disabled — force enabled=false so the frontend disables the button.
	disabled := false

	if cfg.RemoteExecution != nil {
		// Clone the existing config to avoid mutating the original.
		re := *cfg.RemoteExecution
		re.Enabled = &disabled
		cfg.RemoteExecution = &re
	} else {
		cfg.RemoteExecution = &board.RemoteExecutionConfig{Enabled: &disabled}
	}

	return cfg
}

// listProjects handles GET /api/projects.
func (h *projectHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.ListProjects(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	effective := make([]board.ProjectConfig, len(projects))
	for i, p := range projects {
		effective[i] = h.effectiveRemoteExecution(p)
	}

	writeJSON(w, http.StatusOK, effective)
}

// getProject handles GET /api/projects/{project}.
func (h *projectHandlers) getProject(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	project, err := h.svc.GetProject(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, h.effectiveRemoteExecution(*project))
}

// getProjectUsage handles GET /api/projects/{project}/usage.
func (h *projectHandlers) getProjectUsage(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	usage, err := h.svc.AggregateUsage(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, usage)
}

// getProjectDashboard handles GET /api/projects/{project}/dashboard.
func (h *projectHandlers) getProjectDashboard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	dashboard, err := h.svc.GetDashboard(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, dashboard)
}

// createProject handles POST /api/projects.
func (h *projectHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled {
		if requireAdmin(w, r) == nil {
			return
		}
	}

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", sanitizeErrorDetails(err))

		return
	}

	if req.Name == "" && req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "name or display_name is required", "")

		return
	}

	if req.Prefix == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "prefix is required", "")

		return
	}

	cfg, err := h.svc.CreateProject(r.Context(), service.CreateProjectInput{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Prefix:      req.Prefix,
		Repo:        req.Repo,
		States:      req.States,
		Types:       req.Types,
		Priorities:  req.Priorities,
		Transitions: req.Transitions,
	})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, cfg)
}

// updateProject handles PUT /api/projects/{project}.
func (h *projectHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled {
		if requireAdmin(w, r) == nil {
			return
		}
	}

	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", sanitizeErrorDetails(err))

		return
	}

	// Validate default_skills against the configured task-skills directory.
	// nil pointer = field omitted, preserve current; non-nil = replace
	// (empty list means "mount nothing").
	if req.DefaultSkills != nil && len(*req.DefaultSkills) > 0 {
		available, err := h.taskSkills.Names(r.Context())
		if err != nil {
			handleServiceError(w, r, err)

			return
		}

		if err := validateSkillsAgainstAvailable(*req.DefaultSkills, available); err != nil {
			writeError(w, http.StatusBadRequest, ErrCodeValidationError, err.Error(), "")

			return
		}
	}

	// Validate github_credential: reference-only, must resolve within the
	// instance credential pool in multi mode. In none mode a real binding is
	// rejected outright rather than silently ignored — a named-but-broken
	// credential binding must never quietly fall back to the instance
	// credential.
	if req.GitHubCredential != nil && *req.GitHubCredential != "" {
		if !h.authEnabled {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError,
				"credential bindings require multi-user mode", "")

			return
		}

		exists, err := h.credentialExists(r.Context(), *req.GitHubCredential)
		if err != nil {
			handleServiceError(w, r, err)

			return
		}

		if !exists {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "unknown credential", "")

			return
		}
	}

	var remoteExecution *service.RemoteExecutionUpdate
	if req.RemoteExecution != nil {
		remoteExecution = &service.RemoteExecutionUpdate{
			Enabled:     req.RemoteExecution.Enabled,
			RunnerImage: req.RemoteExecution.RunnerImage,
		}
	}

	cfg, err := h.svc.UpdateProject(r.Context(), projectName, service.UpdateProjectInput{
		Repo:             req.Repo,
		Verify:           req.Verify,
		States:           req.States,
		Types:            req.Types,
		Priorities:       req.Priorities,
		Transitions:      req.Transitions,
		GitHub:           req.GitHub,
		DefaultSkills:    req.DefaultSkills,
		GitHubCredential: req.GitHubCredential,
		RemoteExecution:  remoteExecution,
	})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

// recalculateCostsRequest is the JSON body for POST /api/projects/{project}/recalculate-costs.
type recalculateCostsRequest struct {
	DefaultModel string `json:"default_model"`
}

// recalculateCostsResponse is the JSON response for POST /api/projects/{project}/recalculate-costs.
type recalculateCostsResponse struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

// recalculateCosts handles POST /api/projects/{project}/recalculate-costs.
func (h *projectHandlers) recalculateCosts(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled {
		if requireAdmin(w, r) == nil {
			return
		}
	}

	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	var req recalculateCostsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", sanitizeErrorDetails(err))

		return
	}

	if req.DefaultModel == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "default_model is required", "")

		return
	}

	result, err := h.svc.RecalculateCosts(r.Context(), projectName, req.DefaultModel)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, recalculateCostsResponse{
		CardsUpdated:          result.CardsUpdated,
		TotalCostRecalculated: result.TotalCostRecalculated,
	})
}

// deleteProject handles DELETE /api/projects/{project}.
func (h *projectHandlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled {
		if requireAdmin(w, r) == nil {
			return
		}
	}

	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	if err := h.svc.DeleteProject(r.Context(), projectName); err != nil {
		handleServiceError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
