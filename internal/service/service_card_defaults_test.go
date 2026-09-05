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

func setProjectCardDefaults(t *testing.T, svc *CardService, d *board.CardDefaults) {
	t.Helper()

	_, err := svc.UpdateProject(context.Background(), "test-project", cardDefaultsBaseInput(d))
	require.NoError(t, err)
}

func TestCreateCard_InheritsProjectCardDefaults(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	setProjectCardDefaults(t, svc, &board.CardDefaults{
		Autonomous: true, MaxCapability: true, MobParticipants: 3, MobPhases: []string{"review"},
		CreatePR: new(false), AwaitCI: true, AwaitCopilotReview: true,
	})

	// The MCP create_card path and the GitHub syncer never set these fields.
	card, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title: "Inherits", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	assert.True(t, card.Autonomous)
	assert.True(t, card.MaxCapability)
	assert.Equal(t, 3, card.MobParticipants)
	assert.Equal(t, []string{"review"}, card.MobPhases)
	assert.False(t, card.CreatePR)
	assert.True(t, card.AwaitCI)
	assert.True(t, card.AwaitCopilotReview)
	assert.Equal(t, 0, card.BestOfN, "best_of_n is never a project default")
}

func TestCreateCard_BuiltinDefaultsWhenProjectHasNone(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title: "Builtin", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	assert.False(t, card.Autonomous)
	assert.True(t, card.CreatePR, "built-in create_pr is on for top-level cards")
	assert.Equal(t, 0, card.MobParticipants)
	assert.False(t, card.AwaitCI)
}

func TestCreateCard_ExplicitValuesBeatProjectCardDefaults(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	setProjectCardDefaults(t, svc, &board.CardDefaults{
		Autonomous: true, MobParticipants: 3, MobPhases: []string{"review"}, CreatePR: new(false), AwaitCI: true,
	})

	card, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title: "Explicit", Type: "task", Priority: "medium",
		Autonomous: new(false), CreatePR: new(true), AwaitCI: new(false),
		MobParticipants: new(0),
	})
	require.NoError(t, err)

	assert.False(t, card.Autonomous, "explicit false must win over a true default")
	assert.True(t, card.CreatePR)
	assert.False(t, card.AwaitCI)
	assert.Equal(t, 0, card.MobParticipants)
	assert.Empty(t, card.MobPhases, "an explicit seat count carries its own phases (none here)")
}

func TestCreateCard_ExplicitMobSeatsUseExplicitPhases(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	setProjectCardDefaults(t, svc, &board.CardDefaults{MobParticipants: 3, MobPhases: []string{"review"}})

	card, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title: "Mob", Type: "task", Priority: "medium",
		MobParticipants: new(4), MobPhases: []string{"plan", "review"},
	})
	require.NoError(t, err)

	assert.Equal(t, 4, card.MobParticipants)
	assert.Equal(t, []string{"plan", "review"}, card.MobPhases)
}

func TestCreateCard_SubtasksDoNotInheritProjectCardDefaults(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	setProjectCardDefaults(t, svc, &board.CardDefaults{
		Autonomous: true, MaxCapability: true, MobParticipants: 3, MobPhases: []string{"review"}, AwaitCI: true,
	})

	ctx := context.Background()
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "Parent", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	sub, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Sub", Type: "task", Priority: "medium", Parent: parent.ID,
	})
	require.NoError(t, err)

	assert.False(t, sub.Autonomous)
	assert.False(t, sub.MaxCapability)
	assert.Equal(t, 0, sub.MobParticipants)
	assert.Empty(t, sub.MobPhases)
	assert.False(t, sub.CreatePR, "subtasks never open their own PR")
	assert.False(t, sub.AwaitCI)
}
