package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/gitops"
)

// noopCommitChan returns a closed channel that yields a single nil,
// used as a synchronous stand-in when there is nothing to commit
// (auto-commit disabled, or the change was deferred).
func noopCommitChan() <-chan error {
	ch := make(chan error, 1)
	ch <- nil

	close(ch)

	return ch
}

// cardPath returns the relative path for a card file (for git operations).
// Paths are relative to the boards directory (which is the git repo root).
func (s *CardService) cardPath(project, id string) string {
	return filepath.Join(project, "tasks", id+".md")
}

// commitMessage formats a commit message with optional agent prefix.
func commitMessage(agentID, cardID, action string) string {
	if agentID != "" {
		return fmt.Sprintf("[agent:%s] %s: %s", agentID, cardID, action)
	}

	return fmt.Sprintf("[contextmatrix] %s: %s", cardID, action)
}

// commitCardChange either commits a card file immediately or records it for a
// deferred commit, depending on the gitDeferredCommit setting.
// Caller must hold writeMu.
//
// This is the legacy synchronous path retained for callers that do not
// participate in the async-commit flow. When a commit queue is configured,
// prefer enqueueCardCommit so writeMu can be released before the commit
// actually runs.
func (s *CardService) commitCardChange(ctx context.Context, project, cardID, agentID, action string) error {
	if !s.gitAutoCommit {
		return nil
	}

	path := s.cardPath(project, cardID)
	if s.gitDeferredCommit {
		// Accumulate path for later flush; skip the git commit for now.
		s.deferredPaths[cardID] = append(s.deferredPaths[cardID], path)

		return nil
	}

	msg := commitMessage(agentID, cardID, action)

	if s.commitQueue != nil {
		done := s.commitQueue.Enqueue(gitops.CommitJob{
			Project: project,
			Kind:    gitops.CommitKindFile,
			Path:    path,
			Message: msg,
			Ctx:     ctx,
		})
		if err := <-done; err != nil {
			return err
		}

		s.notifyCommit()

		return nil
	}

	if err := s.git.CommitFile(ctx, path, msg); err != nil {
		return err
	}

	s.notifyCommit()

	return nil
}

// enqueueCardCommit enqueues a card-change commit without waiting for it to
// complete, returning a channel that yields the commit result plus a bool
// telling the caller whether notifyCommit should fire on success (true only
// when a real commit was actually scheduled — not for no-ops or deferred).
// Caller must hold writeMu while invoking this (so the enqueue itself is
// serialized); the caller may then release writeMu before awaiting the
// returned channel, which is the whole point of the async path.
func (s *CardService) enqueueCardCommit(ctx context.Context, project, cardID, agentID, action string) (<-chan error, bool) {
	if !s.gitAutoCommit {
		return noopCommitChan(), false
	}

	path := s.cardPath(project, cardID)
	if s.gitDeferredCommit {
		s.deferredPaths[cardID] = append(s.deferredPaths[cardID], path)

		return noopCommitChan(), false
	}

	msg := commitMessage(agentID, cardID, action)

	if s.commitQueue != nil {
		return s.commitQueue.Enqueue(gitops.CommitJob{
			Project: project,
			Kind:    gitops.CommitKindFile,
			Path:    path,
			Message: msg,
			Ctx:     ctx,
		}), true
	}

	// No queue configured — run the commit synchronously inline and
	// return a pre-resolved channel. This preserves the original
	// ordering semantics (commit happens before the caller continues)
	// for tests/callers that never wire a queue.
	err := s.git.CommitFile(ctx, path, msg)

	done := make(chan error, 1)
	done <- err

	close(done)

	return done, true
}

// awaitCommit reads a commit result and invokes notifyCommit on success
// when shouldNotify is true. A small helper to keep caller sites tight.
func (s *CardService) awaitCommit(done <-chan error, shouldNotify bool) error {
	if err := <-done; err != nil {
		return err
	}

	if shouldNotify {
		s.notifyCommit()
	}

	return nil
}

// flushDeferredCommit stages all accumulated deferred paths for cardID and
// produces a single commit. No-ops if there are no deferred paths.
// Caller must hold writeMu (or be in a context where no concurrent mutations occur).
//
// Routes through the commit queue when configured so the flush cannot race
// against an in-flight rebase or push (queue pause covers both).
func (s *CardService) flushDeferredCommit(ctx context.Context, cardID, agentID string) error {
	project := ""
	// Derive project from the first accumulated path: "project/tasks/ID.md".
	if paths := s.deferredPaths[cardID]; len(paths) > 0 {
		project = firstPathProject(paths[0])
	}

	return s.flushDeferredCommitForProject(ctx, project, cardID, agentID)
}

