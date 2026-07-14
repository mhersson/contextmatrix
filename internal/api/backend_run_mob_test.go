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

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mobRunConfig returns the MobConfig used by the trigger tests. Callers
// mutate ExecuteCheckpointsEnabled per case.
func mobRunConfig() config.MobConfig {
	return config.MobConfig{
		MaxParticipants:     5,
		DefaultParticipants: 3,
		DefaultRounds:       2,
		MaxRounds:           3,
		BudgetFactor:        0.75,
		CheckpointMinTier:   "complex",
		CheckpointRounds:    3,
		Guests: []config.MobGuest{
			{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"},
		},
	}
}

// runMobTrigger creates a todo card, applies patch (mob fields, best_of_n),
// triggers /run against a payload-capturing mock backend wired with mobCfg,
// and returns the captured payload, the service (for activity assertions),
// and the card ID.
func runMobTrigger(t *testing.T, mobCfg config.MobConfig, patch *service.PatchCardInput) (backend.TriggerPayload, *service.CardService, string) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Mob task", Type: "task", Priority: "medium",
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
		Mob:             mobCfg,
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

// mobWarnings collects the card's activity-log messages recorded under the
// "mob-warning" action.
func mobWarnings(t *testing.T, svc *service.CardService, cardID string) []string {
	t.Helper()

	card, err := svc.GetCard(context.Background(), "test-project", cardID)
	require.NoError(t, err)

	var msgs []string

	for _, e := range card.ActivityLog {
		if e.Action == "mob-warning" {
			msgs = append(msgs, e.Message)
		}
	}

	return msgs
}

func TestRunCard_MobPayloadShape(t *testing.T) {
	participants := 3
	payload, _, _ := runMobTrigger(t, mobRunConfig(), &service.PatchCardInput{
		MobParticipants: &participants,
		MobPhases:       []string{"plan", "review"},
		MobGuests:       []string{"laptop"},
	})

	require.NotNil(t, payload.Mob)
	assert.Equal(t, 3, payload.Mob.Participants)
	assert.Equal(t, []string{"plan", "review"}, payload.Mob.Phases)
	assert.Equal(t, 2, payload.Mob.Rounds, "rounds = mob.default_rounds")
	assert.InDelta(t, 0.75, payload.Mob.BudgetFactor, 1e-9)
	assert.True(t, payload.Mob.ExecuteCheckpoints, "fixture leaves the flag nil, which means enabled")
	assert.Equal(t, "complex", payload.Mob.CheckpointMinTier)
	assert.Equal(t, 3, payload.Mob.CheckpointRounds)
	require.Len(t, payload.Mob.Guests, 1)
	assert.Equal(t, protocol.GuestSpec{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"}, payload.Mob.Guests[0])
}

func TestRunCard_MobAbsentWhenOff(t *testing.T) {
	payload, _, _ := runMobTrigger(t, mobRunConfig(), nil)
	assert.Nil(t, payload.Mob, "cards without mob_participants >= 2 must not carry a Mob spec")
}

func TestRunCard_MobParticipantsClampedToCurrentMax(t *testing.T) {
	// The stored card value can exceed the max if mob.max_participants was
	// lowered after the card was written (svc.PatchCard in the fixture
	// bypasses REST validation, standing in for a write under the old max) —
	// the trigger clamp is authoritative.
	cfg := mobRunConfig()
	cfg.MaxParticipants = 3

	participants := 5
	payload, _, _ := runMobTrigger(t, cfg, &service.PatchCardInput{MobParticipants: &participants})

	require.NotNil(t, payload.Mob)
	assert.Equal(t, 3, payload.Mob.Participants)
}

func TestRunCard_MobUnknownGuestDropped(t *testing.T) {
	participants := 2
	payload, svc, cardID := runMobTrigger(t, mobRunConfig(), &service.PatchCardInput{
		MobParticipants: &participants,
		MobGuests:       []string{"laptop", "desktop"}, // "desktop" is not registered
	})

	require.NotNil(t, payload.Mob)
	require.Len(t, payload.Mob.Guests, 1, "unknown guest must be dropped, registered one kept")
	assert.Equal(t, "laptop", payload.Mob.Guests[0].Name)

	msgs := mobWarnings(t, svc, cardID)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "desktop")
	assert.Contains(t, msgs[0], "not registered")
}

func TestRunCard_MobAllPhasesDroppedRunsSolo(t *testing.T) {
	// An execute-only card with execute checkpoints off has every explicitly
	// selected phase filtered out. The run must proceed SOLO: an empty Phases
	// slice is the agent's "plan+review both on" default, so shipping the spec
	// would silently run discussions the operator never chose.
	cfg := mobRunConfig()
	off := false
	cfg.ExecuteCheckpointsEnabled = &off

	participants := 3
	payload, svc, cardID := runMobTrigger(t, cfg, &service.PatchCardInput{
		MobParticipants: &participants,
		MobPhases:       []string{"execute"},
	})

	assert.Nil(t, payload.Mob,
		"all requested phases dropped must run solo, not fall back to the plan+review default")

	msgs := mobWarnings(t, svc, cardID)
	require.NotEmpty(t, msgs)

	var sawSolo bool

	for _, m := range msgs {
		if strings.Contains(m, "solo") {
			sawSolo = true
		}
	}

	assert.True(t, sawSolo, "a warning must record that the run proceeds solo: %v", msgs)
}

func TestRunCard_MobExecutePhaseMatrix(t *testing.T) {
	cases := []struct {
		name          string
		checkpointsOn bool
		bestOfN       int
		wantPhases    []string
		wantBestOfN   int
		wantWarning   string // substring; "" = no warning expected
	}{
		{
			name:          "flag off drops execute",
			checkpointsOn: false,
			wantPhases:    []string{"plan"},
			wantWarning:   "execute_checkpoints_enabled",
		},
		{
			name:          "flag off keeps best-of-n",
			checkpointsOn: false,
			bestOfN:       3,
			wantPhases:    []string{"plan"},
			wantBestOfN:   3,
			wantWarning:   "execute_checkpoints_enabled",
		},
		{
			name:          "flag on keeps execute",
			checkpointsOn: true,
			wantPhases:    []string{"plan", "execute"},
		},
		{
			name:          "mob coding wins over best-of-n",
			checkpointsOn: true,
			bestOfN:       3,
			wantPhases:    []string{"plan", "execute"},
			wantBestOfN:   0,
			wantWarning:   "best_of_n ignored: mob coding takes priority",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mobRunConfig()
			cfg.ExecuteCheckpointsEnabled = &tc.checkpointsOn

			participants := 2
			patch := &service.PatchCardInput{
				MobParticipants: &participants,
				MobPhases:       []string{"plan", "execute"},
			}

			if tc.bestOfN > 0 {
				patch.BestOfN = &tc.bestOfN
			}

			payload, svc, cardID := runMobTrigger(t, cfg, patch)

			require.NotNil(t, payload.Mob)
			assert.Equal(t, tc.wantPhases, payload.Mob.Phases)
			assert.Equal(t, tc.wantBestOfN, payload.BestOfN)
			assert.Equal(t, 3, payload.Mob.CheckpointRounds)

			msgs := mobWarnings(t, svc, cardID)
			if tc.wantWarning == "" {
				assert.Empty(t, msgs)
			} else {
				require.Len(t, msgs, 1)
				assert.Contains(t, msgs[0], tc.wantWarning)
			}
		})
	}
}
