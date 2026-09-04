package service

import (
	"context"
	"errors"
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

// ErrForceReleaseRequiresHuman is returned when a non-human agent attempts to force-release a claim.
var ErrForceReleaseRequiresHuman = fmt.Errorf("force-release requires human agent (agent_id must start with %q)", board.HumanAgentIDPrefix)

// ErrClaimFenced is returned when a write needs a claim this instance holds
// but the remote has not confirmed the lease for longer than lease_timeout, so
// a peer may already have stalled or taken over the card. It wraps
// lock.ErrAgentMismatch so callers see the lost-claim error they already
// handle; heartbeats are exempt because a successful sync clears the fence.
var ErrClaimFenced = fmt.Errorf("claim lease not confirmed on the remote: %w", lock.ErrAgentMismatch)

// claimPreconditions loads the card and applies the checks that precede a
// claim: an unvetted import is off limits to agents, and a terminal card is
// only re-claimable by its holder. Returns the pre-claim snapshot.
//
// Caller must hold writeMu, so a concurrent transition cannot slip past the
// terminal-state check.
func (s *CardService) claimPreconditions(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	// Block non-human agents from claiming cards that have an external source
	// but have not been vetted. This prevents prompt-injection attacks where a
	// malicious issue body could instruct an agent to perform unintended actions.
	if !board.IsHumanAgentID(agentID) && snapshot.Source != nil && !snapshot.Vetted {
		return nil, fmt.Errorf("claim card: %w", ErrCardNotVetted)
	}

	// A terminal card is finished work - done, or a human's deliberate
	// not_planned. Claiming one is how a cancelled card gets picked up and
	// reimplemented, so refuse: it must be moved back to todo first.
	//
	// The holder is exempt: an agent keeps its claim through done until
	// ReleaseCard flushes deferred commits, and a re-claim there is a heartbeat
	// refresh rather than a new agent adopting the card. not_planned clears the
	// claim on entry, so nothing can be exempt in that state. The exemption
	// requires a claim that actually exists - agent_id is only length-checked,
	// so an empty one would otherwise match an unclaimed card's empty
	// assigned_agent.
	if board.IsTerminalState(snapshot.State) && !snapshot.ClaimHeldBy(agentID, s.instanceFor(project)) {
		return nil, fmt.Errorf("claim card %s in state %s: %w", id, snapshot.State, ErrCardTerminal)
	}

	return snapshot, nil
}

// ClaimCard assigns a card to an agent.
// Flow: lock claim → store update → git commit → publish event.
// On a shared board the claim runs inside a sync cycle so the remote holds it
// before the agent is told it may start work.
func (s *CardService) ClaimCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	if err := validateAgentIDFormat(agentID); err != nil {
		return nil, err
	}

	r := s.repoOf(project)

	if r.pushVerified() {
		return s.claimCardVerified(ctx, project, id, agentID)
	}

	s.writeMu.Lock()

	snapshot, err := s.claimPreconditions(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, err
	}

	// Claim via lock manager (returns modified card)
	card, err := r.Lock.Claim(ctx, project, id, agentID)
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

	if err := s.awaitCommit(r, commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	// Fresh claims on parent/standalone cards start the run timer;
	// same-card re-claims by a resuming agent keep the original start.
	if snapshot.AssignedAgent == "" && snapshot.Parent == "" {
		s.recordRunStart(project, id)
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

// claimCardVerified takes the claim inside a sync cycle, so the merge has run
// before the epoch is bumped and the remote holds the result before the caller
// is told it owns the card.
func (s *CardService) claimCardVerified(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	var snapshot, written *board.Card

	r := s.repoOf(project)

	_, err := s.runVerified(ctx, r, "claim card",
		func(ctx context.Context) error {
			snap, err := s.claimPreconditions(ctx, project, id, agentID)
			if err != nil {
				return err
			}

			card, err := r.Lock.Claim(ctx, project, id, agentID)
			if err != nil {
				return fmt.Errorf("claim card: %w", err)
			}

			if err := s.store.UpdateCard(ctx, project, card); err != nil {
				return fmt.Errorf("update card: %w", err)
			}

			if err := s.commitNow(ctx, []string{s.cardPath(project, id)}, commitMessage(agentID, id, "claimed")); err != nil {
				return s.rollbackCardOnCommitFailure(ctx, project, snap, err)
			}

			snapshot, written = snap, card

			return nil
		},
		func(ctx context.Context) error {
			return s.undoClaimWrite(ctx, project, id, written, snapshot, "claim undone: remote unreachable")
		})
	if err != nil {
		return nil, err
	}

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card after claim: %w", err)
	}

	// The merge may have handed a double claim to the instance that claimed
	// first, or a peer's takeover may have outranked ours.
	if !s.OwnsClaim(card, agentID) {
		r.Lock.ClearBeat(project, id)

		return nil, fmt.Errorf("claim card %s: %w: taken by %s via %s during sync",
			id, lock.ErrAlreadyClaimed, card.AssignedAgent, card.ClaimedVia)
	}

	// Fresh claims on parent/standalone cards start the run timer;
	// same-card re-claims by a resuming agent keep the original start.
	if snapshot.AssignedAgent == "" && snapshot.Parent == "" {
		s.recordRunStart(project, id)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardClaimed,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: s.clk.Now(),
	})
	s.overlayLiveness(card)

	return card, nil
}

// undoClaimWrite restores the claim tuple a verified write changed, when the
// card still carries exactly that write. A card the merge has moved on is
// left alone: the resolver already decided it. The pre-write epoch is
// restored, not bumped, so a peer's claim made meanwhile outranks the undone
// state in the next merge.
func (s *CardService) undoClaimWrite(ctx context.Context, project, id string, written, snapshot *board.Card, reason string) error {
	if written == nil {
		return nil
	}

	cur, err := s.store.GetCard(ctx, project, id)
	if errors.Is(err, storage.ErrCardNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	if cur.ClaimEpoch != written.ClaimEpoch || cur.AssignedAgent != written.AssignedAgent ||
		cur.ClaimedVia != written.ClaimedVia || cur.State != written.State {
		return nil
	}

	cur.AssignedAgent, cur.ClaimedVia, cur.ClaimedAt, cur.LastHeartbeat = snapshot.AssignedAgent, snapshot.ClaimedVia, snapshot.ClaimedAt, snapshot.LastHeartbeat
	cur.State, cur.WorkerStatus, cur.Phase, cur.ClaimEpoch = snapshot.State, snapshot.WorkerStatus, snapshot.Phase, snapshot.ClaimEpoch
	cur.Updated = s.clk.Now()
	cur.ActivityLog = board.TrimActivityLog(append(cur.ActivityLog,
		board.ActivityEntry{Agent: "system", Action: "sync", Timestamp: cur.Updated, Message: reason}))

	if err := s.store.UpdateCard(ctx, project, cur); err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	s.repoOf(project).Lock.ClearBeat(project, id)

	return s.commitNow(ctx, []string{s.cardPath(project, id)}, commitMessage("", id, reason))
}

// ReleaseCard removes an agent's claim on a card.
func (s *CardService) ReleaseCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)
	r := s.repoOf(project)

	s.writeMu.Lock()

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	if err := s.fenced(snapshot); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("release card: %w", err)
	}

	// Release via lock manager (returns modified card)
	card, err := r.Lock.Release(ctx, project, id, agentID)
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

	if err := s.awaitCommit(r, commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	s.observeRunEnd(project, card, releaseOutcome(card.State))

	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: s.clk.Now(),
	})

	return card, nil
}

