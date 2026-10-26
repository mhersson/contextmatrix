package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/jiraimport"
)

type fakeImporter struct {
	err      error
	gotKey   string
	gotReq   jiraimport.ImportRequest
	gotAgent string
	// deadlines records the deadline each Preview and Begin call saw; the
	// zero time when the context had none.
	deadlines []time.Time
}

func (f *fakeImporter) recordDeadline(ctx context.Context) {
	d, _ := ctx.Deadline()
	f.deadlines = append(f.deadlines, d)
}

func (f *fakeImporter) Preview(ctx context.Context, key string) (*jiraimport.Preview, error) {
	f.recordDeadline(ctx)

	f.gotKey = key
	if f.err != nil {
		return nil, f.err
	}

	return &jiraimport.Preview{Epic: jiraimport.Epic{Key: key}, Children: []jiraimport.Node{}}, nil
}

func (f *fakeImporter) Begin(ctx context.Context, key string, req jiraimport.ImportRequest, agentID string) (string, error) {
	f.recordDeadline(ctx)

	f.gotKey, f.gotReq, f.gotAgent = key, req, agentID
	if f.err != nil {
		return "", f.err
	}

	return "ji-0000000000000001", nil
}

func (f *fakeImporter) Lookup(id string) (*jiraimport.ImportState, bool) {
	if id != "ji-0000000000000001" {
		return nil, false
	}

	return &jiraimport.ImportState{
		ID: id, Epic: "PROJ-1", Status: jiraimport.ImportDone, PlaybookID: "proj-1-checkout", Total: 1,
		Results: []jiraimport.Result{{Key: "PROJ-2", Status: jiraimport.StatusCreated, Project: "test-project", Card: "TEST-001"}},
	}, true
}

func jiraServer(t *testing.T, importer JiraImporter) *httptest.Server {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	cfg := RouterConfig{Service: svc, Bus: bus}
	if importer != nil {
		cfg.JiraImport = importer
	}

	server := httptest.NewServer(newTestRouter(cfg))
	t.Cleanup(server.Close)

	return server
}

func decodeAPIError(t *testing.T, resp *http.Response) APIError {
	t.Helper()

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))

	return apiErr
}

func importBody() map[string]any {
	return map[string]any{
		"selections": []map[string]any{{"key": "PROJ-2", "project": "test-project"}},
		"vetted":     true, "include_epic_context": true,
		"playbook": map[string]any{"title": "PROJ-1 Checkout", "description": "Checkout"},
	}
}

func TestJiraImportAPI_NotConfigured(t *testing.T) {
	server := jiraServer(t, nil)

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/api/jira/epics/PROJ-1/preview"},
		{http.MethodPost, "/api/jira/epics/PROJ-1/import"},
		{http.MethodGet, "/api/jira/imports/ji-0000000000000001"},
	} {
		resp := doJSON(t, req.method, server.URL+req.path, map[string]any{}, "human:alice")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, req.path)
		assert.Equal(t, ErrCodeJiraNotConfigured, decodeAPIError(t, resp).Code)
		closeBody(t, resp.Body)
	}
}

