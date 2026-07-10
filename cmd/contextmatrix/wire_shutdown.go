package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	ghimport "github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/gitsync"
)

// shutdownComponents groups the live objects touched during the multi-phase
// shutdown sequence. Fields are set by main() immediately before calling
// runShutdownSequence.
type shutdownComponents struct {
	// HTTPServer is the main public-facing HTTP server. Never nil.
	HTTPServer *http.Server

	// AdminServer is the optional admin/metrics listener. May be nil when
	// cfg.AdminPort == 0.
	AdminServer *http.Server

	// SessionLog manages backend SSE log sessions; must be closed after HTTP drain
	// so subscribers receive a terminal event instead of a mid-stream EOF.
	SessionLog *sessionlog.Manager

	// CommitQueue must be drained after ctx cancel so buffered writes still
	// reach disk before exit.
	CommitQueue *gitops.CommitQueue

	// Syncer is the board-repo background syncer. May be nil when the boards
	// repo has no remote.
	Syncer *gitsync.Syncer

	// GHSyncer is the GitHub-issue background syncer. May be nil when issue
	// importing is disabled.
	GHSyncer *ghimport.Syncer

	// HTTPCancel cancels the httpCtx passed to http.Server.BaseContext, causing
	// long-lived SSE connections to see r.Context().Done() and exit before
	// server.Shutdown blocks waiting on them.
	HTTPCancel context.CancelFunc

	// AppCancel cancels the long-lived application context, signalling the
	// timeout checker, syncers, and backend subscribers to wind down.
	AppCancel context.CancelFunc
}

// runShutdownSequence executes the five-phase ordered shutdown:
//  1. http_drain      — cancel SSE contexts, drain in-flight HTTP requests.
//  2. sessionlog_close — close backend SSE log sessions with terminal events.
//  3. ctx_cancel      — cancel the long-lived application context.
//  4. commit_queue_close — flush buffered commits to disk.
//  5. syncers_drain   — wait for git syncers to finish any late push.
//
// Returns the main HTTP server's shutdown error, if any (caller should
// os.Exit(1) on non-nil). All other phase errors are logged and swallowed
// so the remaining phases always run.
func runShutdownSequence(ctx context.Context, c shutdownComponents) error {
	shutdownStart := time.Now()

	// Phase 1: stop accepting new HTTP connections and drain in-flight
	// requests. Cancel httpCtx first so SSE handlers see r.Context().Done()
	// and exit immediately instead of blocking until the shutdown timeout.
	slog.Info("shutdown: phase=http_drain")
	c.HTTPCancel()

	var (
		wg              sync.WaitGroup
		mainShutdownErr error
	)

	if c.AdminServer != nil {
		wg.Go(func() {
			if err := c.AdminServer.Shutdown(ctx); err != nil {
				slog.Error("admin server shutdown error", "error", err)
			}
		})
	}

	wg.Go(func() {
		mainShutdownErr = c.HTTPServer.Shutdown(ctx)
	})

	wg.Wait()

	// Phase 2: drain active backend SSE log sessions. HTTP is no longer accepting
	// new connections, so closing these pumps is safe — every subscriber
	// receives a terminal SSE event instead of a mid-stream EOF.
	slog.Info("shutdown: phase=sessionlog_close")

	if err := c.SessionLog.Close(ctx); err != nil {
		slog.Error("session manager shutdown error", "error", err)
	}

	// Phase 3: signal the rest of the app (timeout checker, syncers'
	// periodic loops, backend subscribers) to wind down.
	slog.Info("shutdown: phase=ctx_cancel")
	c.AppCancel()

	// Phase 4: drain the commit queue so any writes that landed on the
	// worker channel — but whose go-git commit had not yet started when
	// ctx was cancelled — still make it to disk before we exit. Running
	// this before the syncers' Wait ensures the on-disk commits exist to
	// be pushed by a final push iteration.
	slog.Info("shutdown: phase=commit_queue_close")

	if err := c.CommitQueue.Close(ctx); err != nil {
		slog.Error("commit queue shutdown error", "error", err)
	}

	// Phase 5: let the git syncers finish any late commit/push triggered by
	// requests that were in flight when HTTP drain began. Running this after
	// HTTP drain (not before) ensures those late mutations still get pushed
	// to the remote before we exit.
	//
	// Each syncer.Wait() is bounded by a per-phase deadline so a wedged
	// subprocess (e.g. a git push that ignores the cancelled ctx) cannot hang
	// shutdown past systemd's TimeoutStopSec. The root ctx.cancel() above is
	// still the primary signal; this wait-timeout is the safety net.
	slog.Info("shutdown: phase=syncers_drain")

	const phase5Timeout = 10 * time.Second

	phase5Ctx, phase5Cancel := context.WithTimeout(context.Background(), phase5Timeout)
	defer phase5Cancel()

	if c.Syncer != nil {
		if err := waitSyncer(phase5Ctx, c.Syncer.Wait); err != nil {
			slog.Error("shutdown: gitsync syncer drain exceeded budget",
				"phase", "syncers_drain",
				"timeout", phase5Timeout,
				"error", err,
			)
		}
	}

	if c.GHSyncer != nil {
		if err := waitSyncer(phase5Ctx, c.GHSyncer.Wait); err != nil {
			slog.Error("shutdown: github syncer drain exceeded budget",
				"phase", "syncers_drain",
				"timeout", phase5Timeout,
				"error", err,
			)
		}
	}

	// gitops.Manager has no Close method today; if it grows one, call it
	// here after the syncers have finished pushing.

	duration := time.Since(shutdownStart)
	slog.Info("shutdown: complete", "duration", duration)

	return mainShutdownErr
}
