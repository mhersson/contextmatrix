package sentinel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func TestRefusal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"a refused transition", &board.ValidationError{Err: board.ErrInvalidTransition}, refusedByProject},
		{"no path to the state", fmt.Errorf("find transition path: %w", &board.ValidationError{Err: board.ErrNoPath}), refusedByProject},
		{"a state the project lacks", &board.ValidationError{Err: board.ErrInvalidState}, refusedByProject},
		{"a guarded write lost", fmt.Errorf("write: %w", service.ErrCardChanged), refusedByChange},
		{"a claim", fmt.Errorf("agent authorization: %w", lock.ErrAgentMismatch), refusedByChange},
		{"a fenced claim", service.ErrClaimFenced, refusedByChange},
		{"a deleted card", storage.ErrCardNotFound, refusedByChange},
		{"a playbook run stopped", fmt.Errorf("%w: rollout", playbookrun.ErrRunInactive), refusedByChange},
		{"a playbook run played", fmt.Errorf("%w: rollout is running", service.ErrPlaybookRunActive), refusedByChange},
		{"a playbook made unrunnable", service.ErrPlaybookNotRunnable, refusedByChange},
		{"anything else", errors.New("open /srv/boards/alpha: permission denied"), refusedOther},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, refusal(tc.err))
		})
	}
}

func TestRefusedKeepsTheRawErrorOutOfItsText(t *testing.T) {
	raw := errors.New("commit /srv/boards/.git: push to https://git.example/boards.git failed")

	err := refused(context.Background(), "cannot move ALPHA-001 to todo", raw)

	assert.Contains(t, err.Error(), "ALPHA-001")
	assert.Contains(t, err.Error(), refusedOther)
	assert.NotContains(t, err.Error(), "/srv/boards")
	assert.NotContains(t, err.Error(), "git.example")
	assert.ErrorIs(t, err, raw, "the raw error stays reachable for errors.Is")
}
