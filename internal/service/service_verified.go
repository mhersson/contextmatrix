package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/gitops"
)

// ErrRemoteUnreachable is returned by a push-verified mutation when the
// remote could not be reached or the push never landed within the request
// budget. The board is unchanged; the caller may retry.
var ErrRemoteUnreachable = errors.New("remote unreachable")

// ErrSyncRunnerPair is returned by a setter handed a sync runner without a
// direct commit or the other way around: the verified paths commit only
// through the bundle's direct commit, so a half-wired bundle would dereference
// nil on the first mutation.
var ErrSyncRunnerPair = errors.New("sync runner and direct commit must be set together")

const (
	// verifyAttempts bounds how many sync cycles one mutation may start
	// when a cycle fails before its write ran.
	verifyAttempts = 3
	verifyBackoff  = 500 * time.Millisecond
)

// SyncMutation is a write the syncer runs inside one shared cycle: Apply
// after the merge, under both write locks; Undo when the cycle fails after
// Apply ran, so a write the caller is told failed never reaches the remote
// on a later push. Undo runs under the same locks and must leave the tree
// clean. Both may be nil.
type SyncMutation struct {
	Apply func(ctx context.Context) error
	Undo  func(ctx context.Context) error
}

// SyncOutcome is what a sync cycle reports back to a mutation.
type SyncOutcome struct {
	BodyRan     bool
	Pushed      bool
	Resolutions []boardmerge.Resolution
}

// SyncRunner runs one shared sync cycle around a mutation. The syncer
// provides it; nil means a private board.
type SyncRunner func(ctx context.Context, trigger string, m SyncMutation) (SyncOutcome, error)

// DirectCommit commits paths synchronously with shell git, for writes that
// run while the commit queue is paused.
type DirectCommit func(ctx context.Context, paths []string, message string) error

// DirectCommitter builds a DirectCommit on a git manager, refreshing go-git's
// view afterwards so later go-git commits build on the shell commit.
func DirectCommitter(git *gitops.Manager) DirectCommit {
	return func(ctx context.Context, paths []string, message string) error {
		if err := git.CommitFilesShell(ctx, paths, message); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}

		if err := git.ReloadRepo(ctx); err != nil {
			ctxlog.Logger(ctx).Warn("reload repo after direct commit", "error", err)
		}

		return nil
	}
}

// SetSyncRunner routes push-verified mutations through the syncer. Must be
// called before the server starts accepting requests. Acts on the first
// configured repo; multi-repo wiring uses the For variant.
func (s *CardService) SetSyncRunner(run SyncRunner) {
	s.repos[0].runner = run
}

// commitNow commits synchronously with shell git in the repo the first path
// belongs to. It is the only commit path allowed inside a sync cycle: the
// queue is paused there, and a queued job would wait for a resume that only
// comes after the cycle returns.
func (s *CardService) commitNow(ctx context.Context, paths []string, message string) error {
	r := s.repoOf(firstPathProject(paths[0]))

	if !r.GitAutoCommit {
		return nil
	}

	return DirectCommitter(r.Git)(ctx, paths, message)
}

// commitAllReloaded stages everything in r and commits, then refreshes
// go-git. The project paths stage as a tree, which the path-scoped commitNow
// cannot express.
func (s *CardService) commitAllReloaded(r *BoardsRepo, msg string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := r.Git.CommitAll(ctx, msg); err != nil {
			return fmt.Errorf("commit all: %w", err)
		}

		if err := r.Git.ReloadRepo(ctx); err != nil {
			ctxlog.Logger(ctx).Warn("reload repo after direct commit", "repo", r.Name, "error", err)
		}

		return nil
	}
}

// runVerified runs apply inside a sync cycle: fetch and merge first, apply
// under the write locks, push, and undo when the push never lands. A cycle
// that fails before apply ran is retried within the request budget. apply's
// own error is returned unchanged; every other failure is the remote's and
// maps to ErrRemoteUnreachable.
//
// The caller must not hold writeMu or the playbook lock: the cycle takes
// both. r is the repo of the project being written, resolved by the caller.
func (s *CardService) runVerified(
	ctx context.Context, r *BoardsRepo, trigger string, apply, undo func(context.Context) error,
) (SyncOutcome, error) {
	var (
		applyErr error
		applied  bool
	)

	m := SyncMutation{
		Apply: func(ctx context.Context) error {
			applied = true
			applyErr = apply(ctx)

			return applyErr
		},
		Undo: undo,
	}

	var lastErr error

	for attempt := range verifyAttempts {
		out, err := r.runner(ctx, trigger, m)
		if applyErr != nil {
			return out, applyErr
		}

		if err == nil {
			// A cycle cannot report success without running the write it was
			// given; the callers below dereference what apply produced.
			if !applied {
				return out, fmt.Errorf("%s: %w: cycle reported success without running the write", trigger, ErrRemoteUnreachable)
			}

			return out, nil
		}

		lastErr = err

		// applied is tracked here rather than taken from the cycle's own
		// report, so a write that ran is never retried even if the runner
		// misreports it.
		if applied || out.BodyRan {
			break // the write ran and was undone; the caller decides whether to retry
		}

		if attempt < verifyAttempts-1 {
			base := time.Duration(attempt+1) * verifyBackoff
			wait := time.Duration(float64(base) * (0.75 + 0.5*rand.Float64())) //nolint:gosec // non-security jitter

			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return out, fmt.Errorf("%s: %w: %w", trigger, ErrRemoteUnreachable, ctx.Err())
			}
		}
	}

	return SyncOutcome{}, fmt.Errorf("%s: %w: %w", trigger, ErrRemoteUnreachable, lastErr)
}
