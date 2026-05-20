package main

import (
	"context"
	"log/slog"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// wireGitSync constructs the board-repo background syncer, performs the
// startup pull, wires the on-commit notification hook, and starts the sync
// loop. Returns nil when the boards repo has no remote (sync is disabled).
// The caller is responsible for waiting on syncer.Wait() during shutdown
// (after context cancellation).
func wireGitSync(
	ctx context.Context,
	cfg *config.Config,
	gitMgr *gitops.Manager,
	store *storage.FilesystemStore,
	svc *service.CardService,
	bus *events.Bus,
) *gitsync.Syncer {
	if !gitMgr.HasRemote() {
		return nil
	}

	pullInterval, _ := cfg.PullIntervalDuration()

	syncer := gitsync.NewSyncer(gitMgr, store, svc, bus, cfg.Boards.Dir,
		cfg.Boards.GitAutoPull, cfg.Boards.GitAutoPush, pullInterval)
	if syncer == nil {
		return nil
	}

	if err := syncer.PullOnStartup(ctx); err != nil {
		slog.Warn("initial pull failed", "error", err)
	}

	if cfg.Boards.GitAutoPush {
		svc.SetOnCommit(syncer.NotifyCommit)
	}

	syncer.Start(ctx)

	slog.Info("git sync initialized",
		"auto_pull", cfg.Boards.GitAutoPull,
		"auto_push", cfg.Boards.GitAutoPush,
		"pull_interval", pullInterval,
	)

	return syncer
}
