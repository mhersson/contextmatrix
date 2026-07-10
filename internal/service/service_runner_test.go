package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProjectName = "test-project"

func TestBackendAuthor_DefaultsToNeutralBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	// No SetTaskBackendName call → unset → defaults to "backend".
	assert.Equal(t, "backend", svc.backendAuthor())
}

func TestBackendAuthor_ReflectsConfiguredBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	svc.SetTaskBackendName("agent")
	assert.Equal(t, "agent", svc.backendAuthor())
}

func TestUpdateRunnerStatus_AttributesMessageToActiveBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	svc.SetTaskBackendName("agent")

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	_, err = svc.UpdateRunnerStatus(context.Background(), testProjectName, card.ID, "running", "container started")
	require.NoError(t, err)

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.NotEmpty(t, got.ActivityLog)
	last := got.ActivityLog[len(got.ActivityLog)-1]
	assert.Equal(t, "runner_status", last.Action)
	assert.Equal(t, "agent", last.Agent)
}
