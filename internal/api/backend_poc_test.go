package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/service"
)

const boardConfigWithPOC = `name: test-project
prefix: TEST
next_id: 1
repo: https://github.com/example/project.git
poc: true
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
  worker_image: my-worker:latest
`

// TestRunCard_POCFollowsProjectAtLaunch pins that the trigger carries the
// project's poc flag as it is when the run starts.
func TestRunCard_POCFollowsProjectAtLaunch(t *testing.T) {
	ctx := context.Background()

	newCard := func(t *testing.T, svc *service.CardService, title string) string {
		t.Helper()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: title, Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		return card.ID
	}

	t.Run("absent poc sends none", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithVerify)
		defer cleanup()

		assert.False(t, triggerVerifyPayload(t, svc, bus, newCard(t, svc, "production")).POC)
	})

	t.Run("switching poc off reaches the next run", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithPOC)
		defer cleanup()

		assert.True(t, triggerVerifyPayload(t, svc, bus, newCard(t, svc, "while poc")).POC)

		cur, err := svc.GetProject(ctx, "test-project")
		require.NoError(t, err)

		_, err = svc.UpdateProject(ctx, "test-project", service.UpdateProjectInput{
			Repo: cur.Repo, States: cur.States, Types: cur.Types, Priorities: cur.Priorities,
			Transitions: cur.Transitions, POC: new(false),
		})
		require.NoError(t, err)

		assert.False(t, triggerVerifyPayload(t, svc, bus, newCard(t, svc, "after switch-off")).POC,
			"the first run after a switch-off must get the full workflow")
	})
}
