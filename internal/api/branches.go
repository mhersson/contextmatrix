package api

import (
	"context"
	"net/http"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
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
	provider         githubauth.TokenGenerator
	githubAPIBaseURL string
	allowedHosts     []string
	newBranchClient  func(provider githubauth.TokenGenerator, baseURL string) BranchFetcher
}

// listBranches handles GET /api/projects/{project}/branches.
// It returns a JSON array of branch name strings from the project's GitHub repo.
func (h *branchHandlers) listBranches(w http.ResponseWriter, r *http.Request) {
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

	owner, repo, _, ok := github.ParseGitHubRepo(project.Repo, h.allowedHosts)
	if !ok {
		writeError(w, http.StatusNotFound, "NO_GITHUB_REPO",
			"project does not have a GitHub repository URL", "")

		return
	}

	client := h.newBranchClient(h.provider, h.githubAPIBaseURL)

	branches, err := client.FetchBranches(r.Context(), owner, repo)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("failed to fetch branches", "project", projectName, "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError,
			"failed to fetch branches", "")

		return
	}

	writeJSON(w, http.StatusOK, branches)
}

// defaultBranchClient returns a github.Client constructed from the provider.
func defaultBranchClient(provider githubauth.TokenGenerator, baseURL string) BranchFetcher {
	return github.NewClientWithBaseURL(provider, baseURL)
}
