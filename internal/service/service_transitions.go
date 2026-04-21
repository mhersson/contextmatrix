package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// enforceTerminalStateInvariants clears fields that must be reset when a card
// enters a terminal-ish state. Called before persisting a state change.
//
//   - not_planned: release agent claim so the lock manager won't treat the card
//     as stalled. not_planned is a manual terminal state — no agent will be
//     active on it.
//   - done / not_planned: clear runner_status — the runner is no longer
//     associated with a finished card.
//
// Safe to call regardless of stateChanged — it only acts when the card's
// current state is one of the targets. But callers should pass stateChanged
// so we only mutate when there is actually a transition.
func enforceTerminalStateInvariants(card *board.Card, stateChanged bool) {
	if !stateChanged {
		return
	}

	if card.State == board.StateNotPlanned {
		card.AssignedAgent = ""
		card.LastHeartbeat = nil
	}

	if card.State == board.StateDone || card.State == board.StateNotPlanned {
		card.RunnerStatus = ""
	}
}

// applyStateChangeSideEffects runs post-commit side effects that fire when a
// card's State has changed. Currently this flushes any accumulated deferred
// commits when the card reaches a state where no subsequent Release or
// markCardStalled call will trigger a flush on its own — namely not_planned
// and review.
//
// Errors are logged (not returned) so a flush failure never blocks the caller's
// primary mutation, which has already been persisted and committed. Safe to
// call when stateChanged is false — no-op in that case.
//
// Caller must hold writeMu.
func (s *CardService) applyStateChangeSideEffects(ctx context.Context, card *board.Card, stateChanged bool) {
	if !stateChanged {
		return
	}

	if card.State != board.StateNotPlanned && card.State != board.StateReview {
		return
	}

	if err := s.flushDeferredCommit(ctx, card.ID, ""); err != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit after state change",
			"card_id", card.ID, "state", card.State, "error", err)
	}
}

// maybeTransitionParent checks if a child's state change should trigger a
// parent state transition. Called after any child state change while writeMu
// is held. It does NOT acquire writeMu — callers must hold it.
//
// Rules:
//   - child moved to in_progress AND parent is in todo → transition parent to in_progress
//
// The parent does NOT auto-transition to review when all subtasks are done.
// The orchestrator spawns a documentation sub-agent (while the parent is still
// in_progress) and then manually transitions the parent to review.
func (s *CardService) maybeTransitionParent(ctx context.Context, child *board.Card) {
	if child.Parent == "" {
		return
	}

	parent, err := s.store.GetCard(ctx, child.Project, child.Parent)
	if err != nil {
		ctxlog.Logger(ctx).Warn("parent auto-transition: get parent card",
			"parent_id", child.Parent,
			"child_id", child.ID,
			"error", err,
		)

		return
	}

	if child.State == board.StateInProgress {
		if parent.State == board.StateTodo {
			if err := s.transitionParentDirect(ctx, parent, board.StateInProgress, child.ID); err != nil {
				ctxlog.Logger(ctx).Error("parent auto-transition failed: todo→in_progress",
					"parent_id", parent.ID,
					"child_id", child.ID,
					"error", err,
				)
			}
		}
	}
}

// transitionParentDirect transitions a parent card to the target state,
// persists it, commits to git, and publishes events. It walks the shortest
// valid transition path. Called while writeMu is held — does NOT re-acquire it.
//
// Commit failures are intentionally not returned: parent auto-transitions
// are fire-and-forget from the child write path, so bubbling the error up
// would surface a rollback requirement that the caller cannot express
// (the child's commit already succeeded). Instead, each failed commit
// increments metrics.ParentAutoTransitionErrors and logs a Warn with
// parent_id, child_id, target_state, and the wrapped error so operators
// can alert on sustained failures.
func (s *CardService) transitionParentDirect(
	ctx context.Context, parent *board.Card, targetState, childID string,
) error {
	if parent.State == targetState {
		return nil
	}

	cfg, err := s.getConfig(ctx, parent.Project)
	if err != nil {
		return fmt.Errorf("get project config: %w", err)
	}

	validator := s.validator

	path, err := validator.FindShortestPath(cfg, parent.State, targetState)
	if err != nil {
		return fmt.Errorf("find transition path from %s to %s: %w", parent.State, targetState, err)
	}

	for _, state := range path {
		oldState := parent.State
		parent.State = state
		parent.Updated = time.Now()

		// State-change invariants: release claim on not_planned, clear
		// runner_status on terminal states.
		enforceTerminalStateInvariants(parent, true)

		if err := s.store.UpdateCard(ctx, parent.Project, parent); err != nil {
			return fmt.Errorf("persist parent card: %w", err)
		}

		if err := s.commitCardChange(ctx, parent.Project, parent.ID, "", "auto-transitioned to "+state); err != nil {
			metrics.ParentAutoTransitionErrors.Inc()
			ctxlog.Logger(ctx).Warn("parent auto-transition commit failed",
				"parent_id", parent.ID,
				"child_id", childID,
				"target_state", state,
				"from_state", oldState,
				"error", fmt.Errorf("git commit for parent auto-transition: %w", err),
			)
		}

		// Flush deferred commits on not_planned/review.
		s.applyStateChangeSideEffects(ctx, parent, true)

		s.bus.Publish(events.Event{
			Type:      events.CardStateChanged,
			Project:   parent.Project,
			CardID:    parent.ID,
			Timestamp: parent.Updated,
			Data: map[string]any{
				"old_state": oldState,
				"new_state": state,
			},
		})

		ctxlog.Logger(ctx).Info("parent auto-transitioned",
			"parent_id", parent.ID,
			"child_id", childID,
			"old_state", oldState,
			"new_state", state,
		)
	}

	return nil
}
