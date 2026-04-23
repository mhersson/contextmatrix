package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// CardLister is the subset of *service.CardService needed by the reconcile
// sweep. Defined locally so the sweep does not pull in the full service
// interface and so tests can inject a fake.
type CardLister interface {
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)
	ListCards(ctx context.Context, project string, filter storage.CardFilter) ([]*board.Card, error)
}

// StartReconciliationSweep launches a ticker goroutine that periodically
// scans every project for cards in a terminal state that still have a live
// runner_status and no agent, and forces the same /end-session + /kill
// sequence the event subscriber uses. Blocks only until the goroutine is
// scheduled; returns immediately.
//
// Rationale: the event subscriber is best-effort — events.Bus drops events
// when any subscriber's buffer is full, and events published while CM is
// restarting are never delivered. A leaked container would then sit alive
// until the runner's (default 2h) container_timeout. This sweep is the
// authoritative backstop: within one interval of a card reaching the
// terminal+released+active-runner_status shape, the sweep will kill the
// container whether or not the event subscriber fired.
//
// An interval of 0 disables the sweep entirely.
func StartReconciliationSweep(ctx context.Context, svc CardLister, client EndSessionClient, interval time.Duration, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	if interval <= 0 {
		logger.Info("reconcile sweep disabled", "interval", interval)

		return
	}

	if svc == nil || client == nil {
		logger.Warn("reconcile sweep not started: missing dependency",
			"svc_nil", svc == nil, "client_nil", client == nil)

		return
	}

	go func() {
		// Run an initial sweep immediately so containers orphaned by a CM
		// restart (events published while CM was down are never delivered)
		// are cleaned up without having to wait a full interval. Then switch
		// to the steady-state ticker.
		runReconcileSweep(ctx, svc, client, logger)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				runReconcileSweep(ctx, svc, client, logger)
			}
		}
	}()
}

// runReconcileSweep scans every project once and fires endSessionAndKill for
// every card that currently satisfies shouldEndSession. Safe to call ad-hoc
// from tests.
//
// Per-project ListCards failures are logged and the sweep continues with the
// next project — a single broken project must not stop the whole sweep.
func runReconcileSweep(ctx context.Context, svc CardLister, client EndSessionClient, logger *slog.Logger) {
	projects, err := svc.ListProjects(ctx)
	if err != nil {
		logger.Warn("reconcile sweep: list projects failed", "error", err)

		return
	}

	var scanned, killed int

	for _, p := range projects {
		// Filter in-memory rather than via storage.CardFilter — CardFilter
		// does not expose a runner_status field, and the typical project
		// has O(100) cards, so the linear scan is cheap.
		cards, err := svc.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			logger.Warn("reconcile sweep: list cards failed",
				"project", p.Name, "error", err)

			continue
		}

		for _, card := range cards {
			scanned++

			if !shouldEndSession(card) {
				continue
			}

			killed++

			endSessionAndKill(ctx, client, logger, p.Name, card.ID, card.State, card.RunnerStatus, sourceSweep)
		}
	}

	if killed > 0 {
		logger.Info("reconcile sweep completed",
			"scanned", scanned, "killed", killed, "projects", len(projects))
	}
}