// ForceReleaseCard clears another agent's claim on a card on behalf of a
// human operator rescuing a crashed or wedged worker. The card state is left
// untouched and no container is stopped.
func (s *CardService) ForceReleaseCard(ctx context.Context, project, id, humanID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	if !board.IsHumanAgentID(humanID) {
		return nil, fmt.Errorf("force-release card %s: %w", id, ErrForceReleaseRequiresHuman)
	}

	r := s.repoOf(project)

	if r.pushVerified() {
		return s.forceReleaseVerified(ctx, project, id, humanID)
	}

	s.writeMu.Lock()

	snapshot, card, prevAgent, err := s.forceReleaseLocked(ctx, project, id, humanID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, err
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, id, humanID, "force-released")

	s.writeMu.Unlock()

	// Await the force-release commit before flushing the dead agent's
	// deferred commits - same ordering as stallCardLocked: flushing first
	// means a rollback on commit failure diverges from git permanently.
	if err := s.awaitCommit(r, commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	s.writeMu.Lock()
	flushErr := s.flushDeferredCommit(ctx, id, prevAgent)
	s.writeMu.Unlock()

	if flushErr != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit on force-release", "card_id", id, "error", flushErr)
	}

	s.observeRunEnd(project, card, "force_released")

	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     humanID,
		Timestamp: s.clk.Now(),
		Data: map[string]any{
			"previous_agent": prevAgent,
			"forced":         true,
		},
	})

	return card, nil
}

