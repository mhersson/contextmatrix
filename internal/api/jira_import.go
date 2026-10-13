package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/jiraimport"
)

// Jira import error codes. NOT_CONFIGURED -> 404 (no jira block, or
// playbooks disabled). IMPORT_NOT_FOUND -> 404 (unknown id, aged out, or
// another instance's import). ISSUE_NOT_FOUND -> 404 (also no permission).
// NOT_EPIC -> 422. PLAYBOOK_RUNNING -> 409 (the epic's playbook has an
// active run). AUTH_FAILED, REQUEST_REJECTED and UNAVAILABLE -> 502 (Jira
// rejected the token, refused the request with another 4xx, or failed).
// RATE_LIMITED -> 503 (Jira's 429 outlasted the wait). IMPORT_UNAVAILABLE
// -> 503 (the import service takes no imports, as during shutdown).
const (
	ErrCodeJiraNotConfigured     = "JIRA_NOT_CONFIGURED"
	ErrCodeJiraImportNotFound    = "JIRA_IMPORT_NOT_FOUND"
	ErrCodeJiraNotFound          = "JIRA_ISSUE_NOT_FOUND"
	ErrCodeJiraNotEpic           = "JIRA_NOT_EPIC"
	ErrCodeJiraPlaybookRunning   = "JIRA_PLAYBOOK_RUNNING"
	ErrCodeJiraAuthFailed        = "JIRA_AUTH_FAILED"
	ErrCodeJiraRequestRejected   = "JIRA_REQUEST_REJECTED"
	ErrCodeJiraUnavailable       = "JIRA_UNAVAILABLE"
	ErrCodeJiraRateLimited       = "JIRA_RATE_LIMITED"
	ErrCodeJiraImportUnavailable = "JIRA_IMPORT_UNAVAILABLE"
)

// jiraImportRequestTimeout bounds the Jira calls a preview or an import makes
// before answering. The Jira client stops waiting out a 429 at the deadline,
// so the response beats the web client's 30 s request timeout.
const jiraImportRequestTimeout = 20 * time.Second

// JiraImporter previews Jira epics and runs their imports in the background.
type JiraImporter interface {
	Preview(ctx context.Context, epicKey string) (*jiraimport.Preview, error)
	Begin(ctx context.Context, epicKey string, req jiraimport.ImportRequest, agentID string) (string, error)
	Lookup(id string) (*jiraimport.ImportState, bool)
}

var _ JiraImporter = (*jiraimport.Service)(nil)

type jiraImportHandlers struct {
	importer JiraImporter
}

func (h *jiraImportHandlers) available(w http.ResponseWriter) bool {
	if h.importer == nil {
		writeError(w, http.StatusNotFound, ErrCodeJiraNotConfigured, "Jira import is not available",
			"configure the jira block and make sure playbooks are enabled")

		return false
	}

	return true
}

func (h *jiraImportHandlers) epicKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !h.available(w) {
		return "", false
	}

	// ParseIssueRef upper-cases a bare key; with no site URL it accepts keys only.
	key, err := jira.ParseIssueRef(r.PathValue("key"), "")
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid Jira issue key", "")

		return "", false
	}

	return key, true
}

// preview handles GET /api/jira/epics/{key}/preview.
func (h *jiraImportHandlers) preview(w http.ResponseWriter, r *http.Request) {
	key, ok := h.epicKey(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), jiraImportRequestTimeout)
	defer cancel()

	p, err := h.importer.Preview(ctx, key)
	if err != nil {
		writeJiraError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, p)
}

// importEpic handles POST /api/jira/epics/{key}/import.
func (h *jiraImportHandlers) importEpic(w http.ResponseWriter, r *http.Request) {
	key, ok := h.epicKey(w, r)
	if !ok {
		return
	}

	if isNonHumanAgent(r) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField, "forbidden", "Jira import is a human-initiated web flow")

		return
	}

	var req jiraimport.ImportRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Begin reads the epic tree before answering; the writes run in the
	// background on the service's own context, so this bound and a slow
	// import never meet.
	ctx, cancel := context.WithTimeout(r.Context(), jiraImportRequestTimeout)
	defer cancel()

	id, err := h.importer.Begin(ctx, key, req, playbookAgentID(r))
	if err != nil {
		writeJiraError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"import_id": id})
}

// getImport handles GET /api/jira/imports/{id}: the state a reconnecting or
// reopened dialog catches up from.
func (h *jiraImportHandlers) getImport(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}

	st, ok := h.importer.Lookup(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, ErrCodeJiraImportNotFound, "import not found",
			"imports are kept for an hour after they finish, on the instance that ran them")

		return
	}

	writeJSON(w, http.StatusOK, st)
}

// writeJiraError answers a failed preview or import start. Every response
// carries a fixed phrase or Jira's own bounded message, never the raw error.
func writeJiraError(w http.ResponseWriter, r *http.Request, err error) {
	authReason := jiraAuthError(err)

	switch {
	case errors.Is(err, jiraimport.ErrInvalidRequest), errors.Is(err, jira.ErrInvalidRef):
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid import request", sanitizeErrorDetails(err))
	case errors.Is(err, jiraimport.ErrNotEpic):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeJiraNotEpic, "the issue is not an epic", "")
	case errors.Is(err, jiraimport.ErrPlaybookRunning):
		writeError(w, http.StatusConflict, ErrCodeJiraPlaybookRunning,
			"the epic's playbook has an active run; stop the run or wait for it to finish, then import again",
			"an import cannot add cards to a playbook while its run is running or waiting")
	case errors.Is(err, jiraimport.ErrNotAccepting):
		writeError(w, http.StatusServiceUnavailable, ErrCodeJiraImportUnavailable, "Jira imports are not being accepted",
			"the server is shutting down or the import service has not started; try again shortly")
	case errors.Is(err, jira.ErrNotFound):
		writeError(w, http.StatusNotFound, ErrCodeJiraNotFound, "Issue not found or no permission", "")
	case authReason == jiraAuthCaptchaRequired:
		writeError(w, http.StatusBadGateway, ErrCodeJiraAuthFailed, "Jira authentication failed",
			"a login CAPTCHA is blocking basic auth on the Jira account; sign in to Jira in a browser once to clear it")
	case authReason == jiraAuthUnauthorized:
		writeError(w, http.StatusBadGateway, ErrCodeJiraAuthFailed, "Jira authentication failed", "")
	case errors.Is(err, jira.ErrRateLimited):
		writeError(w, http.StatusServiceUnavailable, ErrCodeJiraRateLimited, "Jira rate limit reached; try again shortly", "")
	case isJiraClientError(err):
		se, _ := errors.AsType[*jira.StatusError](err)
		writeError(w, http.StatusBadGateway, ErrCodeJiraRequestRejected, "Jira refused the request",
			fmt.Sprintf("Jira answered %d: %s", se.StatusCode, se.Message))
	case errors.Is(err, jiraimport.ErrJiraUnavailable):
		ctxlog.Logger(r.Context()).Warn("jira request failed", "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeJiraUnavailable, "Jira is unavailable", "")
	default:
		handleServiceError(w, r, err)
	}
}

// isJiraClientError reports a Jira 4xx other than 401, 404 and 429: Jira
// understood the request and refused it, so retrying will not help. Its
// Message is Jira's own bounded text and carries no request data.
func isJiraClientError(err error) bool {
	se, ok := errors.AsType[*jira.StatusError](err)

	return ok && se.StatusCode >= 400 && se.StatusCode < 500
}
