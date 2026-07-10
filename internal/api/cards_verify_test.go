package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// TestCreateCardVerify_HumanOnlyGate covers the create human-only gate: an agent
// setting verify is rejected 403, while a human (no X-Agent-ID) is allowed.
func TestCreateCardVerify_HumanOnlyGate(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	createAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	body := `{"title":"verify card","type":"task","priority":"medium","verify":{"command":"make test"}}`

	t.Run("agent setting verify is rejected", func(t *testing.T) {
		resp := createAs(t, body, "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "verify")
	})

	t.Run("human setting verify is allowed", func(t *testing.T) {
		resp := createAs(t, body, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		require.NotNil(t, card.Verify)
		assert.Equal(t, "make test", card.Verify.Command)
	})

	t.Run("agent creating without verify is allowed", func(t *testing.T) {
		resp := createAs(t, `{"title":"plain","type":"task","priority":"medium"}`, "agent:x")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}

// TestPatchCardVerify_HumanOnlyGate covers the patch human-only gate: an agent
// setting verify is rejected 403, a human is allowed, and an invalid verify
// from a human returns 422.
func TestPatchCardVerify_HumanOnlyGate(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "patch verify gate", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	patchAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	t.Run("agent setting verify is rejected", func(t *testing.T) {
		resp := patchAs(t, `{"verify":{"command":"make test"}}`, "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "verify")
	})

	t.Run("human setting verify is allowed", func(t *testing.T) {
		resp := patchAs(t, `{"verify":{"command":"make test","timeout_seconds":300}}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		require.NotNil(t, updated.Verify)
		assert.Equal(t, "make test", updated.Verify.Command)
		assert.Equal(t, 300, updated.Verify.TimeoutSeconds)
	})

	t.Run("human setting invalid verify returns 422", func(t *testing.T) {
		resp := patchAs(t, `{"verify":{"command":"make test","env":["API_KEY"]}}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})
}
