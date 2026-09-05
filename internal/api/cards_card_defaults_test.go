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
)

func postCardAs(t *testing.T, serverURL, body, agentID string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/projects/test-project/cards", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func decodeCard(t *testing.T, resp *http.Response) board.Card {
	t.Helper()

	var card board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))

	return card
}

func setCardDefaults(t *testing.T, server *httptest.Server, d *board.CardDefaults) {
	t.Helper()

	body := validUpdateProjectBody(nil)
	body.CardDefaults = d

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCreateCard_InheritsCardDefaultsWhenFieldsOmitted(t *testing.T) {
	server := cardDefaultsServer(t)
	setCardDefaults(t, server, &board.CardDefaults{
		Autonomous: true, MaxCapability: true, MobParticipants: 3, MobPhases: []string{"review"},
		CreatePR: new(false), AwaitCI: true, AwaitCopilotReview: true,
	})

	resp := postCardAs(t, server.URL, `{"title":"Inherit","type":"task","priority":"medium"}`, "")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	card := decodeCard(t, resp)
	assert.True(t, card.Autonomous)
	assert.True(t, card.MaxCapability)
	assert.Equal(t, 3, card.MobParticipants)
	assert.Equal(t, []string{"review"}, card.MobPhases)
	assert.False(t, card.CreatePR)
	assert.True(t, card.AwaitCI)
	assert.True(t, card.AwaitCopilotReview)
}

func TestCreateCard_ExplicitFalseBeatsCardDefaults(t *testing.T) {
	server := cardDefaultsServer(t)
	setCardDefaults(t, server, &board.CardDefaults{Autonomous: true, MobParticipants: 3, MobPhases: []string{"review"}, AwaitCI: true})

	resp := postCardAs(t, server.URL,
		`{"title":"Explicit","type":"task","priority":"medium","autonomous":false,"await_ci":false,"mob_participants":0}`, "")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	card := decodeCard(t, resp)
	assert.False(t, card.Autonomous)
	assert.False(t, card.AwaitCI)
	assert.Equal(t, 0, card.MobParticipants)
	assert.Empty(t, card.MobPhases)
}

func TestCreateCard_SubtaskIgnoresCardDefaults(t *testing.T) {
	server := cardDefaultsServer(t)
	setCardDefaults(t, server, &board.CardDefaults{Autonomous: true, MobParticipants: 3, MobPhases: []string{"review"}})

	resp := postCardAs(t, server.URL, `{"title":"Parent","type":"task","priority":"medium"}`, "")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	parent := decodeCard(t, resp)
	closeBody(t, resp.Body)

	resp = postCardAs(t, server.URL,
		`{"title":"Sub","type":"task","priority":"medium","parent":"`+parent.ID+`"}`, "")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	sub := decodeCard(t, resp)
	assert.False(t, sub.Autonomous)
	assert.Equal(t, 0, sub.MobParticipants)
	assert.False(t, sub.CreatePR)
}

func TestCreateCard_AgentPresenceOfNullableFieldIsHumanOnly(t *testing.T) {
	server := cardDefaultsServer(t)

	for _, body := range []string{
		`{"title":"A","type":"task","priority":"medium","autonomous":false}`,
		`{"title":"A","type":"task","priority":"medium","await_ci":false}`,
		`{"title":"A","type":"task","priority":"medium","await_copilot_review":false}`,
		`{"title":"A","type":"task","priority":"medium","max_capability":false}`,
		`{"title":"A","type":"task","priority":"medium","mob_participants":0}`,
	} {
		resp := postCardAs(t, server.URL, body, "agent-1")
		require.Equal(t, http.StatusForbidden, resp.StatusCode, body)

		var errResp map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
		assert.Equal(t, ErrCodeHumanOnlyField, errResp["code"], body)
		closeBody(t, resp.Body)
	}

	// Omitting them entirely stays allowed for agents.
	resp := postCardAs(t, server.URL, `{"title":"A","type":"task","priority":"medium"}`, "agent-1")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}
