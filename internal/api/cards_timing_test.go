package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCardTiming_REST(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	c, err := svc.CreateCard(t.Context(), "test-project", service.CreateCardInput{Title: "Timers", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	server := httptest.NewServer(newTestRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	read := func() (board.Card, map[string]any) {
		t.Helper()

		resp, err := http.Get(server.URL + "/api/projects/test-project/cards/" + c.ID)
		require.NoError(t, err)

		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var raw map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))

		data, err := json.Marshal(raw)
		require.NoError(t, err)

		var card board.Card
		require.NoError(t, json.Unmarshal(data, &card))

		return card, raw
	}

	fresh, raw := read()
	assert.False(t, fresh.StateSince.IsZero())
	assert.NotContains(t, raw, "worked_seconds", "no claim recorded: the total is unknown, not zero")
	assert.NotContains(t, raw, "working_since")

	_, err = svc.ClaimCard(t.Context(), c.Project, c.ID, "worker")
	require.NoError(t, err)

	working, _ := read()
	assert.NotNil(t, working.WorkingSince)
	assert.NotNil(t, working.WorkedSeconds)
	assert.Nil(t, working.UnclaimedSince)

	_, err = svc.ReleaseCard(t.Context(), c.Project, c.ID, "worker")
	require.NoError(t, err)

	released, _ := read()
	assert.Nil(t, released.WorkingSince)
	assert.NotNil(t, released.UnclaimedSince)
	require.Len(t, released.WorkIntervals, 1)
	assert.NotNil(t, released.WorkIntervals[0].To)

	resp, err := http.Get(server.URL + "/api/projects/test-project/cards")
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	var page listCardsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, released.WorkedSeconds, page.Items[0].WorkedSeconds)
	assert.Equal(t, released.UnclaimedSince, page.Items[0].UnclaimedSince)
}