// flushDeferredCommitForProject is the implementation. Separated so callers
// that already know the project can pass it in directly.
func (s *CardService) flushDeferredCommitForProject(ctx context.Context, project, cardID, agentID string) error {
	if !s.gitAutoCommit || !s.gitDeferredCommit {
		return nil
	}

	paths := s.deferredPaths[cardID]
	if len(paths) == 0 {
		return nil
	}
	// Deduplicate paths (same file may appear multiple times).
	seen := make(map[string]bool, len(paths))

	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	msg := commitMessage(agentID, cardID, "completed (deferred commit)")

	if s.commitQueue != nil {
		done := s.commitQueue.Enqueue(gitops.CommitJob{
			Project:     project,
			Kind:        gitops.CommitKindFilesShell,
			Paths:       unique,
			Message:     msg,
			ReloadAfter: true,
			Ctx:         ctx,
		})
		if err := <-done; err != nil {
			return err
		}
		// Delete paths only after a successful commit.
		delete(s.deferredPaths, cardID)

		s.notifyCommit()

		return nil
	}

	// Use shell git instead of go-git to avoid stale in-memory state after
	// shell-based push/rebase operations by the gitsync layer.
	if err := s.git.CommitFilesShell(ctx, unique, msg); err != nil {
		return err
	}
	// Delete paths only after a successful commit — prevents data loss if
	// the commit fails.
	delete(s.deferredPaths, cardID)
	// Refresh go-git's in-memory repo state so subsequent read operations
	// (e.g. GetLastCommitMessage) see the shell-git commit.
	if err := s.git.ReloadRepo(ctx); err != nil {
		ctxlog.Logger(ctx).Warn("reload repo after deferred flush", "card_id", cardID, "error", err)
	}

	s.notifyCommit()

	return nil
}

// rollbackCardOnCommitFailure restores a card's cache + disk state to the
// provided snapshot after a failed git commit. Intended for the commit-await
// pattern in the service layer: store.UpdateCard succeeds, the commit is
// enqueued and awaited, the commit fails, and we need to undo the store
// write so the cache + disk no longer describe a state that was never
// committed.
//
// Returns the error to surface to the caller:
//
//   - Rollback succeeded: returns commitErr wrapped in a "git commit" message.
//   - Rollback also failed: returns errors.Join(commitErr, rollbackErr) with
//     a note that the rollback failed and the state is now inconsistent. The
//     caller's slog.Error log line (see below) records the exact failure
//     mode for operators to investigate.
//
// The snapshot must be a deep copy owned by the caller (store.GetCard
// returns one, which is the intended source).
func (s *CardService) rollbackCardOnCommitFailure(
	ctx context.Context, project string, snapshot *board.Card, commitErr error,
) error {
	if snapshot == nil {
		// Defensive: nothing to roll back to.
		ctxlog.Logger(ctx).Error("commit failed without snapshot; cache/disk state unknown",
			"project", project, "error", commitErr)

		return fmt.Errorf("git commit (no snapshot for rollback): %w", commitErr)
	}

	if rollbackErr := s.store.UpdateCard(ctx, project, snapshot); rollbackErr != nil {
		ctxlog.Logger(ctx).Error("commit failed and rollback failed; cache + disk inconsistent",
			"project", project,
			"card_id", snapshot.ID,
			"committed", false,
			"rollback_failed", true,
			"commit_error", commitErr,
			"rollback_error", rollbackErr,
		)

		return errors.Join(
			fmt.Errorf("git commit (rollback failed, state inconsistent): %w", commitErr),
			fmt.Errorf("rollback: %w", rollbackErr),
		)
	}

	ctxlog.Logger(ctx).Warn("commit failed; rolled back cache + disk to pre-mutation state",
		"project", project,
		"card_id", snapshot.ID,
	)

	return fmt.Errorf("git commit: %w", commitErr)
}

// firstPathProject extracts the leading project segment from a path like
// "project/tasks/ID.md". Returns "" if the path is malformed.
func firstPathProject(path string) string {
	// filepath.Separator is platform-specific; deferred paths are stored
	// using filepath.Join, so this is OS-correct.
	for i := 0; i < len(path); i++ {
		if path[i] == filepath.Separator {
			return path[:i]
		}
	}

	return ""
}
