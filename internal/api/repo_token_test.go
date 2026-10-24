package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/mhersson/contextmatrix/internal/service"
)

const (
	scopedTestRepo      = "https://github.com/acme/widgets.git"
	unparseableTestRepo = "http://host.docker.internal:9999/work.git"
)

func boardConfigWithRepo(repo string) string {
	return strings.Replace(boardConfigRemoteExec, "https://github.com/example/project.git", repo, 1)
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		want   string
		wantOK bool
	}{
		{"https with .git", "https://github.com/acme/widgets.git", "widgets", true},
		{"scp-like", "git@github.com:acme/widgets.git", "widgets", true},
		{"ssh without .git", "ssh://ghe.example/acme/widgets", "widgets", true},
		{"empty", "", "", false},
		{"local path", "/srv/repos/widgets", "", false},
		{"owner only", "https://github.com/acme", "", false},
		{"single segment", "http://host.docker.internal:9999/work.git", "", false},
		{"nested path", "https://gitlab.example/g/sub/widgets", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := repoNameFromURL(tt.url)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// triggerOutcome is what a launch against a mock backend produced.
type triggerOutcome struct {
	status        int
	code          string
	payload       backend.TriggerPayload
	backendCalled bool
	workerStatus  string
}

func triggerWithProvider(t *testing.T, repo string, provider githubauth.TokenGenerator) triggerOutcome {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithRepo(repo))
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var (
		out    triggerOutcome
		called atomic.Bool
	)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)

		_ = json.NewDecoder(r.Body).Decode(&out.payload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		Backend:         backend.NewClient(mockBackend.URL, testBackendAPIKey),
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: testBackendAPIKey},
		ProviderForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return provider, "", nil
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	out.status = resp.StatusCode

	if resp.StatusCode == http.StatusConflict {
		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))

		out.code = apiErr.Code
	}

	out.backendCalled = called.Load()

	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	out.workerStatus = updated.WorkerStatus

	return out
}

// countingAppProvider is a real App provider whose GitHub API is a stub that
// counts requests.
func countingAppProvider(t *testing.T) (*githubauth.AppProvider, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"ghs_app","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
	}))
	t.Cleanup(gh.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	p, err := githubauth.NewAppProviderWithKey(123, 456, key, gh.URL)
	require.NoError(t, err)

	return p, &hits
}

func TestTrigger_MintsTokenScopedToProjectRepo(t *testing.T) {
	provider := &fakeTokenProvider{token: "ghs_scoped"}

	out := triggerWithProvider(t, scopedTestRepo, provider)

	assert.Equal(t, http.StatusAccepted, out.status)
	assert.Equal(t, "ghs_scoped", out.payload.GitToken)

	provider.mu.Lock()
	defer provider.mu.Unlock()

	assert.Equal(t, "widgets", provider.lastRepo)
	assert.Equal(t, int32(1), provider.repoCalls.Load())
	assert.Zero(t, provider.calls.Load(), "a worker token must never come from the broad mint")
}

func TestTrigger_ScopedMintFailureRefusesLaunch(t *testing.T) {
	provider := &fakeTokenProvider{err: assert.AnError}

	out := triggerWithProvider(t, scopedTestRepo, provider)

	assert.Equal(t, http.StatusConflict, out.status)
	assert.Equal(t, ErrCodeValidationError, out.code)
	assert.False(t, out.backendCalled)
	assert.Equal(t, "failed", out.workerStatus)
	assert.Zero(t, provider.calls.Load(), "failed scoped mint must not fall back to the broad token")
}

func TestTrigger_PATWithUnparseableRepoStillLaunches(t *testing.T) {
	provider, err := githubauth.NewPATProvider("pat-token")
	require.NoError(t, err)

	out := triggerWithProvider(t, unparseableTestRepo, provider)

	assert.Equal(t, http.StatusAccepted, out.status)
	assert.Equal(t, "pat-token", out.payload.GitToken)
}

func TestTrigger_AppWithUnparseableRepoFailsClosed(t *testing.T) {
	provider, hits := countingAppProvider(t)

	out := triggerWithProvider(t, unparseableTestRepo, provider)

	assert.Equal(t, http.StatusConflict, out.status)
	assert.Equal(t, ErrCodeValidationError, out.code)
	assert.False(t, out.backendCalled)
	assert.Equal(t, "failed", out.workerStatus)
	assert.Zero(t, hits.Load(), "no GitHub request may be made for an unparseable repo")
}

func gitCredentialsGET(t *testing.T, repo string, provider githubauth.TokenGenerator) (int, map[string]string) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithRepo(repo))
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Long run", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "test setup")
	require.NoError(t, err)

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		Backend:         backend.NewClient("http://localhost:9090", testBackendAPIKey),
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: testBackendAPIKey},
		ProviderForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return provider, "", nil
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + card.ID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	var body map[string]string

	_ = json.NewDecoder(resp.Body).Decode(&body)

	return resp.StatusCode, body
}

func TestGitCredentials_MintsTokenScopedToProjectRepo(t *testing.T) {
	provider := &fakeTokenProvider{token: "ghs_scoped"}

	status, body := gitCredentialsGET(t, scopedTestRepo, provider)

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ghs_scoped", body["token"])

	provider.mu.Lock()
	defer provider.mu.Unlock()

	assert.Equal(t, "widgets", provider.lastRepo)
	assert.Zero(t, provider.calls.Load(), "a refreshed worker token must never come from the broad mint")
}

func TestGitCredentials_ScopedMintFailureIs502(t *testing.T) {
	provider := &fakeTokenProvider{err: assert.AnError}

	status, _ := gitCredentialsGET(t, scopedTestRepo, provider)

	assert.Equal(t, http.StatusBadGateway, status)
	assert.Zero(t, provider.calls.Load(), "failed scoped mint must not fall back to the broad token")
}

func TestGitCredentials_PATWithUnparseableRepoReturnsPAT(t *testing.T) {
	provider, err := githubauth.NewPATProvider("pat-token")
	require.NoError(t, err)

	status, body := gitCredentialsGET(t, unparseableTestRepo, provider)

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "pat-token", body["token"])
}

func TestGitCredentials_AppWithUnparseableRepoFailsClosed(t *testing.T) {
	provider, hits := countingAppProvider(t)

	status, _ := gitCredentialsGET(t, unparseableTestRepo, provider)

	assert.Equal(t, http.StatusBadGateway, status)
	assert.Zero(t, hits.Load())
}

func TestWorkerDeniedRepoNames(t *testing.T) {
	got := workerDeniedRepoNames([]string{
		"https://github.com/acme/Boards.git",
		"git@github.com:acme/task-skills.git",
		"",
		"::not-a-url::",
		"https://github.com/onlyowner",
	})

	assert.Equal(t, map[string]bool{"boards": true, "task-skills": true}, got)
}
