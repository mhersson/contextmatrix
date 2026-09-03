package service

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// enforceTerminalStateInvariants applies the claim rules of a state change:
//
//   - not_planned releases the claim tuple. It is a manual terminal state
//     and no agent will be active on it.
//   - On a shared board every terminal state bumps claim_epoch, so the
//     completion outranks a stale stall or takeover in a merge.
//
// done keeps the claim in both modes: the holder releases it afterwards
// (complete_task, the workflow skills' mandatory release_card), which on a
// private board also flushes deferred commits. Four tests pin that contract:
// TestDeferredCommitFlushOnDone, TestDeferredCommitParentManualReviewTransition,
// TestDeferredCommitBoardYamlIncluded and
// TestUpdateWorkerStatus_FailedAfterTerminalNormalizesToCompleted. The
// stall checker skips terminal cards, so a done card with a live claim is
// never flagged stalled.
//
// worker_status is intentionally not cleared here: the end-session subscriber
// and the backend's own terminal callback own that field.
func enforceTerminalStateInvariants(card *board.Card, stateChanged, shared bool) {
	if !stateChanged {
		return
	}

	if card.State == board.StateNotPlanned {
		card.ClearClaim()
	}

	if shared && board.IsTerminalState(card.State) {
		card.ClaimEpoch++
	}
}

// applyTerminalInvariants is enforceTerminalStateInvariants plus the lease
// bookkeeping that belongs with it: a cancelled card carries no claim, so the
// live beat and confirmation stamp this instance kept for it go too.
func (s *CardService) applyTerminalInvariants(project string, card *board.Card, stateChanged bool) {
	r := s.repoOf(project)

	enforceTerminalStateInvariants(card, stateChanged, r.sharedClaims())

	if stateChanged && card.State == board.StateNotPlanned {
		r.Lock.ClearBeat(project, card.ID)
	}
}

// applyStateChangeSideEffects runs post-commit side effects that fire when a
// card's State has changed. Currently this flushes any accumulated deferred
// commits when the card reaches a state where no subsequent Release or
// markCardStalled call will trigger a flush on its own - namely not_planned
// and review.
//
// Errors are logged (not returned) so a flush failure never blocks the caller's
// primary mutation, which has already been persisted and committed. Safe to
// call when stateChanged is false - no-op in that case.
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
// is held. It does NOT acquire writeMu - callers must hold it.
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
// valid transition path. Called while writeMu is held; per step it releases
// writeMu before awaiting the commit and re-acquires it before the next
// iteration. writeMu is held on entry and held on return.
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

	for i, state := range path {
		// Re-load the parent at the start of every iteration after the
		// first so concurrent writes during the previous step's commit
		// await are not silently clobbered.
		if i > 0 {
			refreshed, err := s.store.GetCard(ctx, parent.Project, parent.ID)
			if err != nil {
				return fmt.Errorf("refresh parent card: %w", err)
			}

			parent = refreshed
		}

		oldState := parent.State
		parent.State = state
		parent.Updated = s.clk.Now()

		// Record the transition on the activity log so the dashboard sparkline
		// reconstruction sees parent auto-transitions too (mirrors
		// applyCardMutation). "system" - auto-transitions carry no agent.
		appendStateChangeLog(parent, oldState, state, "system", parent.Updated)

		// State-change invariants: release claim on not_planned, clear
		// worker_status on terminal states.
		s.applyTerminalInvariants(parent.Project, parent, true)

		if err := s.store.UpdateCard(ctx, parent.Project, parent); err != nil {
			return fmt.Errorf("persist parent card: %w", err)
		}

		// Enqueue under writeMu so per-project ordering is preserved
		// relative to the child's commit and any other in-flight writes.
		commitDone, notify := s.enqueueCardCommit(ctx, parent.Project, parent.ID, "", "auto-transitioned to "+state)

		// Flush deferred commits on not_planned/review under writeMu so
		// deferredPaths stays serialized; the flush itself routes through
		// the queue so per-project ordering covers it.
		s.applyStateChangeSideEffects(ctx, parent, true)

		// Release writeMu before awaiting the parent's commit so a slow
		// commit does not stall other concurrent writers. Re-acquire
		// before continuing so the caller's lock-held invariant holds.
		s.writeMu.Unlock()
		commitErr := s.awaitCommit(s.repoOf(parent.Project), commitDone, notify)
		s.writeMu.Lock()

		if commitErr != nil {
			metrics.ParentAutoTransitionErrors.Inc()
			ctxlog.Logger(ctx).Warn("parent auto-transition commit failed",
				"parent_id", parent.ID,
				"child_id", childID,
				"target_state", state,
				"from_state", oldState,
				"error", fmt.Errorf("git commit for parent auto-transition: %w", commitErr),
			)
		}

		// Parent auto-transitions are always system-driven (fired from the
		// child write path, no agent context). Tag with "system" so SSE
		// consumers can render the row consistently with the activity log
		// entry written by appendStateChangeLog.
		s.bus.Publish(events.Event{
			Type:      events.CardStateChanged,
			Project:   parent.Project,
			CardID:    parent.ID,
			Agent:     "system",
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
