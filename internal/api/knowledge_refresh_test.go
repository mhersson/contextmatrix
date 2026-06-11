package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/refresh"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// testSetupWithRepo creates a test environment with a project that has a "core" repo configured.
// This is required for trigger tests that need to resolve the repo URL.
func testSetupWithRepo(t *testing.T) (*service.CardService, *events.Bus, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	boardConfig := `name: test-project
prefix: TEST
next_id: 1
repo: https://github.com/example/core.git
repos:
  - name: core
    url: https://github.com/example/core.git
    primary: true
states: [todo, in_progress, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	require.NoError(t, git.CommitFile(context.Background(), "test-project/.board.yaml", "init: seed boards repo"))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return svc, bus, func() {}
}

func TestKnowledgeRefreshAPI_GetPlan_ReturnsJSON(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge/core/refresh-plan")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "items")
}

func TestKnowledgeRefreshAPI_Trigger_RejectsMissingAgentID(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestKnowledgeRefreshAPI_Trigger_RejectsNonHumanAgent(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-foo")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestKnowledgeRefreshAPI_Trigger_RejectsBareHumanPrefix(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"bare \"human:\" agent ID must be rejected — auditing the literal prefix is meaningless")
}

func TestKnowledgeRefreshAPI_Trigger_RejectsWhenRunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// Runner is nil in this RouterConfig.
	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus, RefreshRegistry: refresh.NewRegistry()}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:test")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestKnowledgeRefreshAPI_Trigger_HappyPath(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	// Stub runner that returns 200 OK to /refresh-knowledge
	var receivedPayload runner.RefreshKnowledgePayload

	stubRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubRunner.Close()

	runnerClient := runner.NewClient(stubRunner.URL, "test-key")
	reg := refresh.NewRegistry()

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:            svc,
		Bus:                bus,
		Runner:             runnerClient,
		KnowledgeRefresher: runnerClient,
		RefreshRegistry:    reg,
	}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{"overwrite_docs":["api-documentation.md"]}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:web-aaa")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode, "response: %s", readBody(resp))

	// The lock should be held; the registry should have the job.
	snap := reg.Snapshot("test-project")
	job, ok := snap["core"]
	require.True(t, ok)
	assert.Equal(t, "human:web-aaa", job.AgentID)
	assert.Equal(t, "test-project", receivedPayload.Project)
	assert.Equal(t, "core", receivedPayload.Repo)
	assert.Equal(t, []string{"api-documentation.md"}, receivedPayload.OverwriteDocs)
}

func TestKnowledgeRefreshAPI_Trigger_409OnDuplicate(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	stubRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubRunner.Close()

	reg := refresh.NewRegistry()
	// Pre-acquire so a real trigger collides.
	_, err := reg.Acquire("test-project", "core", "human:other")
	require.NoError(t, err)

	rc := runner.NewClient(stubRunner.URL, "test-key")

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:            svc,
		Bus:                bus,
		Runner:             rc,
		KnowledgeRefresher: rc,
		RefreshRegistry:    reg,
	}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:web-aaa")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestKnowledgeRefreshAPI_Trigger_RejectsInvalidOverwriteDoc(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	stubRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubRunner.Close()

	rc := runner.NewClient(stubRunner.URL, "test-key")

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:            svc,
		Bus:                bus,
		Runner:             rc,
		KnowledgeRefresher: rc,
		RefreshRegistry:    refresh.NewRegistry(),
	}))
	defer server.Close()

	body := `{"repo":"core","overwrite_docs":["not-a-real-doc.md"]}`
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(body))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:web-test1234")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body2, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body2), "overwrite_docs")
}

func TestKnowledgeRefreshAPI_Trigger_RejectsTooManyOverwriteDocs(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	stubRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubRunner.Close()

	rc := runner.NewClient(stubRunner.URL, "test-key")

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:            svc,
		Bus:                bus,
		Runner:             rc,
		KnowledgeRefresher: rc,
		RefreshRegistry:    refresh.NewRegistry(),
	}))
	defer server.Close()

	docs := make([]string, 0, len(board.KnowledgeDocNames)*2)
	for _, d := range board.KnowledgeDocNames {
		docs = append(docs, d, d) // duplicate to exceed cap
	}

	payload, err := json.Marshal(map[string]any{"repo": "core", "overwrite_docs": docs})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/knowledge/core/refresh",
		strings.NewReader(string(payload)))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:web-test1234")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestKnowledgeRefreshAPI_Status_EmptyWhenIdle(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge/refresh-status")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"repos"`)
}

