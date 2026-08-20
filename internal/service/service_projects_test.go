package service

import (
	"context"
	"testing"

	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProject_ReservedPlaybooksName(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	for _, name := range []string{"playbooks", "Playbooks"} {
		input := validCreateProjectInput()
		input.Name = name

		_, err := svc.CreateProject(ctx, input)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, storage.ErrInvalidInput, name)
	}
}

func TestCreateProject_ReservedPlaybooksNameFromDisplayName(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	input := validCreateProjectInput()
	input.Name = ""
	input.DisplayName = "Playbooks"

	_, err := svc.CreateProject(ctx, input)
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidInput)
}
