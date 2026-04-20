package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

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
	if err := s.git.CommitFile(ctx, path, msg); err != nil {
		return err
	}

	s.notifyCommit()

	return nil
}

// flushDeferredCommit stages all accumulated deferred paths for cardID and
// produces a single commit. No-ops if there are no deferred paths.
// Caller must hold writeMu (or be in a context where no concurrent mutations occur).
func (s *CardService) flushDeferredCommit(ctx context.Context, cardID, agentID string) error {
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
