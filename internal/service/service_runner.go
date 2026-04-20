package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
)

// ErrProtectedBranch is returned when an operation targets a protected branch (main/master).
var ErrProtectedBranch = fmt.Errorf("pushing to main/master is never allowed")

// ErrInvalidPRUrl is returned when a PR URL does not use http:// or https://.
var ErrInvalidPRUrl = fmt.Errorf("pr_url must use http or https scheme")

// ErrReviewAttemptsCapped is returned when the review_attempts counter has reached its limit.
var ErrReviewAttemptsCapped = fmt.Errorf("review attempts limit reached")

// ErrCardTerminal is returned when an operation is not allowed on a card in a terminal state (done/not_planned).
var ErrCardTerminal = fmt.Errorf("card is in a terminal state")

// ErrRunnerDisabled is returned when runner operations are attempted but the runner is not enabled.
var ErrRunnerDisabled = fmt.Errorf("remote execution is not enabled")

// ErrPromoteRequiresHuman is returned when a non-human agent attempts to promote a card to autonomous mode.
var ErrPromoteRequiresHuman = fmt.Errorf("promote requires human agent (agent_id must start with \"human:\")")

// isProtectedBranch returns true if the branch name resolves to main or master.
func isProtectedBranch(branch string) bool {
	normalized := strings.ToLower(strings.TrimSpace(branch))
	normalized = strings.TrimPrefix(normalized, "refs/heads/")

	return normalized == "main" || normalized == "master"
}

// RecordPush records a git push event on a card, updating PRUrl if provided and
// adding an activity log entry. All mutations are atomic under a single lock.
// Returns ErrProtectedBranch if the branch is main/master.
func (s *CardService) RecordPush(ctx context.Context, project, id, agentID, branch, prURL string) (*board.Card, error) {
	id = strings.ToUpper(id)

	// Service-layer branch protection — defense in depth.
	if isProtectedBranch(branch) {
		return nil, ErrProtectedBranch
	}

	// Validate PR URL scheme before acquiring the lock.
	if prURL != "" && !strings.HasPrefix(prURL, "https://") && !strings.HasPrefix(prURL, "http://") {
		return nil, ErrInvalidPRUrl
	}

	s.writeMu.Lock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != agentID {
		s.writeMu.Unlock()

		return nil, lock.ErrAgentMismatch
	}

	// Update PR URL if provided.
	if prURL != "" {
		card.PRUrl = prURL
	}

	// Append activity log entry.
	msg := "Pushed to branch " + branch
	if prURL != "" {
		msg += "; PR: " + prURL
	}

	entry := board.ActivityEntry{
		Agent:     agentID,
		Action:    "pushed",
		Message:   msg,
		Timestamp: time.Now(),
	}

	card.ActivityLog = append(card.ActivityLog, entry)
	if len(card.ActivityLog) > maxActivityLogEntries {
		card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "pushed to "+branch)

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUpdated,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
		Data: map[string]any{
			"action": "pushed",
			"branch": branch,
			"pr_url": prURL,
		},
	})

	return card, nil
}

// IncrementReviewAttempts atomically increments the review_attempts counter on a card.
// Returns lock.ErrAgentMismatch if the caller is not the assigned agent, and
// ErrReviewAttemptsCapped if the counter has reached maxReviewAttempts.
func (s *CardService) IncrementReviewAttempts(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != agentID {
		s.writeMu.Unlock()

		return nil, lock.ErrAgentMismatch
	}

	if card.ReviewAttempts >= maxReviewAttempts {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("review attempts capped at %d: %w", maxReviewAttempts, ErrReviewAttemptsCapped)
	}

	card.ReviewAttempts++

	card.Updated = time.Now()
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, id, agentID, "review_attempts incremented")

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUpdated,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
		Data: map[string]any{
			"action":          "review_attempts_incremented",
			"review_attempts": card.ReviewAttempts,
		},
	})

	return card, nil
}

