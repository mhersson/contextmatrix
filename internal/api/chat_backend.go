package api

import (
	"net/http"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/backend"
)

// chatBackendHandlers serves the HMAC-signed callbacks the dedicated chat
// service makes back to CM. Today that is only the task-skills git pointer; the
// chat worker clones it and exposes the skills via its Skill tool. It closes
// over the chat backend's HMAC key + its own replay cache so it verifies
// independently of the task backend.
type chatBackendHandlers struct {
	apiKey                 string
	replayCache            *backend.SignatureCache
	taskSkillsDir          string
	taskSkillsGitRemoteURL string

	// instanceTokenProvider mirrors backendHandlers.instanceTokenProvider —
	// same instance-scoped, best-effort mint for the chat variant's
	// task-skills-source response.
	instanceTokenProvider githubauth.TokenGenerator
}

// getTaskSkillsSource serves GET /api/chat/task-skills-source — the chat service
// fetches this {git_remote_url, ref} pointer and clones the task-skills repo
// itself, mirroring the agent backend. CM stays the single source of truth.
func (h *chatBackendHandlers) getTaskSkillsSource(w http.ResponseWriter, r *http.Request) {
	if !authenticateBackendGet(w, r, h.apiKey, h.replayCache) {
		return
	}

	url, ref := taskSkillsSource(h.taskSkillsDir, h.taskSkillsGitRemoteURL)
	token, tokenExpiresAt := mintInstanceToken(r.Context(), h.instanceTokenProvider)

	writeJSON(w, http.StatusOK, taskSkillsSourceResponse{
		GitRemoteURL: url, Ref: ref, Token: token, TokenExpiresAt: tokenExpiresAt,
	})
}
