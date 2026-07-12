package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
)

// TestCreateCardBestOfNAndCoop covers the create-time acceptance of the
// best_of_n and co-op fields: valid values are persisted, invalid values
// return 400, and non-human callers setting any of them get 403
// HUMAN_ONLY_FIELD. Mirrors the PATCH/PUT coverage in
// TestPatchCardBestOfN / TestPatchCardCoop_ValidationMatrix.
func TestCreateCardBestOfNAndCoop(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{
		Service: svc,
		Bus:     bus,
		BestOfN: config.BestOfNConfig{MaxCandidates: 5},
		Coop:    coopTestConfig(),
	})

	server := httptest.NewServer(router)
	defer server.Close()

	postAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost,
			server.URL+"/api/projects/test-project/cards", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	t.Run("create with best_of_n=3 persists the value", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:    "Best of N create",
			Type:     "task",
			Priority: "medium",
			BestOfN:  3,
		})

		resp := postAs(t, string(body), "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, 3, card.BestOfN)
	})

	t.Run("create with co-op fields persists the values", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:            "Co-op create",
			Type:             "task",
			Priority:         "medium",
			CoopParticipants: 3,
			CoopPhases:       []string{"plan", "review"},
			CoopGuests:       []string{"laptop"},
		})

		resp := postAs(t, string(body), "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, 3, card.CoopParticipants)
		assert.Equal(t, []string{"plan", "review"}, card.CoopPhases)
		assert.Equal(t, []string{"laptop"}, card.CoopGuests)
	})

	t.Run("create with best_of_n=1 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:    "Bad best_of_n",
			Type:     "task",
			Priority: "medium",
			BestOfN:  1,
		})

		resp := postAs(t, string(body), "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
	})

	t.Run("create with coop_participants over max returns 400", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:            "Bad coop",
			Type:             "task",
			Priority:         "medium",
			CoopParticipants: 6, // coopTestConfig max is 5
		})

		resp := postAs(t, string(body), "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
	})

	t.Run("create with best_of_n=3 as non-human agent returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:    "Agent best_of_n",
			Type:     "task",
			Priority: "medium",
			BestOfN:  3,
		})

		resp := postAs(t, string(body), "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "best_of_n")
	})

	t.Run("create with co-op fields as non-human agent returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:            "Agent coop",
			Type:             "task",
			Priority:         "medium",
			CoopParticipants: 3,
		})

		resp := postAs(t, string(body), "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "co-op")
	})

	t.Run("create with no human-only fields as non-human agent succeeds", func(t *testing.T) {
		// Sanity check: an agent creating a plain card is still allowed —
		// only the new fields are gated, not the whole create path.
		body, _ := json.Marshal(createCardRequest{
			Title:    "Agent plain",
			Type:     "task",
			Priority: "medium",
		})

		resp := postAs(t, string(body), "agent:y")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "Agent plain", card.Title)
		assert.Zero(t, card.BestOfN)
		assert.Zero(t, card.CoopParticipants)
	})
}