// UpdateRunnerStatus sets the runner_status field on a card.
func (s *CardService) UpdateRunnerStatus(ctx context.Context, project, cardID, status, message string) (*board.Card, error) {
	cardID = strings.ToUpper(cardID)

	s.writeMu.Lock()

	if err := s.validator.ValidateRunnerStatus(status); err != nil {
		s.writeMu.Unlock()

		return nil, err
	}

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card: %w", err)
	}

	prevRunnerStatus := card.RunnerStatus
	card.RunnerStatus = status
	card.Updated = time.Now()

	// Clear agent claim on terminal runner statuses.
	if status == "failed" || status == "killed" || status == "completed" {
		card.AssignedAgent = ""
		card.LastHeartbeat = nil
	}
	// On completed, also clear runner_status since the run is over.
	if status == "completed" {
		card.RunnerStatus = ""
	}

	if message != "" {
		card.ActivityLog = append(card.ActivityLog, board.ActivityEntry{
			Agent:     "runner",
			Timestamp: time.Now(),
			Action:    "runner_status",
			Message:   message,
		})
		if len(card.ActivityLog) > maxActivityLogEntries {
			card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
		}
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, cardID, "runner", "runner_status: "+status)

	// Flush deferred commits only on terminal runner statuses (completed,
	// failed, killed). These occur after the agent has released the card, so
	// there is no subsequent flush point. Non-terminal statuses (queued,
	// running) happen during active work and should continue to defer.
	var flushErr error
	if status == "failed" || status == "killed" || status == "completed" {
		flushErr = s.flushDeferredCommit(ctx, cardID, "runner")
	}

	s.writeMu.Unlock()

	if flushErr != nil {
		ctxlog.Logger(ctx).Error("flush deferred commit on runner status update", "card_id", cardID, "error", flushErr)
	}

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Session lifecycle hooks — only when a manager is wired.
	if s.sessionManager != nil {
		switch {
		case prevRunnerStatus != "running" && status == "running":
			// Transition INTO running: open the upstream SSE buffer.
			if startErr := s.sessionManager.Start(ctx, cardID, project); startErr != nil {
				ctxlog.Logger(ctx).Error("sessionlog: Start failed on runner status update",
					"card_id", cardID, "project", project, "error", startErr)
			}
		case status == "failed" || status == "killed" || status == "completed":
			// Transition to terminal: drain and clear the buffer.
			// Fire-and-forget in a goroutine so Stop (which waits for the pump
			// to exit) does not hold the write lock.
			go s.sessionManager.Stop(cardID)
		}
	}

	var eventType events.EventType

	switch status {
	case "queued":
		eventType = events.RunnerTriggered
	case "running":
		eventType = events.RunnerStarted
	case "failed":
		eventType = events.RunnerFailed
	case "killed":
		eventType = events.RunnerKilled
	default:
		eventType = events.CardUpdated
	}

	s.bus.Publish(events.Event{
		Type:      eventType,
		Project:   project,
		CardID:    cardID,
		Timestamp: time.Now(),
		Data:      map[string]any{"runner_status": status},
	})

	return card, nil
}

// PromoteToAutonomous sets the Autonomous flag on a card to true and appends
// an activity log entry. It is idempotent: if the card is already autonomous,
// it returns the current card unchanged without writing a log entry or commit.
// Returns ErrCardTerminal if the card is in a terminal state (done/not_planned).
func (s *CardService) PromoteToAutonomous(ctx context.Context, project, cardID, agentID string) (*board.Card, error) {
	cardID = strings.ToUpper(cardID)

	s.writeMu.Lock()

	// Guard: only human agents (agent_id prefixed with "human:") may promote a card.
	// Checked before the store load so a rejected call has no side effects.
	if agentID == "" || !strings.HasPrefix(agentID, "human:") {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("promote card %s: %w", cardID, ErrPromoteRequiresHuman)
	}

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card: %w", err)
	}

	// Guard: cannot promote a card in a terminal state.
	if card.State == board.StateDone || card.State == board.StateNotPlanned {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("promote card %s: %w", cardID, ErrCardTerminal)
	}

	// Idempotent: if already autonomous, return current card without side effects.
	if card.Autonomous {
		s.writeMu.Unlock()
		s.enrichDependenciesMet(ctx, card)

		return card, nil
	}

	now := time.Now()

	card.Autonomous = true
	card.Updated = now

	// Append activity log entry (honoring the 50-entry cap).
	entry := board.ActivityEntry{
		Agent:     agentID,
		Timestamp: now,
		Action:    "promoted",
		Message:   "Promoted to autonomous mode",
	}

	card.ActivityLog = append(card.ActivityLog, entry)
	if len(card.ActivityLog) > maxActivityLogEntries {
		card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("update card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, cardID, agentID, "promoted to autonomous")

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUpdated,
		Project:   project,
		CardID:    cardID,
		Agent:     agentID,
		Timestamp: now,
		Data:      map[string]any{"autonomous": true},
	})

	s.enrichDependenciesMet(ctx, card)

	return card, nil
}
