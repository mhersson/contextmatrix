package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// cardDefaultsBaseInput reuses remoteExecBaseInput's valid states/types/
// transitions for the "test-project" fixture, carrying only card_defaults.
func cardDefaultsBaseInput(d *board.CardDefaults) UpdateProjectInput {
	in := remoteExecBaseInput(nil)
	in.CardDefaults = d

	return in
}

func TestUpdateProject_CardDefaultsPreserveSetClear(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	projectDir := filepath.Join(tmpDir, "boards", "test-project")

	// Set: a non-nil block lands on the config and round-trips to disk.
	cfg, err := svc.UpdateProject(ctx, "test-project", cardDefaultsBaseInput(&board.CardDefaults{
		Autonomous: true, MobParticipants: 3, MobPhases: []string{"review"}, CreatePR: new(false),
	}))
	require.NoError(t, err)
	require.NotNil(t, cfg.CardDefaults)
	assert.True(t, cfg.CardDefaults.Autonomous)
	assert.Equal(t, 3, cfg.CardDefaults.MobParticipants)

	reloaded, err := board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	require.NotNil(t, reloaded.CardDefaults)
	assert.True(t, reloaded.CardDefaults.Autonomous)
	assert.Equal(t, []string{"review"}, reloaded.CardDefaults.MobPhases)
	require.NotNil(t, reloaded.CardDefaults.CreatePR)
	assert.False(t, *reloaded.CardDefaults.CreatePR)

	// Preserve: a nil pointer leaves the block untouched.
	cfg, err = svc.UpdateProject(ctx, "test-project", cardDefaultsBaseInput(nil))
	require.NoError(t, err)
	require.NotNil(t, cfg.CardDefaults, "nil card_defaults must preserve the existing block")
	assert.True(t, cfg.CardDefaults.Autonomous)

	// Clear: a block equal to the built-ins normalizes away to nil.
	cfg, err = svc.UpdateProject(ctx, "test-project", cardDefaultsBaseInput(&board.CardDefaults{CreatePR: new(true)}))
	require.NoError(t, err)
	assert.Nil(t, cfg.CardDefaults, "built-in values must normalize to nil")

	reloaded, err = board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	assert.Nil(t, reloaded.CardDefaults, "cleared card_defaults must be absent from .board.yaml on disk")
}

func TestUpdateProject_CardDefaultsNormalizesCreatePRTrue(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	cfg, err := svc.UpdateProject(context.Background(), "test-project", cardDefaultsBaseInput(&board.CardDefaults{
		Autonomous: true, CreatePR: new(true),
	}))
	require.NoError(t, err)
	require.NotNil(t, cfg.CardDefaults)
	assert.Nil(t, cfg.CardDefaults.CreatePR, "create_pr true is the built-in and is not persisted")
	assert.True(t, cfg.CardDefaults.CreatePROn())
}

func TestCopyProjectConfig_DeepCopiesCardDefaults(t *testing.T) {
	src := &board.ProjectConfig{
		Name: "p", Prefix: "P", NextID: 1,
		CardDefaults: &board.CardDefaults{MobParticipants: 3, MobPhases: []string{"review"}, CreatePR: new(false)},
	}

	cp := copyProjectConfig(src)
	cp.CardDefaults.MobPhases[0] = "plan"
	*cp.CardDefaults.CreatePR = true
	cp.CardDefaults.MobParticipants = 5

	assert.Equal(t, []string{"review"}, src.CardDefaults.MobPhases)
	assert.False(t, *src.CardDefaults.CreatePR)
	assert.Equal(t, 3, src.CardDefaults.MobParticipants)
}
