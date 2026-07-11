package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// coopRunConfig returns the CoopConfig used by the trigger tests. Callers
// mutate ExecuteCheckpointsEnabled per case.
func coopRunConfig() config.CoopConfig {
	return config.CoopConfig{
		MaxParticipants:     5,
		DefaultParticipants: 3,
		DefaultRounds:       2,
		MaxRounds:           3,
		BudgetFactor:        0.75,
		CheckpointMinTier:   "complex",
		Guests: []config.CoopGuest{
			{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"},
		},
	}
}

// runCoopTrigger creates a todo card, applies patch (co-op fields, best_of_n),
// triggers /run against a payload-capturing mock backend wired with coopCfg,
// and returns the captured payload, the service (for activity assertions),
// and the card ID.
func runCoopTrigger(t *testing.T, coopCfg config.CoopConfig, patch *service.PatchCardInput) (backend.TriggerPayload, *service.CardService, string) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	t.Cleanup(cleanup)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Coop task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	if patch != nil {
		_, err = svc.PatchCard(ctx, "test-project", card.ID, *patch)
		require.NoError(t, err)
	}

	var received backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	t.Cleanup(mockBackend.Close)

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		BestOfN:         config.BestOfNConfig{MaxCandidates: 5, DefaultCandidates: 3, OutcomeFloor: 20},
		Coop:            coopCfg,
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	return received, svc, card.ID
}

// coopWarnings collects the card's activity-log messages recorded under the
// "coop-warning" action.
func coopWarnings(t *testing.T, svc *service.CardService, cardID string) []string {
	t.Helper()

	card, err := svc.GetCard(context.Background(), "test-project", cardID)
	require.NoError(t, err)

	var msgs []string

	for _, e := range card.ActivityLog {
		if e.Action == "coop-warning" {
			msgs = append(msgs, e.Message)
		}
	}

	return msgs
}

func TestRunCard_CoopPayloadShape(t *testing.T) {
	participants := 3
	payload, _, _ := runCoopTrigger(t, coopRunConfig(), &service.PatchCardInput{
		CoopParticipants: &participants,
		CoopPhases:       []string{"plan", "review"},
		CoopGuests:       []string{"laptop"},
	})

	require.NotNil(t, payload.Coop)
	assert.Equal(t, 3, payload.Coop.Participants)
	assert.Equal(t, []string{"plan", "review"}, payload.Coop.Phases)
	assert.Equal(t, 2, payload.Coop.Rounds, "rounds = coop.default_rounds")
	assert.InDelta(t, 0.75, payload.Coop.BudgetFactor, 1e-9)
	assert.False(t, payload.Coop.ExecuteCheckpoints)
	assert.Equal(t, "complex", payload.Coop.CheckpointMinTier)
	require.Len(t, payload.Coop.Guests, 1)
	assert.Equal(t, protocol.GuestSpec{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"}, payload.Coop.Guests[0])
}

func TestRunCard_CoopAbsentWhenOff(t *testing.T) {
	payload, _, _ := runCoopTrigger(t, coopRunConfig(), nil)
	assert.Nil(t, payload.Coop, "cards without coop_participants >= 2 must not carry a Coop spec")
}

func TestRunCard_CoopParticipantsClampedToCurrentMax(t *testing.T) {
	// The stored card value can exceed the max if coop.max_participants was
	// lowered after the card was written (svc.PatchCard in the fixture
	// bypasses REST validation, standing in for a write under the old max) —
	// the trigger clamp is authoritative.
	cfg := coopRunConfig()
	cfg.MaxParticipants = 3

	participants := 5
	payload, _, _ := runCoopTrigger(t, cfg, &service.PatchCardInput{CoopParticipants: &participants})

	require.NotNil(t, payload.Coop)
	assert.Equal(t, 3, payload.Coop.Participants)
}

func TestRunCard_CoopUnknownGuestDropped(t *testing.T) {
	participants := 2
	payload, svc, cardID := runCoopTrigger(t, coopRunConfig(), &service.PatchCardInput{
		CoopParticipants: &participants,
		CoopGuests:       []string{"laptop", "desktop"}, // "desktop" is not registered
	})

	require.NotNil(t, payload.Coop)
	require.Len(t, payload.Coop.Guests, 1, "unknown guest must be dropped, registered one kept")
	assert.Equal(t, "laptop", payload.Coop.Guests[0].Name)

	msgs := coopWarnings(t, svc, cardID)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "desktop")
	assert.Contains(t, msgs[0], "not registered")
}

func TestRunCard_CoopExecutePhaseMatrix(t *testing.T) {
	cases := []struct {
		name          string
		checkpointsOn bool
		bestOfN       int
		wantPhases    []string
		wantWarning   string // substring; "" = no warning expected
	}{
		{
			name:          "flag off drops execute",
			checkpointsOn: false,
			wantPhases:    []string{"plan"},
			wantWarning:   "execute_checkpoints_enabled",
		},
		{
			name:          "flag on keeps execute",
			checkpointsOn: true,
			wantPhases:    []string{"plan", "execute"},
		},
		{
			name:          "best-of-n wins over execute even with flag on",
			checkpointsOn: true,
			bestOfN:       3,
			wantPhases:    []string{"plan"},
			wantWarning:   "best_of_n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := coopRunConfig()
			cfg.ExecuteCheckpointsEnabled = tc.checkpointsOn

			participants := 2
			patch := &service.PatchCardInput{
				CoopParticipants: &participants,
				CoopPhases:       []string{"plan", "execute"},
			}

			if tc.bestOfN > 0 {
				patch.BestOfN = &tc.bestOfN
			}

			payload, svc, cardID := runCoopTrigger(t, cfg, patch)

			require.NotNil(t, payload.Coop)
			assert.Equal(t, tc.wantPhases, payload.Coop.Phases)

			msgs := coopWarnings(t, svc, cardID)
			if tc.wantWarning == "" {
				assert.Empty(t, msgs)
			} else {
				require.Len(t, msgs, 1)
				assert.Contains(t, msgs[0], tc.wantWarning)
				assert.Contains(t, msgs[0], "skipped", "warning should say the phase was skipped: %s", msgs[0])
			}
		})
	}
}
