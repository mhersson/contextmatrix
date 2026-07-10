package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// verifyBaseInput reuses remoteExecBaseInput's valid states/types/transitions
// for the "test-project" fixture, carrying only the verify config under test.
func verifyBaseInput(v *board.VerifyConfig) UpdateProjectInput {
	in := remoteExecBaseInput(nil)
	in.Verify = v

	return in
}

func TestUpdateProject_VerifyPreserveSetClear(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	projectDir := filepath.Join(tmpDir, "boards", "test-project")

	// Set: a non-nil verify lands on the config and round-trips to disk.
	cfg, err := svc.UpdateProject(ctx, "test-project", verifyBaseInput(&board.VerifyConfig{
		Command: "make test", TimeoutSeconds: 600, Env: []string{"JAVA_HOME"},
	}))
	require.NoError(t, err)
	require.NotNil(t, cfg.Verify)
	assert.Equal(t, "make test", cfg.Verify.Command)

	reloaded, err := board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Verify)
	assert.Equal(t, "make test", reloaded.Verify.Command)
	assert.Equal(t, 600, reloaded.Verify.TimeoutSeconds)

	// Preserve: a nil pointer leaves the config untouched.
	cfg, err = svc.UpdateProject(ctx, "test-project", verifyBaseInput(nil))
	require.NoError(t, err)
	require.NotNil(t, cfg.Verify, "nil verify must preserve the existing config")
	assert.Equal(t, "make test", cfg.Verify.Command)

	// Clear: a non-nil zero-value verify normalizes away to nil.
	cfg, err = svc.UpdateProject(ctx, "test-project", verifyBaseInput(&board.VerifyConfig{}))
	require.NoError(t, err)
	assert.Nil(t, cfg.Verify, "zero-value verify must normalize to nil")

	reloaded, err = board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	assert.Nil(t, reloaded.Verify, "cleared verify must be absent from .board.yaml on disk")
}

func TestUpdateProject_VerifyValidationRejected(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.UpdateProject(context.Background(), "test-project", verifyBaseInput(&board.VerifyConfig{
		Command: "make test", Env: []string{"API_KEY"},
	}))
	require.Error(t, err)
	assert.ErrorIs(t, err, board.ErrInvalidProjectConfig)
}

func TestUpdateProject_VerifyTrimmedAndNormalized(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	cfg, err := svc.UpdateProject(context.Background(), "test-project", verifyBaseInput(&board.VerifyConfig{
		Command: "  make test  ",
	}))
	require.NoError(t, err)
	require.NotNil(t, cfg.Verify)
	assert.Equal(t, "make test", cfg.Verify.Command, "command must be trimmed on save")
}

func TestCreateCard_Verify(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "verify card", Type: "task", Priority: "medium",
		Verify: &board.VerifyConfig{Command: "  go test ./...  ", TimeoutSeconds: 120},
	})
	require.NoError(t, err)
	require.NotNil(t, card.Verify)
	assert.Equal(t, "go test ./...", card.Verify.Command, "command trimmed on create")
	assert.Equal(t, 120, card.Verify.TimeoutSeconds)

	// Zero-value verify normalizes to nil on create.
	card, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "no verify card", Type: "task", Priority: "medium",
		Verify: &board.VerifyConfig{},
	})
	require.NoError(t, err)
	assert.Nil(t, card.Verify)
}

func TestCreateCard_VerifyValidationRejected(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title: "bad verify", Type: "task", Priority: "medium",
		Verify: &board.VerifyConfig{Command: "x", Env: []string{"bad-name"}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidVerify)
}

func TestPatchCard_VerifyPreserveSetClear(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "patch verify", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	require.Nil(t, card.Verify)

	// Set via patch.
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Verify: &board.VerifyConfig{Command: "make check", TimeoutSeconds: 300},
	})
	require.NoError(t, err)
	require.NotNil(t, patched.Verify)
	assert.Equal(t, "make check", patched.Verify.Command)

	// Preserve: a patch that omits verify leaves it intact.
	highPriority := "high"
	patched, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Priority: &highPriority,
	})
	require.NoError(t, err)
	require.NotNil(t, patched.Verify, "omitting verify must preserve it")
	assert.Equal(t, "make check", patched.Verify.Command)

	// Clear: a zero-value verify normalizes away.
	patched, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Verify: &board.VerifyConfig{},
	})
	require.NoError(t, err)
	assert.Nil(t, patched.Verify, "zero-value verify clears the card override")
}

func TestPatchCard_VerifyValidationRejected(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "patch bad verify", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Verify: &board.VerifyConfig{Command: "line1\nline2"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidVerify)
}
