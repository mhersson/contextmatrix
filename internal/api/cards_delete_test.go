package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteCard_LiveWorkerIs409: the refusal is a WORKER_CONFLICT, and
// stopping the worker is the remedy.
func TestDeleteCard_LiveWorkerIs409(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(newTestRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "Queued", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "queued", "task queued for worker")
	require.NoError(t, err)

	url := server.URL + "/api/projects/test-project/cards/" + card.ID

	resp := doJSON(t, http.MethodDelete, url, nil, "human:alice")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeWorkerConflict, apiErr.Code)

	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "killed", "task stopped by user")
	require.NoError(t, err)

	again := doJSON(t, http.MethodDelete, url, nil, "human:alice")
	defer closeBody(t, again.Body)

	assert.Equal(t, http.StatusNoContent, again.StatusCode)
}

// TestDeleteCard_FinishedCardWithLiveWorkerIs204: a done card's worker is
// being reaped, so the delete goes through.
func TestDeleteCard_FinishedCardWithLiveWorkerIs204(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(newTestRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "Finished", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)
	_, err = svc.TransitionTo(ctx, "test-project", card.ID, "done")
	require.NoError(t, err)

	resp := doJSON(t, http.MethodDelete, server.URL+"/api/projects/test-project/cards/"+card.ID, nil, "human:alice")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
