package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// mockSyncer implements Syncer for testing.
type mockSyncer struct {
	triggerErr error
	statuses   []gitsync.SyncStatus
	lastRepo   string
}

func (m *mockSyncer) TriggerSync(_ context.Context, repo string) error {
	m.lastRepo = repo

	return m.triggerErr
}

func (m *mockSyncer) Statuses() []gitsync.SyncStatus {
	return m.statuses
}

// --- POST /api/sync ---

func TestTriggerSync_Enabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		statuses: []gitsync.SyncStatus{{Repo: "boards", Enabled: true}},
	}

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var statuses []gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statuses))
	require.Len(t, statuses, 1)
	assert.True(t, statuses[0].Enabled)
}

func TestTriggerSync_Disabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// No syncer → sync disabled.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: nil})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeSyncDisabled, apiErr.Code)
}

func TestTriggerSync_Error(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		triggerErr: errors.New("rebase conflict"),
		statuses:   []gitsync.SyncStatus{{Repo: "boards", Enabled: true, LastSyncError: "rebase conflict"}},
	}

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeSyncError, apiErr.Code)
	assert.Contains(t, apiErr.Details, "rebase conflict")
}

func TestTriggerSync_RepoQuery(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{statuses: []gitsync.SyncStatus{{Repo: "team", Enabled: true}, {Repo: "private"}}}
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync?repo=team", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "team", syncer.lastRepo)

	var statuses []gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statuses))
	require.Len(t, statuses, 2)
	assert.Equal(t, "private", statuses[1].Repo)
}

func TestTriggerSync_UnknownRepoIsBadRequest(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{triggerErr: fmt.Errorf("%w: %q", storage.ErrUnknownRepo, "nope")}
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync?repo=nope", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

func TestTriggerSync_DisabledRepoIs503(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{triggerErr: fmt.Errorf("private: %w", gitsync.ErrSyncDisabled)}
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync?repo=private", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeSyncDisabled, apiErr.Code)
}

// --- GET /api/sync ---

func TestGetSyncStatus_Enabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		statuses: []gitsync.SyncStatus{{Repo: "boards", Enabled: true, Syncing: false}},
	}

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/sync")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var statuses []gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statuses))
	require.Len(t, statuses, 1)
	assert.True(t, statuses[0].Enabled)
	assert.False(t, statuses[0].Syncing)
}

func TestGetSyncStatus_Disabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// No syncer → disabled.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: nil})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/sync")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var statuses []gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statuses))
	assert.Empty(t, statuses)
}

func TestGetSyncStatus_NoSyncerIsAnEmptyList(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/sync")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(body))
}

// --- POST /api/projects/{project}/recalculate-costs ---

func TestRecalculateCosts_Success(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"default_model":"claude-sonnet-4-6"}`
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/recalculate-costs",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result recalculateCostsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	// With no cards that need recalculation, both fields should be zero.
	assert.Equal(t, 0, result.CardsUpdated)
	assert.InDelta(t, 0.0, result.TotalCostRecalculated, 0.0001)
}

func TestRecalculateCosts_MissingModel(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{}`
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/recalculate-costs",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

func TestRecalculateCosts_InvalidJSON(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/recalculate-costs",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

func TestRecalculateCosts_ProjectNotFound(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"default_model":"claude-sonnet-4-6"}`
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/nonexistent/recalculate-costs",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