// forceReleaseLocked clears the claim and appends the audit entry, returning
// the pre-release snapshot, the written card and the agent that held it.
// Caller must hold writeMu and owns the commit.
func (s *CardService) forceReleaseLocked(
	ctx context.Context, project, id, humanID string,
) (snapshot, written *board.Card, prevAgent string, err error) {
	// Snapshot for rollback on commit failure.
	snapshot, err = s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get card snapshot: %w", err)
	}

	card, prevAgent, err := s.repoOf(project).Lock.ForceRelease(ctx, project, id)
	if err != nil {
		return nil, nil, "", fmt.Errorf("force-release card: %w", err)
	}

	// The worker is presumed dead - same normalization as the stall path.
	// Leaving queued/running would 409 every future run trigger, and with
	// the claim gone the stall sweep would never correct it.
	if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
		card.WorkerStatus = "failed"
	}

	// Appended before the store write so the audit entry and the claim-clear
	// land in a single commit.
	card.ActivityLog = append(card.ActivityLog, board.ActivityEntry{
		Agent:     humanID,
		Timestamp: card.Updated,
		Action:    "force_released",
		Message:   fmt.Sprintf("Force-released claim held by %s", prevAgent),
	})
	card.ActivityLog = board.TrimActivityLog(card.ActivityLog)

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, nil, "", fmt.Errorf("update card: %w", err)
	}

	return snapshot, card, prevAgent, nil
}

// forceReleaseVerified clears the claim inside a sync cycle so the remote
// holds the release before the operator is told the card is free. A shared
// board never defers commits, so there is nothing to flush here.
func (s *CardService) forceReleaseVerified(ctx context.Context, project, id, humanID string) (*board.Card, error) {
	var (
		snapshot, written *board.Card
		prevAgent         string
	)

	r := s.repoOf(project)

	_, err := s.runVerified(ctx, r, "force-release",
		func(ctx context.Context) error {
			snap, card, prev, err := s.forceReleaseLocked(ctx, project, id, humanID)
			if err != nil {
				return err
			}

			if err := s.commitNow(ctx, []string{s.cardPath(project, id)}, commitMessage(humanID, id, "force-released")); err != nil {
				return s.rollbackCardOnCommitFailure(ctx, project, snap, err)
			}

			snapshot, written, prevAgent = snap, card, prev

			return nil
		},
		func(ctx context.Context) error {
			return s.undoClaimWrite(ctx, project, id, written, snapshot, "force-release undone: remote unreachable")
		})
	if err != nil {
		return nil, err
	}

	s.observeRunEnd(project, written, "force_released")

	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     humanID,
		Timestamp: s.clk.Now(),
		Data: map[string]any{
			"previous_agent": prevAgent,
			"forced":         true,
		},
	})

	return written, nil
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
func (s *CardService) HeartbeatCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)
	r := s.repoOf(project)

	s.writeMu.Lock()

	// Heartbeat via lock manager (returns modified card)
	card, persist, err := r.Lock.Heartbeat(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("heartbeat card: %w", err)
	}

	// On a shared board the beat lives in memory until the file lease is
	// older than lease_interval; the ack still reports the live value.
	if !persist {
		s.writeMu.Unlock()
		s.overlayLiveness(card)

		return card, nil
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer, silent, no event)
	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "heartbeat")

	s.writeMu.Unlock()

	if err := s.awaitCommit(r, commitDone, notify); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	return card, nil
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

	for _, r := range s.repos {
		stalled, err := r.Lock.FindStalled(ctx)
		if err != nil {
			return fmt.Errorf("find stalled in %s: %w", r.Name, err)
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
	}

	// FindStalled only covers CLAIMED cards. A parent card is never itself
	// claimed (only its subtasks are), so a dead run leaves it in_progress +
	// unclaimed forever - invisible to the loop above. Reap those on the same
	// tick; log and continue so a janitor error never masks the stall sweep.
	if err := s.processAbandonedParents(ctx); err != nil {
		ctxlog.Logger(ctx).Error("process abandoned parents", "error", err)
	}

	s.processForeignStalls(ctx)

	return nil
}

