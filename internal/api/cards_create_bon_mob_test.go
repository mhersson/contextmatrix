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

// TestCreateCardBestOfNAndMob covers the create-time acceptance of the
// best_of_n and mob fields: valid values are persisted, invalid values
// return 400, and non-human callers setting any of them get 403
// HUMAN_ONLY_FIELD. Mirrors the PATCH/PUT coverage in
// TestPatchCardBestOfN / TestPatchCardMob_ValidationMatrix.
func TestCreateCardBestOfNAndMob(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{
		Service: svc,
		Bus:     bus,
		BestOfN: config.BestOfNConfig{MaxCandidates: 5},
		Mob:     mobTestConfig(),
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

	t.Run("create with mob fields persists the values", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:           "Mob create",
			Type:            "task",
			Priority:        "medium",
			MobParticipants: 3,
			MobPhases:       []string{"plan", "review"},
			MobGuests:       []string{"laptop"},
		})

		resp := postAs(t, string(body), "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, 3, card.MobParticipants)
		assert.Equal(t, []string{"plan", "review"}, card.MobPhases)
		assert.Equal(t, []string{"laptop"}, card.MobGuests)
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

	t.Run("create with mob_participants over max returns 400", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:           "Bad mob",
			Type:            "task",
			Priority:        "medium",
			MobParticipants: 6, // mobTestConfig max is 5
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

	t.Run("create with mob fields as non-human agent returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		body, _ := json.Marshal(createCardRequest{
			Title:           "Agent mob",
			Type:            "task",
			Priority:        "medium",
			MobParticipants: 3,
		})

		resp := postAs(t, string(body), "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "mob")
	})

	t.Run("create with no human-only fields as non-human agent succeeds", func(t *testing.T) {
		// Sanity check: an agent creating a plain card is still allowed -
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
		assert.Zero(t, card.MobParticipants)
	})
}
