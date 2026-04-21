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
	if !isHumanAgent(agentID) {
		card, err := s.store.GetCard(ctx, project, strings.ToUpper(id))
		if err != nil {
			return nil, fmt.Errorf("get card for vetting check: %w", err)
		}

		if card.Source != nil && !card.Vetted {
			return nil, fmt.Errorf("claim card: %w", ErrCardNotVetted)
		}
	}

	s.writeMu.Lock()

	// Claim via lock manager (returns modified card)
	card, err := s.lock.Claim(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("claim card: %w", err)
	}

	// Persist
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
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardClaimed,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
	})

	return card, nil
}

// ReleaseCard removes an agent's claim on a card.
func (s *CardService) ReleaseCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	// Release via lock manager (returns modified card)
	card, err := s.lock.Release(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("release card: %w", err)
	}

	// Persist
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
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
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
func (s *CardService) HeartbeatCard(ctx context.Context, project, id, agentID string) error {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	// Heartbeat via lock manager (returns modified card)
	card, err := s.lock.Heartbeat(ctx, project, id, agentID)
	if err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("heartbeat card: %w", err)
	}

	// Persist
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
// The ticker is driven by the service's clock.Clock — tests that inject a
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
// trade-off — holding writeMu across the entire loop would block all mutations
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

	return nil
}

// markCardStalled transitions a card to the "stalled" state.
func (s *CardService) markCardStalled(ctx context.Context, sc lock.StalledCard) error {
	s.writeMu.Lock()

	// Re-read card from store to avoid stale data (TOCTOU).
	card, err := s.store.GetCard(ctx, sc.Project, sc.Card.ID)
	if err != nil {
		s.writeMu.Unlock()
		// Card was deleted between FindStalled and now — skip silently.
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

	previousAgent := card.AssignedAgent

	// Update card state
	card.State = board.StateStalled
	card.AssignedAgent = ""
	card.LastHeartbeat = nil
	card.Updated = time.Now()

	// Persist
	if err := s.store.UpdateCard(ctx, sc.Project, card); err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, sc.Project, card.ID, "", "stalled (heartbeat timeout)")

	// Flush any deferred commits since card is now in a final state. Runs
	// under writeMu because the flush mutates deferredPaths.
	flushErr := s.flushDeferredCommit(ctx, card.ID, previousAgent)

	s.writeMu.Unlock()

	if flushErr != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit after stall", "card_id", card.ID, "error", flushErr)
	}

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardStalled,
		Project:   sc.Project,
		CardID:    card.ID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"previous_agent": previousAgent,
		},
	})

	metrics.StallCardsMarked.Inc()

	ctxlog.Logger(ctx).Info("card marked stalled",
		"project", sc.Project,
		"card_id", card.ID,
		"previous_agent", previousAgent,
	)

	return nil
}

// isHumanAgent returns true if the agent ID represents a human user.
// Human agent IDs are prefixed with "human:" (e.g. "human:alice").
func isHumanAgent(agentID string) bool {
	return strings.HasPrefix(agentID, "human:")
}

// validateAgentIDFormat checks that an agent ID is within length limits.
func validateAgentIDFormat(agentID string) error {
	if len(agentID) > maxAgentIDLen {
		return fmt.Errorf("agent_id length %d exceeds limit of %d: %w", len(agentID), maxAgentIDLen, ErrFieldTooLong)
	}

	return nil
}
