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
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
)

const boardConfigWithVerify = `name: test-project
prefix: TEST
next_id: 1
repo: https://github.com/example/project.git
states: [todo, in_progress, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
remote_execution:
  enabled: true
  runner_image: my-runner:latest
verify:
  command: make test
  timeout_seconds: 600
  env: [JAVA_HOME]
`

// triggerVerifyPayload runs a card and captures the TriggerPayload the agent
// backend received.
func triggerVerifyPayload(t *testing.T, svc *service.CardService, bus *events.Bus, cardID string) backend.TriggerPayload {
	t.Helper()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	var capturedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	t.Cleanup(mockBackend.Close)

	backendClient := backend.NewClient(mockBackend.URL, apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey:       apiKey,
			DefaultModel: "openrouter/auto",
		},
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/cards/"+cardID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	return capturedPayload
}

// TestRunCard_VerifyResolution covers the resolved verify in the TriggerPayload:
// project-only, card-over-project field merge, and the no-verify fallthrough.
func TestRunCard_VerifyResolution(t *testing.T) {
	ctx := context.Background()

	t.Run("project verify sent for agent backend", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithVerify)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "project verify", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		payload := triggerVerifyPayload(t, svc, bus, card.ID)

		require.NotNil(t, payload.Verify)
		assert.Equal(t, "make test", payload.Verify.Command)
		assert.Equal(t, 600, payload.Verify.TimeoutSeconds)
		assert.Equal(t, []string{"JAVA_HOME"}, payload.Verify.Env)
	})

	t.Run("card verify merges over project field by field", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithVerify)
		defer cleanup()

		// Card overrides the command only; timeout and env fall through to project.
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "card verify", Type: "task", Priority: "medium",
			Verify: &board.VerifyConfig{Command: "go test ./..."},
		})
		require.NoError(t, err)

		payload := triggerVerifyPayload(t, svc, bus, card.ID)

		require.NotNil(t, payload.Verify)
		assert.Equal(t, "go test ./...", payload.Verify.Command, "card command wins")
		assert.Equal(t, 600, payload.Verify.TimeoutSeconds, "project timeout falls through")
		assert.Equal(t, []string{"JAVA_HOME"}, payload.Verify.Env, "project env falls through")
	})

	t.Run("card empty-env override clears the project env in the payload", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithVerify)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "empty env override", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		// PATCH the card to explicitly clear env (non-nil empty) while inheriting
		// the project's command/timeout.
		_, err = svc.PatchCard(ctx, "test-project", card.ID, service.PatchCardInput{
			Verify: &board.VerifyConfig{Command: "go test ./...", Env: []string{}},
		})
		require.NoError(t, err)

		payload := triggerVerifyPayload(t, svc, bus, card.ID)

		require.NotNil(t, payload.Verify)
		assert.Equal(t, "go test ./...", payload.Verify.Command)
		assert.Empty(t, payload.Verify.Env, "card's empty-env override must clear the project env")
		assert.NotContains(t, payload.Verify.Env, "JAVA_HOME", "project env must not leak through an override-to-clear")
	})

	t.Run("no verify anywhere yields nil for agent backend", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "no verify", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		payload := triggerVerifyPayload(t, svc, bus, card.ID)

		assert.Nil(t, payload.Verify)
	})
}
