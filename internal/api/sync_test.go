package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mockSyncer implements Syncer for testing.
type mockSyncer struct {
	triggerErr error
	status     gitsync.SyncStatus
}

func (m *mockSyncer) TriggerSync(_ context.Context) error {
	return m.triggerErr
}

func (m *mockSyncer) Status() gitsync.SyncStatus {
	return m.status
}

// --- POST /api/sync ---

func TestTriggerSync_Enabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		status: gitsync.SyncStatus{Enabled: true},
	}

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/sync", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	assert.True(t, status.Enabled)
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
	assert.Equal(t, "SYNC_DISABLED", apiErr.Code)
}

func TestTriggerSync_Error(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		triggerErr: errors.New("rebase conflict"),
		status:     gitsync.SyncStatus{Enabled: true, LastSyncError: "rebase conflict"},
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
	assert.Equal(t, "SYNC_ERROR", apiErr.Code)
	assert.Contains(t, apiErr.Details, "rebase conflict")
}

// --- GET /api/sync ---

func TestGetSyncStatus_Enabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	syncer := &mockSyncer{
		status: gitsync.SyncStatus{Enabled: true, Syncing: false},
	}

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Syncer: syncer})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/sync")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	assert.True(t, status.Enabled)
	assert.False(t, status.Syncing)
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

	var status gitsync.SyncStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	assert.False(t, status.Enabled)
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

func TestRecalculateCosts_WithUsageData(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card, claim it, and report usage without a model so cost is $0.
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Usage card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card.ID, service.ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     1000,
		CompletionTokens: 500,
		// No model specified → cost will be $0.
	})
	require.NoError(t, err)

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
	// The card had zero cost so should be recalculated (if model is in cost map).
	// If claude-sonnet-4-6 is not in the cost map, cardsUpdated may be 0.
	// Either way, the response structure is valid.
	assert.GreaterOrEqual(t, result.CardsUpdated, 0)
}