// SweepStalled runs one stall pass now: heartbeat timeouts on this instance's
// claims, abandoned parents, and expired leases of other instances' claims.
func (s *CardService) SweepStalled(ctx context.Context) error {
	return s.processStalled(ctx)
}

// processAbandonedParents reaps parent cards left in_progress + unclaimed after
// their whole run died. FindStalled only flags claimed cards, but a parent is
// never itself claimed (only its subtasks are), so a dead run strands the
// parent in_progress with no heartbeat to ever time out. A parent is abandoned
// only when it is in_progress AND unclaimed AND untouched within the stall
// timeout AND has no active subtask - the last two guards prevent reaping a
// parent that is merely between subtask claims.
func (s *CardService) processAbandonedParents(ctx context.Context) error {
	cutoff := s.clk.Now().Add(-s.heartbeatTimeout)

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
	r := s.repoOf(sc.Project)

	s.writeMu.Lock()

	// Re-read card from store to avoid stale data (TOCTOU).
	card, err := s.store.GetCard(ctx, sc.Project, sc.Card.ID)
	if err != nil {
		s.writeMu.Unlock()
		// Card was deleted between FindStalled and now - skip silently.
		return nil
	}

	// Re-check if still stalled: agent may have sent a heartbeat in the
	// meantime, or the claim may since belong to a peer instance, which this
	// instance never stalls.
	if card.AssignedAgent == "" || card.ClaimedElsewhere(r.Instance) {
		s.writeMu.Unlock()

		return nil
	}

	if last := r.Lock.LastBeat(card); last != nil && s.clk.Now().Sub(*last) < s.heartbeatTimeout {
		s.writeMu.Unlock()

		return nil
	}

	// This instance has been out of contact for longer than lease_timeout, so
	// a peer may already hold the card. Sync first: a stall written now would
	// race the takeover at the same epoch, and a peer's own foreign-stall
	// sweep covers the card meanwhile.
	if r.Lock.Fenced(card) {
		s.writeMu.Unlock()
		ctxlog.Logger(ctx).Debug("stall skipped: own claim is fenced",
			"project", sc.Project, "card_id", card.ID, "agent", card.AssignedAgent)

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
	r := s.repoOf(project)

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
	card.ClearClaim()

	if r.sharedClaims() {
		card.ClaimEpoch++
	}

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

	r.Lock.ClearBeat(project, card.ID)

	commitDone, notify := s.enqueueCardCommit(ctx, project, card.ID, "", reason)

	s.writeMu.Unlock()

	// Await the stall commit FIRST. Flushing the deferred queue before the
	// stall commit lands would mean a rollback (commit failure) restores the
	// card snapshot while the deferred-flush commit is already in git -
	// permanent state divergence. Defer the flush until the stall commit
	// succeeds; on commit failure, the deferred paths remain queued and will
	// be picked up by the next mutation/release.
	if err := s.awaitCommit(r, commitDone, notify); err != nil {
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
	s.observeRunEnd(project, card, "stalled")

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
