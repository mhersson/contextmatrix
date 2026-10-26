package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/mhersson/contextmatrix/internal/service"
)

// BranchFetcher can list branches from a GitHub repository.
type BranchFetcher interface {
	FetchBranches(ctx context.Context, owner, repo string) ([]string, error)
}

// branchHandlers contains handlers for the branch listing endpoint.
type branchHandlers struct {
	svc             *service.CardService
	allowedHosts    []string
	newBranchClient func(provider githubauth.TokenGenerator, baseURL string) BranchFetcher
	// providerForProject resolves the token provider and API base URL per
	// request, by project (credential binding support).
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error)
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
		writeError(w, http.StatusNotFound, ErrCodeNoGitHubRepo,
			"project does not have a GitHub repository URL", "")

		return
	}

	provider, apiBaseURL, err := h.providerForProject(r.Context(), projectName)
	if err != nil {
		if errors.Is(err, auth.ErrCredentialUnavailable) {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError,
				"project credential unavailable", "")

			return
		}

		handleServiceError(w, r, err)

		return
	}

	client := h.newBranchClient(provider, apiBaseURL)

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
