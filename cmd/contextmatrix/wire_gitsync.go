package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/service"
)

// wireGitSync constructs one background syncer per boards repository that
// has a remote, performs each startup pull, wires the on-commit hooks, and
// starts the loops. Repos without a remote get a group entry with no
// syncer, so /api/sync still lists them. The caller waits on Group.Wait
// during shutdown, after context cancellation.
//
// A wiring failure on a shared repo is returned as an error and no group
// is started: a shared repo without its sync runner degrades every
// push-verified mutation to an unverified local write. Private repos log
// and continue.
func wireGitSync(
	ctx context.Context,
	cfg *config.Config,
	boards *boardsBundles,
	svc *service.CardService,
	pbSvc *service.PlaybookService,
	bus *events.Bus,
) (*gitsync.Group, error) {
	entries := make([]gitsync.GroupEntry, 0, len(boards.repos))

	for _, b := range boards.repos {
		syncer, err := wireRepoSync(ctx, cfg, b, boards, svc, pbSvc, bus)
		if err != nil {
			return nil, fmt.Errorf("wire git sync: %w", err)
		}

		entries = append(entries, gitsync.GroupEntry{Name: b.cfg.Name, Syncer: syncer})
	}

	group := gitsync.NewGroup(boards.composite.Hidden, entries...)
	group.Start(ctx)

	return group, nil
}

// wireRepoSync builds and primes the syncer of one repo. Returns a nil
// syncer when the repo has no remote or a wiring step fails; for shared
// repos every wiring failure is also returned as an error so startup can
// abort, because a shared repo without its runner silently degrades to
// unverified local writes.
func wireRepoSync(
	ctx context.Context,
	cfg *config.Config,
	b *repoBundle,
	boards *boardsBundles,
	svc *service.CardService,
	pbSvc *service.PlaybookService,
	bus *events.Bus,
) (*gitsync.Syncer, error) {
	if !b.git.HasRemote() {
		slog.Info("git sync disabled: no remote configured", "repo", b.cfg.Name)

		return nil, nil
	}

	view, err := boards.composite.View(b.cfg.Name)
	if err != nil {
		if b.cfg.Shared {
			return nil, fmt.Errorf("boards repo %q: repo view: %w", b.cfg.Name, err)
		}

		slog.Error("git sync: repo view", "repo", b.cfg.Name, "error", err)

		return nil, nil
	}

	pullInterval, _ := b.cfg.PullIntervalDuration()

	opts := []gitsync.Option{gitsync.WithRepo(b.cfg.Name)}

	if b.cfg.Shared {
		leaseInterval, _ := b.cfg.LeaseIntervalDuration()
		opts = append(opts, gitsync.WithShared(cfg.Instance.ID), gitsync.WithLeaseInterval(leaseInterval))
	}

	syncer := gitsync.NewSyncer(b.git, view, svc, bus, b.cfg.Dir,
		b.cfg.GitAutoPull, b.cfg.GitAutoPush, pullInterval, opts...)
	if syncer == nil {
		return nil, nil
	}

	// Mutations that depend on a global decision (an ID, a slug, a claim)
	// run inside a sync cycle so the merge has happened before the write
	// and the remote holds it before the caller is told it landed.
	if b.cfg.Shared {
		runner := func(ctx context.Context, trigger string, m service.SyncMutation) (service.SyncOutcome, error) {
			r, err := syncer.SyncedMutation(ctx, trigger, m)

			return service.SyncOutcome{BodyRan: r.BodyRan, Pushed: r.Pushed, Resolutions: r.Resolutions}, err
		}

		if err := svc.SetSyncRunnerFor(b.cfg.Name, runner); err != nil {
			return nil, fmt.Errorf("boards repo %q: sync runner: %w", b.cfg.Name, err)
		}

		if pbSvc != nil {
			if err := pbSvc.SetSyncRunnerFor(b.cfg.Name, runner, service.DirectCommitter(b.git)); err != nil {
				return nil, fmt.Errorf("boards repo %q: playbook sync runner: %w", b.cfg.Name, err)
			}
		}
	}

	// SetPlaybooks must run before PullOnStartup so the initial pull already
	// quiesces and reloads the playbook index, not just later periodic pulls.
	if pbSvc != nil {
		syncer.SetPlaybooks(pbSvc.ForRepo(b.cfg.Name))
	}

	// The image index is set before the startup pull for the same reason:
	// files a peer pushed while this instance was down are indexed by the
	// reload that pull triggers.
	if b.images != nil {
		syncer.SetImages(b.images)
	}

	if err := syncer.PullOnStartup(ctx); err != nil {
		slog.Warn("initial pull failed", "repo", b.cfg.Name, "error", err)
	}

	if b.cfg.GitAutoPush {
		if err := svc.SetOnCommitFor(b.cfg.Name, syncer.NotifyCommit); err != nil {
			if b.cfg.Shared {
				return nil, fmt.Errorf("boards repo %q: on-commit hook: %w", b.cfg.Name, err)
			}

			slog.Error("git sync: on-commit hook", "repo", b.cfg.Name, "error", err)
		}

		// The startup sweep's commit was made before this syncer existed, so
		// no on-commit hook fired for it. The push channel is buffered, so a
		// notification queued here is delivered once the group starts.
		if b.recovered {
			syncer.NotifyCommit()
		}

		if pbSvc != nil {
			if err := pbSvc.SetOnCommitFor(b.cfg.Name, syncer.NotifyCommit); err != nil {
				if b.cfg.Shared {
					return nil, fmt.Errorf("boards repo %q: playbook on-commit hook: %w", b.cfg.Name, err)
				}

				slog.Error("git sync: playbook on-commit hook", "repo", b.cfg.Name, "error", err)
			}
		}
	}

	slog.Info("git sync initialized",
		"repo", b.cfg.Name,
		"auto_pull", b.cfg.GitAutoPull,
		"auto_push", b.cfg.GitAutoPush,
		"pull_interval", pullInterval,
		"shared", b.cfg.Shared,
		"instance", cfg.Instance.ID,
	)

	return syncer, nil
}
