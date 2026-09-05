package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
)

// cardDefaultsServer is noneModeServer plus the mob bounds the card_defaults
// validation reads (mobTestConfig: max 5 seats).
func cardDefaultsServer(t *testing.T) *httptest.Server {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	router := NewRouter(RouterConfig{
		Service: svc,
		Bus:     bus,
		BestOfN: config.BestOfNConfig{MaxCandidates: 5},
		Mob:     mobTestConfig(),
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server
}

func decodeProject(t *testing.T, resp *http.Response) board.ProjectConfig {
	t.Helper()

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))

	return cfg
}

func TestUpdateProject_CardDefaults_RoundTrip(t *testing.T) {
	server := cardDefaultsServer(t)

	body := validUpdateProjectBody(nil)
	body.CardDefaults = &board.CardDefaults{
		Autonomous: true, MobParticipants: 3, MobPhases: []string{"review"}, CreatePR: new(false), AwaitCI: true,
	}

	resp := putProject(t, server.URL, nil, body)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg := decodeProject(t, resp)
	require.NotNil(t, cfg.CardDefaults)
	assert.True(t, cfg.CardDefaults.Autonomous)
	assert.Equal(t, 3, cfg.CardDefaults.MobParticipants)
	assert.Equal(t, []string{"review"}, cfg.CardDefaults.MobPhases)
	require.NotNil(t, cfg.CardDefaults.CreatePR)
	assert.False(t, *cfg.CardDefaults.CreatePR)
	assert.True(t, cfg.CardDefaults.AwaitCI)
}

func TestUpdateProject_CardDefaults_OmittedPreserves(t *testing.T) {
	server := cardDefaultsServer(t)

	set := validUpdateProjectBody(nil)
	set.CardDefaults = &board.CardDefaults{Autonomous: true}
	resp := putProject(t, server.URL, nil, set)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	closeBody(t, resp.Body)

	resp = putProject(t, server.URL, nil, validUpdateProjectBody(nil))
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg := decodeProject(t, resp)
	require.NotNil(t, cfg.CardDefaults, "omitting card_defaults must preserve the stored block")
	assert.True(t, cfg.CardDefaults.Autonomous)
}

func TestUpdateProject_CardDefaults_BuiltinsClear(t *testing.T) {
	server := cardDefaultsServer(t)

	set := validUpdateProjectBody(nil)
	set.CardDefaults = &board.CardDefaults{Autonomous: true}
	resp := putProject(t, server.URL, nil, set)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	closeBody(t, resp.Body)

	reset := validUpdateProjectBody(nil)
	reset.CardDefaults = &board.CardDefaults{CreatePR: new(true)}

	resp = putProject(t, server.URL, nil, reset)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	cfg := decodeProject(t, resp)
	assert.Nil(t, cfg.CardDefaults, "a block equal to the built-ins clears card_defaults")
}

func TestUpdateProject_CardDefaults_MobBoundsRejected(t *testing.T) {
	server := cardDefaultsServer(t)

	tests := []struct {
		name string
		d    board.CardDefaults
	}{
		{name: "seats above max", d: board.CardDefaults{MobParticipants: 6, MobPhases: []string{"review"}}},
		{name: "single seat", d: board.CardDefaults{MobParticipants: 1}},
		{name: "unknown phase", d: board.CardDefaults{MobParticipants: 3, MobPhases: []string{"deploy"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := validUpdateProjectBody(nil)
			body.CardDefaults = &tt.d

			resp := putProject(t, server.URL, nil, body)
			defer closeBody(t, resp.Body)

			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

			var errResp map[string]any
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
			assert.Equal(t, ErrCodeValidationError, errResp["code"])
		})
	}
}
