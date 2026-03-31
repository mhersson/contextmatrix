package api

import (
	"encoding/json"
	"net/http"

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
	Repo        string              `json:"repo,omitempty"`
	States      []string            `json:"states"`
	Types       []string            `json:"types"`
	Priorities  []string            `json:"priorities"`
	Transitions map[string][]string `json:"transitions"`
}

// projectHandlers contains handlers for project-related endpoints.
type projectHandlers struct {
	svc *service.CardService
}

// listProjects handles GET /api/projects
func (h *projectHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.ListProjects(r.Context())
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, projects)
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

	writeJSON(w, http.StatusOK, project)
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
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, cfg)
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
