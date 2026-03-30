package api

import (
	"net/http"

	"github.com/mhersson/contextmatrix/internal/service"
)

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
