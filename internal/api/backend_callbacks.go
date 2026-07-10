package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// Callback request bodies are protocol-owned; aliased so handlers keep their local names.
type workerStatusRequest = protocol.StatusCallbackPayload

// workerStatusUpdate handles POST <AgentCallbackPath>/status — the backend's
// worker-status callback.
func (h *backendHandlers) workerStatusUpdate(w http.ResponseWriter, r *http.Request) {
	body, ok := h.authenticatePost(w, r)
	if !ok {
		return
	}

	var req workerStatusRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON", "")

		return
	}

	// Validate that the callback only sets backend-settable statuses.
	v := board.NewValidator()
	if err := v.ValidateWorkerCallbackStatus(req.RunnerStatus); err != nil {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError,
			"invalid runner callback status", err.Error())

		return
	}

	card, err := h.svc.UpdateWorkerStatus(r.Context(), req.Project, strings.ToUpper(req.CardID), req.RunnerStatus, req.Message)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// cardAutonomousResponse is the minimal read-only shape returned to the
// backend's VerifyAutonomous call. Deliberately narrow — only the boolean
// is needed, and a backend-facing endpoint must not leak unrelated card
// fields.
type cardAutonomousResponse struct {
	Autonomous bool `json:"autonomous"`
}

// getCardAutonomous handles GET /api/v1/cards/{project}/{id}/autonomous.
// The backend calls this during /promote to fail-closed confirm the card's
// autonomous flag before writing the canned stdin message.
func (h *backendHandlers) getCardAutonomous(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateGet(w, r) {
		return
	}

	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), projectName, strings.ToUpper(cardID))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, cardAutonomousResponse{Autonomous: card.Autonomous})
}

// getTaskSkillsSource serves GET /api/<backend>/task-skills-source — the agent
// backend fetches this {git_remote_url, ref} pointer and clones the task-skills
// repo itself. Signed-GET like getCardAutonomous.
func (h *backendHandlers) getTaskSkillsSource(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateGet(w, r) {
		return
	}

	url, ref := taskSkillsSource(h.taskSkillsDir, h.taskSkillsGitRemoteURL)
	token, tokenExpiresAt := mintInstanceToken(r.Context(), h.instanceTokenProvider)

	writeJSON(w, http.StatusOK, taskSkillsSourceResponse{
		GitRemoteURL: url, Ref: ref, Token: token, TokenExpiresAt: tokenExpiresAt,
	})
}

// mintInstanceToken best-effort mints an instance-scoped git token for a
// task-skills-source response. See the asymmetry comment on
// taskSkillsSourceResponse: unlike getGitCredentials (fail-closed on a broken
// project binding), this never fails the request — a nil provider or a mint
// error just returns empty strings, and the caller falls back to its own
// configured credential during the compat window.
//
// err is only ever a githubauth provider error (JWT/HTTP-status class
// messages, never the token itself), so logging "error", err here is safe —
// mirrors the class-only logging already used for provider errors elsewhere
// in this file.
func mintInstanceToken(ctx context.Context, provider githubauth.TokenGenerator) (token, expiresAt string) {
	if provider == nil {
		return "", ""
	}

	tok, exp, err := provider.GenerateToken(ctx)
	if err != nil {
		ctxlog.Logger(ctx).Warn("failed to mint instance token for task-skills-source; continuing without it",
			"error", err)

		return "", ""
	}

	return tok, tokenExpiryString(exp)
}

// tokenExpiryString formats a minted token's expiry for the wire. Zero and
// far-future sentinel expiries (githubauth's PATProvider reports year 9999 —
// a PAT has no server-managed TTL) are omitted entirely: an absent expiry
// means "do not schedule a refresh", which is exactly the PAT semantic.
func tokenExpiryString(t time.Time) string {
	if t.IsZero() || t.Year() >= 9000 {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

// getGitCredentials handles GET /api/<backend>/git-credentials — re-mints the
// project-scoped git token for a running card. Long runs outlive ~1h GitHub
// App installation tokens, so the backend calls this mid-run to refresh.
// HMAC-signed like every backend callback.
//
// Fail-closed on the project binding, mirroring rejectRunForCredentialFailure:
// a broken/unresolvable providerForProject NEVER falls back to the instance
// credential — unlike task-skills-source (mintInstanceToken), which is
// deliberately best-effort because it has no binding to be wrong about.
func (h *backendHandlers) getGitCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateGet(w, r) {
		return
	}

	project := r.URL.Query().Get("project")
	cardID := r.URL.Query().Get("card_id")

	if project == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card_id required", "")

		return
	}

	// No free token faucet: the card must exist and be actively running.
	card, err := h.svc.GetCard(r.Context(), project, strings.ToUpper(cardID))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if card.RunnerStatus != "running" {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "card is not running", "")

		return
	}

	if h.providerForProject == nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "project credential unavailable", "")

		return
	}

	provider, _, err := h.providerForProject(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusConflict, ErrCodeValidationError, "project credential unavailable", sanitizeErrorDetails(err))

		return
	}

	token, expiresAt, err := provider.GenerateToken(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, ErrCodeInternalError, "token mint failed", sanitizeErrorDetails(err))

		return
	}

	resp := map[string]string{"token": token}
	if s := tokenExpiryString(expiresAt); s != "" {
		resp["expires_at"] = s
	}

	writeJSON(w, http.StatusOK, resp)
}
