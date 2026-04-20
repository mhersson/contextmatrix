package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/service"
)

// BranchFetcher can list branches from a GitHub repository.
type BranchFetcher interface {
	FetchBranches(ctx context.Context, owner, repo string) ([]string, error)
}

// branchHandlers contains handlers for the branch listing endpoint.
type branchHandlers struct {
	svc              *service.CardService
	githubToken      string
	githubAPIBaseURL string
	allowedHosts     []string
	newBranchClient  func(token, baseURL string) BranchFetcher
}

// listBranches handles GET /api/projects/{project}/branches.
// It returns a JSON array of branch name strings from the project's GitHub repo.
func (h *branchHandlers) listBranches(w http.ResponseWriter, r *http.Request) {
	if h.githubToken == "" {
		writeError(w, http.StatusServiceUnavailable, "NO_GITHUB_TOKEN",
			"GitHub token is not configured", "")

		return
	}

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

	owner, repo, _, ok := github.ParseGitHubRepo(project.Repo, h.allowedHosts)
	if !ok {
		writeError(w, http.StatusNotFound, "NO_GITHUB_REPO",
			"project does not have a GitHub repository URL", "")

		return
	}

	client := h.newBranchClient(h.githubToken, h.githubAPIBaseURL)

	branches, err := client.FetchBranches(r.Context(), owner, repo)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("failed to fetch branches", "project", projectName, "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError,
			"failed to fetch branches", "")

		return
	}

	writeJSON(w, http.StatusOK, branches)
}

// defaultBranchClient creates a real GitHub API client.
func defaultBranchClient(token, baseURL string) BranchFetcher {
	return github.NewClientWithBaseURL(token, baseURL)
}
