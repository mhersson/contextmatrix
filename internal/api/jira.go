package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/jira"
)

// JiraStatusClient is the slice of the Jira client the status endpoint uses.
type JiraStatusClient interface {
	Myself(ctx context.Context) (jira.Account, error)
}

// JiraWritebackStatus reports the writeback worker's live state.
type JiraWritebackStatus interface {
	Paused() bool
}

// JiraInfo is the non-secret Jira configuration the API reports.
type JiraInfo struct {
	SiteURL          string
	AuthMode         string
	WritebackEnabled bool
}

const jiraStatusTimeout = 10 * time.Second

// jiraAuthError results; the status endpoint reports them as auth_error.
const (
	jiraAuthCaptchaRequired = "captcha_required"
	jiraAuthUnauthorized    = "unauthorized"
	jiraAuthUnreachable     = "unreachable"
)

type jiraHandlers struct {
	client    JiraStatusClient
	info      JiraInfo
	writeback JiraWritebackStatus
}

type jiraStatusResponse struct {
	Configured       bool   `json:"configured"`
	Authenticated    bool   `json:"authenticated"`
	Account          string `json:"account,omitempty"`
	AuthMode         string `json:"auth_mode"`
	WritebackEnabled bool   `json:"writeback_enabled"`
	WritebackPaused  bool   `json:"writeback_paused"`
	SiteURL          string `json:"site_url"`
	AuthError        string `json:"auth_error,omitempty"`
}

// getStatus reports whether Jira is configured and whether its credentials
// work right now. It always answers 200: a failing token is status, not an
// API error.
func (h *jiraHandlers) getStatus(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"configured": false})

		return
	}

	resp := jiraStatusResponse{
		Configured:       true,
		AuthMode:         h.info.AuthMode,
		WritebackEnabled: h.info.WritebackEnabled,
		SiteURL:          h.info.SiteURL,
	}

	if h.writeback != nil {
		resp.WritebackPaused = h.writeback.Paused()
	}

	ctx, cancel := context.WithTimeout(r.Context(), jiraStatusTimeout)
	defer cancel()

	acct, err := h.client.Myself(ctx)
	if err != nil {
		resp.AuthError = jiraAuthError(err)
		ctxlog.Logger(r.Context()).Warn("jira status probe failed", "auth_error", resp.AuthError, "error", err)
		writeJSON(w, http.StatusOK, resp)

		return
	}

	resp.Authenticated = true
	resp.Account = acct.DisplayName
	writeJSON(w, http.StatusOK, resp)
}

func jiraAuthError(err error) string {
	if authErr, ok := errors.AsType[*jira.AuthError](err); ok && authErr.CaptchaRequired() {
		return jiraAuthCaptchaRequired
	}

	if errors.Is(err, jira.ErrUnauthorized) {
		return jiraAuthUnauthorized
	}

	return jiraAuthUnreachable
}
