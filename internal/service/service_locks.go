package service

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ErrCardNotVetted is returned when an agent tries to claim a card that has not been vetted for agent use.
var ErrCardNotVetted = fmt.Errorf("card has not been vetted for agent use")

// ClaimCard assigns a card to an agent.
// Flow: lock claim → store update → git commit → publish event.
func (s *CardService) ClaimCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	if err := validateAgentIDFormat(agentID); err != nil {
		return nil, err
	}

	// Block non-human agents from claiming cards that have an external source
	// but have not been vetted. This prevents prompt-injection attacks where a
	// malicious issue body could instruct an agent to perform unintended actions.
	if !board.IsHumanAgentID(agentID) {
		card, err := s.store.GetCard(ctx, project, id)
		if err != nil {
			return nil, fmt.Errorf("get card for vetting check: %w", err)
		}

		if card.Source != nil && !card.Vetted {
			return nil, fmt.Errorf("claim card: %w", ErrCardNotVetted)
		}
	}

	s.writeMu.Lock()

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	// Claim via lock manager (returns modified card)
	card, err := s.lock.Claim(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("claim card: %w", err)
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	// Enqueue the commit (or record deferred). writeMu stays held only for
	// the store write + enqueue; the commit itself runs on a worker after
	// we release the lock, so concurrent writers do not serialize on it.
	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "claimed")

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	s.bus.Publish(events.Event{
		Type:      events.CardClaimed,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: s.clk.Now(),
	})

	return card, nil
}

// ReleaseCard removes an agent's claim on a card.
func (s *CardService) ReleaseCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	// Release via lock manager (returns modified card)
	card, err := s.lock.Release(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("release card: %w", err)
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	// Enqueue the release commit.
	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "released")

	// Flush any remaining deferred commits (release is the end of a work
	// session). Flush is still synchronous-under-writeMu because it
	// involves a shell-git commit + reload that must fully serialize with
	// subsequent writes on the same card.
	flushErr := s.flushDeferredCommit(ctx, id, agentID)

	s.writeMu.Unlock()

	if flushErr != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit on release", "card_id", id, "error", flushErr)
	}

	if err := s.awaitCommit(commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: s.clk.Now(),
	})

	return card, nil
}

// HeartbeatCard updates the heartbeat timestamp for a claimed card.
//
// Heartbeats are the highest-frequency mutation in the system, so the write
// mutex is released as soon as the store write + commit enqueue have run.
// The commit itself is awaited after releasing writeMu, which lets
// heartbeats for different cards run concurrently through the per-project
// commit queue.
//
// No rollback on commit failure: heartbeats are self-healing. A failed
// commit leaves the cache/disk with a newer LastHeartbeat timestamp than
// git; the next heartbeat (typically within the heartbeat interval) will
// emit another commit and restore consistency. Rolling back would be net
// harmful - the cache's advanced timestamp still prevents the stall
// scanner from prematurely marking the card, and a rollback would
// re-expose a stale timestamp that the next scan could act on.
func (s *CardService) HeartbeatCard(ctx context.Context, project, id, agentID string) error {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	// Heartbeat via lock manager (returns modified card)
	card, err := s.lock.Heartbeat(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("heartbeat card: %w", err)
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer, silent, no event)
	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "heartbeat")

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// StartTimeoutChecker starts a background goroutine that periodically
// checks for stalled cards and transitions them to the "stalled" state.
// The goroutine stops when the context is cancelled.
//
// The ticker is driven by the service's clock.Clock - tests that inject a
// fake clock can call Advance to deterministically trigger iterations.
// The ticker is created synchronously before the goroutine starts so that
// tests can Advance immediately after this call returns without racing
// against goroutine startup.
func (s *CardService) StartTimeoutChecker(ctx context.Context, interval time.Duration) {
	ticker := s.clk.NewTicker(interval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				ctxlog.Logger(ctx).Info("timeout checker stopped")

				return
			case <-ticker.C():
				func() {
					defer func() {
						if r := recover(); r != nil {
							ctxlog.Logger(ctx).Error("timeout checker panicked", "panic", r, "stack", string(debug.Stack()))
						}
					}()

					if err := s.stalledFn(ctx); err != nil {
						ctxlog.Logger(ctx).Error("process stalled cards", "error", err)
					}
				}()
			}
		}
	}()

	ctxlog.Logger(ctx).Info("timeout checker started", "interval", interval)
}

