package api

import (
	"encoding/json"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// createProjectRequest is the JSON body for POST /api/projects.
type createProjectRequest struct {
	Name        string              `json:"name"`
	Prefix      string              `json:"prefix"`
	Repo        string              `json:"repo,omitempty"`
	States      []string            `json:"states"`
	Types       []string            `json:"types"`
	Priorities  []string            `json:"priorities"`
	Transitions map[string][]string `json:"transitions"`
}

// updateProjectRequest is the JSON body for PUT /api/projects/{project}.
type updateProjectRequest struct {
	Repo        string                   `json:"repo,omitempty"`
	States      []string                 `json:"states"`
	Types       []string                 `json:"types"`
	Priorities  []string                 `json:"priorities"`
	Transitions map[string][]string      `json:"transitions"`
	GitHub      *board.GitHubImportConfig `json:"github,omitempty"`
}

// projectHandlers contains handlers for project-related endpoints.
type projectHandlers struct {
	svc           *service.CardService
	runnerEnabled bool
}

// effectiveRemoteExecution returns a cloned project config with remote_execution.enabled
// forced to false when the runner is globally disabled. This ensures the frontend sees
// the effective state rather than the raw per-project configuration.
func (h *projectHandlers) effectiveRemoteExecution(cfg board.ProjectConfig) board.ProjectConfig {
	if h.runnerEnabled {
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

// listProjects handles GET /api/projects
func (h *projectHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.ListProjects(r.Context())
	if err != nil {
		handleServiceError(w, err)
		return
	}

	effective := make([]board.ProjectConfig, len(projects))
	for i, p := range projects {
		effective[i] = h.effectiveRemoteExecution(p)
	}

	writeJSON(w, http.StatusOK, effective)
}

// getProject handles GET /api/projects/{project}
func (h *projectHandlers) getProject(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	project, err := h.svc.GetProject(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, h.effectiveRemoteExecution(*project))
}

// getProjectUsage handles GET /api/projects/{project}/usage
func (h *projectHandlers) getProjectUsage(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	usage, err := h.svc.AggregateUsage(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, usage)
}

// getProjectDashboard handles GET /api/projects/{project}/dashboard
func (h *projectHandlers) getProjectDashboard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	dashboard, err := h.svc.GetDashboard(r.Context(), projectName)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, dashboard)
}

// createProject handles POST /api/projects
func (h *projectHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" || req.Prefix == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "name and prefix are required", "")
		return
	}

	cfg, err := h.svc.CreateProject(r.Context(), service.CreateProjectInput{
		Name:        req.Name,
		Prefix:      req.Prefix,
		Repo:        req.Repo,
		States:      req.States,
		Types:       req.Types,
		Priorities:  req.Priorities,
		Transitions: req.Transitions,
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, cfg)
}

// updateProject handles PUT /api/projects/{project}
func (h *projectHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", err.Error())
		return
	}

	cfg, err := h.svc.UpdateProject(r.Context(), projectName, service.UpdateProjectInput{
		Repo:        req.Repo,
		States:      req.States,
		Types:       req.Types,
		Priorities:  req.Priorities,
		Transitions: req.Transitions,
		GitHub:      req.GitHub,
	})
	if err != nil {
		handleServiceError(w, err)
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

// recalculateCosts handles POST /api/projects/{project}/recalculate-costs
func (h *projectHandlers) recalculateCosts(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	var req recalculateCostsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body", err.Error())
		return
	}

	if req.DefaultModel == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "default_model is required", "")
		return
	}

	result, err := h.svc.RecalculateCosts(r.Context(), projectName, req.DefaultModel)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, recalculateCostsResponse{
		CardsUpdated:          result.CardsUpdated,
		TotalCostRecalculated: result.TotalCostRecalculated,
	})
}

// deleteProject handles DELETE /api/projects/{project}
func (h *projectHandlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")
		return
	}

	if err := h.svc.DeleteProject(r.Context(), projectName); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
