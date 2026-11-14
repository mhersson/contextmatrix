package sentinel

import (
	"context"
	"errors"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// The classes a refused write's park reason names. A park reason is committed
// with the card, so the raw error, which can carry paths and remote URLs, is
// only logged.
const (
	refusedByProject = "the project does not allow it"
	refusedByChange  = "it changed meanwhile"
	refusedByService = "the card service refused it"
	refusedOther     = "refused"
)

// refusal classes a refused write's error.
func refusal(err error) string {
	switch {
	case errors.Is(err, board.ErrInvalidTransition), errors.Is(err, board.ErrNoPath), errors.Is(err, board.ErrInvalidState):
		return refusedByProject
	case errors.Is(err, service.ErrCardChanged), errors.Is(err, lock.ErrAgentMismatch),
		errors.Is(err, storage.ErrCardNotFound), errors.Is(err, playbookrun.ErrRunInactive),
		errors.Is(err, service.ErrPlaybookRunActive), errors.Is(err, service.ErrPlaybookNotRunnable):
		return refusedByChange
	case errors.Is(err, service.ErrInvalidVerify), errors.Is(err, service.ErrFieldTooLong),
		errors.Is(err, service.ErrInvalidSectionPatch):
		return refusedByService
	}

	return refusedOther
}

// refusedError is a refused write whose text names only the step and the
// refusal's class.
type refusedError struct {
	step string
	err  error
}

func (e *refusedError) Error() string { return e.step + ": " + refusal(e.err) }

func (e *refusedError) Unwrap() error { return e.err }

// refused logs err whole and returns it as a refusal of step.
func refused(ctx context.Context, step string, err error) error {
	ctxlog.Logger(ctx).Warn("sentinel: write refused", "step", step, "error", err)

	return &refusedError{step: step, err: err}
}