// processStalled finds and handles all stalled cards.
// Design note: FindStalled runs without writeMu, then markCardStalled acquires
// it per card. A heartbeat or release between the two could change the card, so
// markCardStalled re-reads and re-validates before acting. This is an accepted
// trade-off - holding writeMu across the entire loop would block all mutations
// during stalled-card processing, which is worse for throughput.
func (s *CardService) processStalled(ctx context.Context) error {
	start := time.Now()

	defer func() { metrics.StallScanDuration.Observe(time.Since(start).Seconds()) }()

	stalled, err := s.lock.FindStalled(ctx)
	if err != nil {
		return fmt.Errorf("find stalled: %w", err)
	}

	for _, sc := range stalled {
		if err := s.markCardStalled(ctx, sc); err != nil {
			ctxlog.Logger(ctx).Error("mark card stalled",
				"project", sc.Project,
				"card_id", sc.Card.ID,
				"error", err,
			)
			// Continue processing other cards
		}
	}

	// FindStalled only covers CLAIMED cards. A parent card is never itself
	// claimed (only its subtasks are), so a dead run leaves it in_progress +
	// unclaimed forever - invisible to the loop above. Reap those on the same
	// tick; log and continue so a janitor error never masks the stall sweep.
	if err := s.processAbandonedParents(ctx); err != nil {
		ctxlog.Logger(ctx).Error("process abandoned parents", "error", err)
	}

	return nil
}

// processAbandonedParents reaps parent cards left in_progress + unclaimed after
// their whole run died. FindStalled only flags claimed cards, but a parent is
// never itself claimed (only its subtasks are), so a dead run strands the
// parent in_progress with no heartbeat to ever time out. A parent is abandoned
// only when it is in_progress AND unclaimed AND untouched within the stall
// timeout AND has no active subtask - the last two guards prevent reaping a
// parent that is merely between subtask claims.
func (s *CardService) processAbandonedParents(ctx context.Context) error {
	cutoff := s.clk.Now().Add(-s.lock.Timeout())

	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	for _, proj := range projects {
		cards, err := s.store.ListCards(ctx, proj.Name, storage.CardFilter{})
		if err != nil {
			return fmt.Errorf("list cards for %s: %w", proj.Name, err)
		}

		for _, card := range cards {
			// Cheap pre-filter without the write lock; reapAbandonedParent
			// re-validates every guard authoritatively under writeMu.
			if card.State != board.StateInProgress || card.AssignedAgent != "" {
				continue
			}

			if card.Updated.After(cutoff) {
				continue // touched within the stall window
			}

			active, err := s.hasActiveSubtask(ctx, proj.Name, card.ID)
			if err != nil {
				ctxlog.Logger(ctx).Error("abandoned-parent scan: list subtasks",
					"project", proj.Name, "card_id", card.ID, "error", err)

				continue
			}

			if active {
				continue // still has runnable/claimed work
			}

			if err := s.reapAbandonedParent(ctx, proj.Name, card.ID, cutoff); err != nil {
				ctxlog.Logger(ctx).Error("reap abandoned parent",
					"project", proj.Name, "card_id", card.ID, "error", err)
			}
		}
	}

	return nil
}

// hasActiveSubtask reports whether any child of parentID still carries runnable
// work: it is claimed, or in an active board state (todo/in_progress/review).
// done/stalled/not_planned do not count.
func (s *CardService) hasActiveSubtask(ctx context.Context, project, parentID string) (bool, error) {
	subs, err := s.store.ListCards(ctx, project, storage.CardFilter{Parent: parentID})
	if err != nil {
		return false, fmt.Errorf("list subtasks: %w", err)
	}

	for _, sub := range subs {
		if sub.AssignedAgent != "" {
			return true, nil
		}

		switch sub.State {
		case board.StateTodo, board.StateInProgress, board.StateReview:
			return true, nil
		}
	}

	return false, nil
}

// reapAbandonedParent stalls a single abandoned parent. It re-reads the card and
// re-validates every abandonment guard under writeMu - a claim, transition, or
// subtask update may have landed since the unlocked scan - before delegating the
// mutation to stallCardLocked. writeMu is released on every return path.
func (s *CardService) reapAbandonedParent(ctx context.Context, project, cardID string, cutoff time.Time) error {
	s.writeMu.Lock()

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()
		// Deleted between scan and reap - skip silently.
		return nil
	}

	// Re-validate under the lock: still an untouched, unclaimed in_progress parent?
	if card.State != board.StateInProgress || card.AssignedAgent != "" || card.Updated.After(cutoff) {
		s.writeMu.Unlock()

		return nil
	}

	active, err := s.hasActiveSubtask(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return err
	}

	if active {
		s.writeMu.Unlock()

		return nil
	}

	return s.stallCardLocked(ctx, project, card, "stalled (abandoned run)")
}