func TestKnowledgeRefreshAPI_Status_ReportsAcquiredJob(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	reg := refresh.NewRegistry()
	_, err := reg.Acquire("test-project", "core", "human:web-aaa")
	require.NoError(t, err)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus, RefreshRegistry: reg}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/knowledge/refresh-status")
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"core"`)
	assert.Contains(t, bodyStr, `"planning"`)
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)

	return string(b)
}

// signedRunnerRequest builds an HMAC-signed POST request matching the scheme
// used by runner callbacks: method + "\n" + uri + "\n" + ts + "." + body.
// Uses protocol.SignRequestHeaders so the signing logic stays in one place.
func signedRunnerRequest(t *testing.T, baseURL, apiKey, path string, body []byte) *http.Request {
	t.Helper()

	sigHeader, tsHeader := protocol.SignRequestHeaders(apiKey, http.MethodPost, path, body)

	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	return req
}

func TestRunnerKnowledgeStatus_AcceptsValidSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	reg := refresh.NewRegistry()
	_, err := reg.Acquire("test-project", "core", "human:test")
	require.NoError(t, err)
	require.NoError(t, reg.MarkRunning("test-project", "core", 4))
	require.NoError(t, reg.MarkCommitted("test-project", "core", "abc"))

	apiKey := "test-runner-secret"

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:         svc,
		Bus:             bus,
		Runner:          runner.NewClient("http://unused", apiKey),
		BackendCfg:      config.BackendConfig{APIKey: apiKey, CallbackPath: "/api/runner"},
		RefreshRegistry: reg,
	}))
	defer server.Close()

	body := []byte(`{"project":"test-project","repo":"core","state":"succeeded"}`)
	req := signedRunnerRequest(t, server.URL, apiKey, "/api/runner/knowledge-status", body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", readBody(resp))

	snap := reg.Snapshot("test-project")
	assert.Equal(t, refresh.StateSucceeded, snap["core"].State)
}

func TestRunnerKnowledgeStatus_FailedWithoutCommitMarksFailed(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	reg := refresh.NewRegistry()
	_, err := reg.Acquire("test-project", "core", "human:test")
	require.NoError(t, err)
	require.NoError(t, reg.MarkRunning("test-project", "core", 4))
	// Note: NO MarkCommitted call — commit_knowledge_docs side effect never fired.

	apiKey := "test-runner-secret"

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:         svc,
		Bus:             bus,
		Runner:          runner.NewClient("http://unused", apiKey),
		BackendCfg:      config.BackendConfig{APIKey: apiKey, CallbackPath: "/api/runner"},
		RefreshRegistry: reg,
	}))
	defer server.Close()

	body := []byte(`{"project":"test-project","repo":"core","state":"succeeded"}`)
	req := signedRunnerRequest(t, server.URL, apiKey, "/api/runner/knowledge-status", body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	snap := reg.Snapshot("test-project")
	assert.Equal(t, refresh.StateFailed, snap["core"].State,
		"runner reported succeeded but no commit landed -> Failed")
}

func TestRunnerKnowledgeStatus_RejectsMissingSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRepo(t)
	defer cleanup()

	reg := refresh.NewRegistry()
	apiKey := "test-runner-secret"

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:         svc,
		Bus:             bus,
		Runner:          runner.NewClient("http://unused", apiKey),
		BackendCfg:      config.BackendConfig{APIKey: apiKey, CallbackPath: "/api/runner"},
		RefreshRegistry: reg,
	}))
	defer server.Close()

	body := []byte(`{"project":"test-project","repo":"core","state":"succeeded"}`)
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/runner/knowledge-status",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