func TestJiraImportAPI_PreviewImportAndState(t *testing.T) {
	f := &fakeImporter{}
	server := jiraServer(t, f)

	resp := doJSON(t, http.MethodGet, server.URL+"/api/jira/epics/PROJ-1/preview", nil, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "PROJ-1", f.gotKey)
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodGet, server.URL+"/api/jira/epics/proj-1/preview", nil, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "PROJ-1", f.gotKey, "a lowercase key is upper-cased like jira.ParseIssueRef")
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodGet, server.URL+"/api/jira/epics/not-a-key/preview", nil, "")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, ErrCodeBadRequest, decodeAPIError(t, resp).Code)
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodPost, server.URL+"/api/jira/epics/PROJ-1/import", importBody(), "agent-1")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "import is human-only")
	assert.Equal(t, ErrCodeHumanOnlyField, decodeAPIError(t, resp).Code)
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodPost, server.URL+"/api/jira/epics/PROJ-1/import", importBody(), "human:alice")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var started struct {
		ImportID string `json:"import_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&started))
	closeBody(t, resp.Body)
	assert.Equal(t, "ji-0000000000000001", started.ImportID)
	assert.Equal(t, "human:alice", f.gotAgent)
	assert.True(t, f.gotReq.Vetted)
	assert.True(t, f.gotReq.IncludeEpicContext)
	assert.Equal(t, []jiraimport.Selection{{Key: "PROJ-2", Project: "test-project"}}, f.gotReq.Selections)
	assert.Equal(t, "PROJ-1 Checkout", f.gotReq.Playbook.Title)

	resp = doJSON(t, http.MethodGet, server.URL+"/api/jira/imports/"+started.ImportID, nil, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var st jiraimport.ImportState
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&st))
	closeBody(t, resp.Body)
	assert.Equal(t, jiraimport.ImportDone, st.Status)
	assert.Equal(t, "proj-1-checkout", st.PlaybookID)

	resp = doJSON(t, http.MethodGet, server.URL+"/api/jira/imports/ji-ffffffffffffffff", nil, "")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, ErrCodeJiraImportNotFound, decodeAPIError(t, resp).Code)
}

func TestJiraImportAPI_ImportWithoutIdentityRunsAsWebHuman(t *testing.T) {
	f := &fakeImporter{}
	server := jiraServer(t, f)

	resp := doJSON(t, http.MethodPost, server.URL+"/api/jira/epics/PROJ-1/import", importBody(), "")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "human:web", f.gotAgent)
}

func TestJiraImportAPI_MalformedImportBody(t *testing.T) {
	f := &fakeImporter{}
	server := jiraServer(t, f)

	resp := doJSON(t, http.MethodPost, server.URL+"/api/jira/epics/PROJ-1/import", []int{1}, "human:alice")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, ErrCodeBadRequest, decodeAPIError(t, resp).Code)
	assert.Empty(t, f.gotKey, "nothing reaches the importer")
}

// A 429 wait in the Jira client stops at the context deadline, so preview and
// the synchronous part of an import must answer before the web's own timeout.
func TestJiraImportAPI_BoundsTheJiraCalls(t *testing.T) {
	f := &fakeImporter{}
	server := jiraServer(t, f)

	before := time.Now()

	resp := doJSON(t, http.MethodGet, server.URL+"/api/jira/epics/PROJ-1/preview", nil, "")
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodPost, server.URL+"/api/jira/epics/PROJ-1/import", importBody(), "human:alice")
	closeBody(t, resp.Body)

	require.Len(t, f.deadlines, 2)

	for i, d := range f.deadlines {
		require.False(t, d.IsZero(), "call %d has no deadline", i)
		assert.LessOrEqual(t, d.Sub(before), jiraImportRequestTimeout+time.Second, "call %d", i)
	}
}

func TestJiraImportAPI_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		// importOnly marks errors only Begin returns.
		importOnly bool
		// details, when set, must appear in the response details.
		details string
		// hidden must not appear anywhere in the response.
		hidden string
	}{
		{name: "not an epic", err: fmt.Errorf("x: %w", jiraimport.ErrNotEpic), status: http.StatusUnprocessableEntity, code: ErrCodeJiraNotEpic},
		{name: "not found", err: fmt.Errorf("x: %w", jira.ErrNotFound), status: http.StatusNotFound, code: ErrCodeJiraNotFound},
		{name: "unauthorized", err: fmt.Errorf("x: %w", &jira.AuthError{}), status: http.StatusBadGateway, code: ErrCodeJiraAuthFailed},
		{name: "captcha", err: fmt.Errorf("x: %w", captchaErr()), status: http.StatusBadGateway, code: ErrCodeJiraAuthFailed},
		{name: "rate limited", err: fmt.Errorf("x: %w", &jira.RateLimitError{RetryAfter: time.Minute}), status: http.StatusServiceUnavailable, code: ErrCodeJiraRateLimited},
		{
			name:   "unavailable",
			err:    fmt.Errorf("x: %w: %w", jiraimport.ErrJiraUnavailable, errors.New("dial tcp 10.0.0.7:443")),
			status: http.StatusBadGateway, code: ErrCodeJiraUnavailable, hidden: "10.0.0.7",
		},
		{
			name:   "jira 4xx",
			err:    fmt.Errorf("x: %w: %w", jiraimport.ErrJiraUnavailable, &jira.StatusError{StatusCode: 400, Message: "bad jql"}),
			status: http.StatusBadGateway, code: ErrCodeJiraRequestRejected, details: "bad jql",
		},
		{
			name:   "jira 5xx",
			err:    fmt.Errorf("x: %w: %w", jiraimport.ErrJiraUnavailable, &jira.StatusError{StatusCode: 503, Message: "down"}),
			status: http.StatusBadGateway, code: ErrCodeJiraUnavailable,
		},
		{name: "invalid request", err: fmt.Errorf("%w: select at least one issue", jiraimport.ErrInvalidRequest), status: http.StatusBadRequest, code: ErrCodeBadRequest},
		{
			name:   "playbook running",
			err:    fmt.Errorf("%w: stop the run of playbook %q", jiraimport.ErrPlaybookRunning, "Secret rollout"),
			status: http.StatusConflict, code: ErrCodeJiraPlaybookRunning, importOnly: true, hidden: "Secret rollout",
		},
		{
			name:   "not accepting",
			err:    fmt.Errorf("begin: %w", jiraimport.ErrNotAccepting),
			status: http.StatusServiceUnavailable, code: ErrCodeJiraImportUnavailable, importOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := jiraServer(t, &fakeImporter{err: tt.err})

			type call struct {
				method, path string
				body         any
			}

			calls := []call{{http.MethodPost, "/api/jira/epics/PROJ-1/import", importBody()}}
			if !tt.importOnly {
				calls = append(calls, call{http.MethodGet, "/api/jira/epics/PROJ-1/preview", nil})
			}

			for _, c := range calls {
				resp := doJSON(t, c.method, server.URL+c.path, c.body, "human:alice")

				assert.Equal(t, tt.status, resp.StatusCode, c.path)

				got := decodeAPIError(t, resp)
				closeBody(t, resp.Body)

				assert.Equal(t, tt.code, got.Code, c.path)

				if tt.details != "" {
					assert.Contains(t, got.Details, tt.details, c.path)
				}

				if tt.hidden != "" {
					assert.NotContains(t, got.Error+got.Details, tt.hidden, c.path)
				}
			}
		})
	}
}

func TestJiraImportAPI_CaptchaIsNamedInDetails(t *testing.T) {
	plain := jiraServer(t, &fakeImporter{err: fmt.Errorf("x: %w", &jira.AuthError{})})
	captcha := jiraServer(t, &fakeImporter{err: fmt.Errorf("x: %w", captchaErr())})

	resp := doJSON(t, http.MethodGet, plain.URL+"/api/jira/epics/PROJ-1/preview", nil, "")
	plainErr := decodeAPIError(t, resp)
	closeBody(t, resp.Body)

	resp = doJSON(t, http.MethodGet, captcha.URL+"/api/jira/epics/PROJ-1/preview", nil, "")
	captchaErr := decodeAPIError(t, resp)
	closeBody(t, resp.Body)

	assert.Empty(t, plainErr.Details)
	assert.NotEmpty(t, captchaErr.Details)
}