// markCardStalled transitions a CLAIMED card to the "stalled" state after its
// heartbeat timed out. It re-validates the live claim (TOCTOU) before handing
// the mutation to stallCardLocked.
func (s *CardService) markCardStalled(ctx context.Context, sc lock.StalledCard) error {
	s.writeMu.Lock()

	// Re-read card from store to avoid stale data (TOCTOU).
	card, err := s.store.GetCard(ctx, sc.Project, sc.Card.ID)
	if err != nil {
		s.writeMu.Unlock()
		// Card was deleted between FindStalled and now - skip silently.
		return nil
	}

	// Re-check if still stalled: agent may have sent a heartbeat in the meantime.
	if card.AssignedAgent == "" {
		s.writeMu.Unlock()

		return nil
	}

	if card.LastHeartbeat != nil && s.clk.Now().Sub(*card.LastHeartbeat) < s.lock.Timeout() {
		s.writeMu.Unlock()

		return nil
	}

	return s.stallCardLocked(ctx, sc.Project, card, "stalled (heartbeat timeout)")
}

// stallCardLocked performs the card→stalled mutation shared by markCardStalled
// (heartbeat-timed-out claimed cards) and reapAbandonedParent (abandoned
// in_progress + unclaimed parents). writeMu MUST be held on entry; it is
// released on every return path. The card is the caller's fresh, writeMu-guarded
// re-read. reason is the commit/audit message for the stall (each caller passes
// its own so the git history distinguishes a heartbeat timeout from a reap).
func (s *CardService) stallCardLocked(ctx context.Context, project string, card *board.Card, reason string) error {
	// Defense-in-depth: never re-stall a card that has already reached a
	// terminal state. Per the design tension noted in
	// enforceTerminalStateInvariants, a card may legitimately retain a live
	// claim through StateDone so the subsequent ReleaseCard call can flush
	// deferred commits - that done-with-claim window must not be flagged
	// stalled. StateNotPlanned already clears the claim on transition so
	// this guard is symmetric and free of behavioural impact for it.
	// StateStalled itself is included for idempotency.
	if card.State == board.StateDone || card.State == board.StateNotPlanned || card.State == board.StateStalled {
		s.writeMu.Unlock()

		return nil
	}

	// Snapshot for rollback on commit failure. card is a deep copy but we
	// are about to mutate it in place, so capture the pre-mutation state
	// by loading a second copy.
	snapshot, err := s.store.GetCard(ctx, project, card.ID)
	if err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("get card snapshot: %w", err)
	}

	previousAgent := card.AssignedAgent
	previousState := card.State

	card.State = board.StateStalled
	card.AssignedAgent = ""
	card.LastHeartbeat = nil

	// A stalled worker is presumed dead. Leaving worker_status at queued/running
	// makes runCard 409 (ErrCodeWorkerConflict) on every future trigger until a
	// manual Stop - normalize to the terminal status the failed-callback path
	// would have set. Terminal/blank statuses are left untouched.
	if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
		card.WorkerStatus = "failed"
	}

	card.Updated = s.clk.Now()

	if previousState != board.StateStalled {
		appendStateChangeLog(card, previousState, board.StateStalled, "", card.Updated)
	}

	// Validate the post-mutation card so card-level invariants (state-enum,
	// agent/heartbeat consistency) hold even though the stall path bypasses
	// the per-project transition map.
	cfg, cfgErr := s.getConfig(ctx, project)
	if cfgErr != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("get project config: %w", cfgErr)
	}

	if err := s.validateStalledCardFn(cfg, card); err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("validate stalled card: %w", err)
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, card.ID, "", reason)

	s.writeMu.Unlock()

	// Await the stall commit FIRST. Flushing the deferred queue before the
	// stall commit lands would mean a rollback (commit failure) restores the
	// card snapshot while the deferred-flush commit is already in git -
	// permanent state divergence. Defer the flush until the stall commit
	// succeeds; on commit failure, the deferred paths remain queued and will
	// be picked up by the next mutation/release.
	if err := s.awaitCommit(commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return rollbackErr
	}

	// Stall commit landed - now safe to flush deferred commits. Re-acquire
	// writeMu because flushDeferredCommit mutates deferredPaths and routes
	// through the commit queue.
	s.writeMu.Lock()
	flushErr := s.flushDeferredCommit(ctx, card.ID, previousAgent)
	s.writeMu.Unlock()

	if flushErr != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit after stall", "card_id", card.ID, "error", flushErr)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardStalled,
		Project:   project,
		CardID:    card.ID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"previous_agent": previousAgent,
		},
	})

	metrics.StallCardsMarked.Inc()

	ctxlog.Logger(ctx).Info("card marked stalled",
		"project", project,
		"card_id", card.ID,
		"previous_agent", previousAgent,
	)

	return nil
}

// validateAgentIDFormat checks that an agent ID is within length limits.
func validateAgentIDFormat(agentID string) error {
	if len(agentID) > maxAgentIDLen {
		return fmt.Errorf("agent_id length %d exceeds limit of %d: %w", len(agentID), maxAgentIDLen, ErrFieldTooLong)
	}

	return nil
}
