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

	image := "ghcr.io/org/worker:latest"

	body := validUpdateProjectBody(nil)
	body.RemoteExecution = &remoteExecutionUpdate{WorkerImage: &image}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	require.NotNil(t, cfg.RemoteExecution)
	assert.Equal(t, "ghcr.io/org/worker:latest", cfg.RemoteExecution.WorkerImage)
}

func TestUpdateProject_RemoteExecution_OmittedPreserves(t *testing.T) {
	server := noneModeServer(t)

	image := "ghcr.io/org/worker:latest"

	setBody := validUpdateProjectBody(nil)
	setBody.RemoteExecution = &remoteExecutionUpdate{WorkerImage: &image}

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

func TestUpdateProject_ChatWorkerImage_RoundTrip(t *testing.T) {
	server := noneModeServer(t)

	image := "contextmatrix-chat-worker:go-node"

	body := validUpdateProjectBody(nil)
	body.RemoteExecution = &remoteExecutionUpdate{ChatWorkerImage: &image}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	require.NotNil(t, cfg.RemoteExecution,
		"a lone chat_worker_image must survive zero-value normalization")
	assert.Equal(t, "contextmatrix-chat-worker:go-node", cfg.RemoteExecution.ChatWorkerImage)
	assert.Empty(t, cfg.RemoteExecution.WorkerImage)
}

func TestUpdateProject_ChatWorkerImage_OmittedPreservesAndEmptyClears(t *testing.T) {
	server := noneModeServer(t)

	image := "contextmatrix-chat-worker:go-node"
	setBody := validUpdateProjectBody(nil)
	setBody.RemoteExecution = &remoteExecutionUpdate{ChatWorkerImage: &image}

	resp := putProject(t, server.URL, nil, setBody)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	closeBody(t, resp.Body)

	// Omitting chat_worker_image (nil pointer) preserves it.
	worker := "ghcr.io/org/worker:latest"
	otherBody := validUpdateProjectBody(nil)
	otherBody.RemoteExecution = &remoteExecutionUpdate{WorkerImage: &worker}

	resp = putProject(t, server.URL, nil, otherBody)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// A fresh decode target per response: chat_worker_image and worker_image
	// both carry "omitempty" on the wire, so an empty value is an absent key,
	// not an explicit reset - decoding into a var already populated from an
	// earlier response would silently keep its stale value for the omitted key.
	var afterPreserve board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&afterPreserve))
	closeBody(t, resp.Body)
	assert.Equal(t, "contextmatrix-chat-worker:go-node", afterPreserve.RemoteExecution.ChatWorkerImage,
		"omitting chat_worker_image must preserve it")
	assert.Equal(t, "ghcr.io/org/worker:latest", afterPreserve.RemoteExecution.WorkerImage)

	// A non-nil "" clears it.
	empty := ""
	clearBody := validUpdateProjectBody(nil)
	clearBody.RemoteExecution = &remoteExecutionUpdate{ChatWorkerImage: &empty}

	resp = putProject(t, server.URL, nil, clearBody)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var afterClear board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&afterClear))
	closeBody(t, resp.Body)
	assert.Empty(t, afterClear.RemoteExecution.ChatWorkerImage)
	assert.Equal(t, "ghcr.io/org/worker:latest", afterClear.RemoteExecution.WorkerImage,
		"clearing chat_worker_image must not touch worker_image")
}

func TestUpdateProject_ChatWorkerImage_InvalidCharactersRejected(t *testing.T) {
	server := noneModeServer(t)

	image := "bad image!"
	body := validUpdateProjectBody(nil)
	body.RemoteExecution = &remoteExecutionUpdate{ChatWorkerImage: &image}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}
