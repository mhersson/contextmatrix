package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noneModeServer builds a none-mode router over the "test-project" fixture so
// the remote_execution PUT path can be exercised without auth. The PUT handler
// echoes back the raw stored config (not the effective one), so response
// assertions reflect what actually landed in .board.yaml.
func noneModeServer(t *testing.T) *httptest.Server {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server
}

func TestUpdateProject_RemoteExecution_RoundTrip(t *testing.T) {
	server := noneModeServer(t)

	enabled := true
	image := "ghcr.io/org/worker:latest"

	body := validUpdateProjectBody(nil)
	body.RemoteExecution = &remoteExecutionUpdate{Enabled: &enabled, WorkerImage: &image}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	require.NotNil(t, cfg.RemoteExecution)
	require.NotNil(t, cfg.RemoteExecution.Enabled)
	assert.True(t, *cfg.RemoteExecution.Enabled)
	assert.Equal(t, "ghcr.io/org/worker:latest", cfg.RemoteExecution.WorkerImage)
}

func TestUpdateProject_RemoteExecution_OmittedPreserves(t *testing.T) {
	server := noneModeServer(t)

	enabled := true
	image := "ghcr.io/org/worker:latest"

	setBody := validUpdateProjectBody(nil)
	setBody.RemoteExecution = &remoteExecutionUpdate{Enabled: &enabled, WorkerImage: &image}

	resp := putProject(t, server.URL, nil, setBody)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	closeBody(t, resp.Body)

	// A PUT that omits remote_execution entirely must leave the existing config
	// untouched.
	resp = putProject(t, server.URL, nil, validUpdateProjectBody(nil))
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	require.NotNil(t, cfg.RemoteExecution, "omitting remote_execution must preserve it")
	assert.Equal(t, "ghcr.io/org/worker:latest", cfg.RemoteExecution.WorkerImage)
}

func TestUpdateProject_RemoteExecution_InvalidImage422(t *testing.T) {
	server := noneModeServer(t)

	bad := "ghcr.io/org/worker latest" // embedded space is rejected

	body := validUpdateProjectBody(nil)
	body.RemoteExecution = &remoteExecutionUpdate{WorkerImage: &bad}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
}
