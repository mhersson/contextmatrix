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
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mobTestConfig is the MobConfig wired into the router for these tests:
// max 5 seats, one registered guest ("laptop").
func mobTestConfig() config.MobConfig {
	return config.MobConfig{
		MaxParticipants:     5,
		DefaultParticipants: 3,
		DefaultRounds:       2,
		MaxRounds:           3,
		BudgetFactor:        0.75,
		CheckpointMinTier:   "complex",
		Guests: []config.MobGuest{
			{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"},
		},
	}
}

func TestPatchCardMob_ValidationMatrix(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Mob: mobTestConfig()})

	server := httptest.NewServer(router)
	defer server.Close()

	newCard := func(t *testing.T) *board.Card {
		t.Helper()

		card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
			Title: "mob matrix", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		return card
	}

	patchAs := func(t *testing.T, cardID, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPatch,
			server.URL+"/api/projects/test-project/cards/"+cardID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	t.Run("human enables mob session with phases and registered guest", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID,
			`{"mob_participants":3,"mob_phases":["plan","review"],"mob_guests":["laptop"]}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, 3, updated.MobParticipants)
		assert.Equal(t, []string{"plan", "review"}, updated.MobPhases)
		assert.Equal(t, []string{"laptop"}, updated.MobGuests)
	})

	t.Run("participants 1 rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":1}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("participants above max rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":6}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown phase rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":3,"mob_phases":["plan","judge"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Contains(t, apiErr.Details, "judge")
	})

	t.Run("duplicate phase rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":3,"mob_phases":["plan","plan"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("execute phase accepted at write time", func(t *testing.T) {
		// The server-side execute_checkpoints flag gates the TRIGGER, not the
		// card write — flipping the flag later must not require a card edit.
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":2,"mob_phases":["execute"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("unregistered guest rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_participants":3,"mob_guests":["desktop"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Contains(t, apiErr.Details, "desktop")
	})

	t.Run("guests without participants rejected", func(t *testing.T) {
		card := newCard(t)

		resp := patchAs(t, card.ID, `{"mob_guests":["laptop"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("guest patch valid against existing participants", func(t *testing.T) {
		// Cross-field rule validates the RESULTING state: participants were
		// set by an earlier patch, this one only adds the guest.
		card := newCard(t)

		first := patchAs(t, card.ID, `{"mob_participants":2}`, "")
		closeBody(t, first.Body)
		require.Equal(t, http.StatusOK, first.StatusCode)

		resp := patchAs(t, card.ID, `{"mob_guests":["laptop"]}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("turning off clears via zero and empty arrays", func(t *testing.T) {
		card := newCard(t)

		on := patchAs(t, card.ID, `{"mob_participants":3,"mob_phases":["plan"],"mob_guests":["laptop"]}`, "")
		closeBody(t, on.Body)
		require.Equal(t, http.StatusOK, on.StatusCode)

		resp := patchAs(t, card.ID, `{"mob_participants":0,"mob_phases":[],"mob_guests":[]}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Zero(t, updated.MobParticipants)
		assert.Empty(t, updated.MobPhases)
		assert.Empty(t, updated.MobGuests)
	})

	t.Run("agent setting any mob field rejected 403", func(t *testing.T) {
		card := newCard(t)

		for _, body := range []string{
			`{"mob_participants":3}`,
			`{"mob_phases":["plan"]}`,
			`{"mob_guests":["laptop"]}`,
		} {
			resp := patchAs(t, card.ID, body, "agent-1")

			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "body %s", body)

			var apiErr APIError
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
			closeBody(t, resp.Body)
		}
	})
}

func TestUpdateCardMob_PutSemantics(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Mob: mobTestConfig()})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "mob put", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	putAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPut,
			server.URL+"/api/projects/test-project/cards/"+card.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	base := `"title":"mob put","type":"task","state":"todo","priority":"medium"`

	t.Run("human sets mob fields via PUT", func(t *testing.T) {
		resp := putAs(t, `{`+base+`,"mob_participants":2,"mob_phases":["review"],"mob_guests":["laptop"]}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, 2, updated.MobParticipants)
		assert.Equal(t, []string{"review"}, updated.MobPhases)
		assert.Equal(t, []string{"laptop"}, updated.MobGuests)
	})

	t.Run("agent clearing mob via PUT omission rejected 403", func(t *testing.T) {
		// PUT is full-replace: an agent body that omits the fields would
		// clear them — the guard compares against the existing card.
		resp := putAs(t, `{`+base+`}`, "agent-1")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human clears mob via PUT omission", func(t *testing.T) {
		resp := putAs(t, `{`+base+`}`, "human:alice")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Zero(t, updated.MobParticipants)
		assert.Empty(t, updated.MobPhases)
		assert.Empty(t, updated.MobGuests)
	})
}
