package service

import (
	"context"
	"fmt"
	"log/slog"
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

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
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
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
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

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
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
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
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

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	// Post-terminal cleanup normalization: once the card has reached a
	// terminal state (done/not_planned), the reconcile sweep and end-session
	// subscriber kill the container as a cleanup step. The runner reports
	// that cleanup through the same callback path it uses for a genuine
	// mid-run failure — "failed: killed by operator". Recording that as
	// `failed` would lie about the run (the work succeeded; only the
	// container lingered past the card's done transition). Translate such
	// post-terminal failure/killed callbacks to `completed` so the card UI
	// reflects what actually happened.
	//
	// The user-initiated Stop path (stopTask → UpdateRunnerStatus("killed"))
	// targets non-terminal cards, so the normalization only fires for the
	// cleanup case it is intended for. If a user manages to Stop a card that
	// is already done (rare race — human clicking just after the agent
	// transitions), that is semantically identical to the sweep cleanup
	// anyway: the work is already done, the container is being reaped.
	//
	// The activity log message is rewritten alongside the status so the UI
	// doesn't display "failed: killed by operator" on a card recorded as
	// completed — the two would contradict each other otherwise.
	if (status == "failed" || status == "killed") &&
		(card.State == board.StateDone || card.State == board.StateNotPlanned) {
		ctxlog.Logger(ctx).Info("normalizing post-terminal cleanup callback to completed",
			"card_id", cardID, "project", project,
			"card_state", card.State, "incoming_status", status, "message", message)

		status = "completed"
		message = "container cleaned up after run completed"
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
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
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

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return nil, fmt.Errorf("get card snapshot: %w", err)
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
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
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

// SkillEngagedDedupWindow is how long after a skill_engaged entry is recorded
// the service suppresses subsequent duplicates for the same card+skill.
// Tunable via package-level var so tests can override.
var SkillEngagedDedupWindow = 60 * time.Second

// RecordSkillEngaged appends a skill_engaged activity log entry for the
// given card+skill, suppressing duplicates within SkillEngagedDedupWindow.
// Source-agnostic: handles entries from the runner callback path AND from
// agent-side add_log calls (Path A) via a single dedup point.
func (s *CardService) RecordSkillEngaged(ctx context.Context, project, cardID, skillName string) error {
	cardID = strings.ToUpper(cardID)

	s.writeMu.Lock()

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("get card: %w", err)
	}

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("get card snapshot: %w", err)
	}

	// Dedup: scan recent entries for a same-skill skill_engaged within the window.
	cutoff := time.Now().Add(-SkillEngagedDedupWindow)

	for i := len(card.ActivityLog) - 1; i >= 0; i-- {
		e := card.ActivityLog[i]
		if e.Timestamp.Before(cutoff) {
			break
		}

		if e.Action == "skill_engaged" && skillNameOf(e) == skillName {
			// Already logged within the window; suppress.
			s.writeMu.Unlock()

			return nil
		}
	}

	entry := board.ActivityEntry{
		Agent:     "runner",
		Timestamp: time.Now(),
		Action:    "skill_engaged",
		Message:   "engaged " + skillName,
		Skill:     skillName,
	}

	card.ActivityLog = append(card.ActivityLog, entry)
	if len(card.ActivityLog) > maxActivityLogEntries {
		card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		s.writeMu.Unlock()

		return fmt.Errorf("save card: %w", err)
	}

	commitDone, notify := s.enqueueCardCommit(ctx, project, cardID, "runner", "log: skill_engaged")

	s.writeMu.Unlock()

	if err := s.awaitCommit(commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return rollbackErr
	}

	slog.InfoContext(ctx, "skill engaged recorded",
		"project", project,
		"card_id", cardID,
		"skill", skillName,
	)

	s.bus.Publish(events.Event{
		Type:    events.CardLogAdded,
		Project: project,
		CardID:  cardID,
		Agent:   "runner",
		Data: map[string]any{
			"action":  "skill_engaged",
			"message": "engaged " + skillName,
		},
	})

	return nil
}

// skillNameOf extracts the skill name from an activity entry. Prefers the
// structured Skill field (set by the runner callback path); falls back to
// parsing "engaged X" from the message (set by agent-side add_log calls).
func skillNameOf(e board.ActivityEntry) string {
	if e.Skill != "" {
		return e.Skill
	}

	const prefix = "engaged "
	if strings.HasPrefix(e.Message, prefix) {
		return strings.TrimPrefix(e.Message, prefix)
	}

	return ""
}
