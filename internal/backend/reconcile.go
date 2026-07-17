package backend

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ContainerMaxAge caps how long a worker container is allowed to live before
// the reconcile sweep kills it regardless of card state. The backend has its
// own container_timeout (default 2h) as a last-resort safety net; this
// slightly-longer cap is the belt-and-suspenders layer on the CM side for the
// pathological case where neither the backend's timeout nor CM's own card
// bookkeeping catches a runaway container.
//
// Declared as a package var so tests can shrink it without a multi-hour
// wait. The value is read exactly once per StartReconciliationSweep call
// (in the caller's goroutine) and captured into the spawned goroutine as a
// local, so a ticker-driven read never races a subsequent test's write.
var ContainerMaxAge = 150 * time.Minute

// ContainerLister is the subset of *Client needed to ask the backend for
// ground-truth container state. The reconcile sweep consults this instead of
// CM's own worker_status field so the decision to kill never depends on a
// piece of CM bookkeeping that could drift away from Docker reality. See
// docs/remote-execution.md for why we moved authority to the backend.
type ContainerLister interface {
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
}

// ReconcileClient is the union of interfaces the sweep needs: it both lists
// containers (authoritative answer to "what's running") and kills them
// (via EndSession + Kill). In production both methods come from the same
// *Client, but keeping them as separate interfaces lets the sweep test inject
// a fake without standing up an HTTP server.
type ReconcileClient interface {
	ContainerLister
	EndSessionClient
}

// CardLookup is the per-card read path used by the sweep. A missing card is a
// positive signal (the card was deleted and the container should die with it),
// not an error to swallow - so GetCard returns (nil, nil) for "not found" or
// a real error that aborts just this container's decision, not the whole
// sweep.
type CardLookup interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
}

// StartReconciliationSweep launches a ticker goroutine that periodically asks
// the backend for every worker container and decides, per container, whether
// it should still be running. A card container is killed if:
//
//  1. CM has no card matching (project, card_id) - deleted or renamed out
//     from under the container.
//  2. The card's state is terminal (done / not_planned) - the work is over.
//  3. The container is older than ContainerMaxAge - runaway cap.
//
// Notably: the sweep does NOT consult the card's worker_status field. That
// field is a CM-side bookkeeping convenience that has repeatedly drifted
// away from Docker reality (the backend's completion/failure callbacks can
// flip it before the Docker cleanup actually succeeds); any
// path that gates on worker_status inherits every past and future drift bug.
// Docker is the single authority on "is this container running"; CM is the
// single authority on "should it be". Those two facts, nothing else.
//
// Blocks only until the goroutine is scheduled; returns immediately. An
// interval of 0 disables the sweep entirely.
func StartReconciliationSweep(ctx context.Context, svc CardLookup, client ReconcileClient, interval time.Duration, logger *slog.Logger) {
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

	// Capture ContainerMaxAge once in the caller's goroutine. If a test
	// mutates the package var after the sweep goroutine has started, that
	// write races the sweep's per-tick read - using a captured local
	// eliminates the race and also gives every running sweep a stable
	// cap for its lifetime.
	maxAge := ContainerMaxAge

	go func() {
		// Run an initial sweep immediately so containers orphaned by a CM
		// restart (events published while CM was down are never delivered)
		// are cleaned up without having to wait a full interval.
		runReconcileSweep(ctx, svc, client, maxAge, logger)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				runReconcileSweep(ctx, svc, client, maxAge, logger)
			}
		}
	}()
}

// runReconcileSweep asks the backend for its current container list once,
// then runs the card-kill loop against it. Safe to call ad-hoc from tests.
//
// Every tick logs scanned/killed - including 0/0 - so "is the sweep actually
// running?" is answerable from a single grep.
func runReconcileSweep(ctx context.Context, svc CardLookup, client ReconcileClient, maxAge time.Duration, logger *slog.Logger) {
	containers, err := client.ListContainers(ctx)
	if err != nil {
		logger.Warn("reconcile sweep: backend list failed (skipping tick)", "error", err)

		return
	}

	var killed int

	for _, c := range containers {
		reason, shouldKill := decideKill(ctx, svc, c, maxAge, logger)
		if !shouldKill {
			continue
		}

		killed++

		logger.Info("reconcile sweep killing container",
			"source", sourceSweep,
			"project", c.Project, "card_id", c.CardID,
			"container_id", truncate(c.ContainerID),
			"container_state", c.State,
			"reason", reason,
		)

		endSessionAndKill(ctx, client, logger, c.Project, c.CardID, "", "", sourceSweep)
	}

	logger.Info("reconcile sweep tick",
		"scanned", len(containers),
		"killed", killed,
	)
}

// decideKill runs the three-rule authoritative check against a single
// container. Returns a short reason string for the log line plus whether the
// container should be killed. A failed GetCard (for any reason other than
// "card not found") is logged and the container is left alone for this tick -
// a transient store error must not trigger a kill.
//
// maxAge is the ContainerMaxAge value captured at StartReconciliationSweep
// time, passed through explicitly instead of re-read from the package var so
// a concurrent test mutation cannot race the sweep's per-tick check.
func decideKill(ctx context.Context, svc CardLookup, c ContainerInfo, maxAge time.Duration, logger *slog.Logger) (string, bool) {
	// Only card containers are the sweep's business. Routing a container
	// without a CardID through decideKill would fire /end-session with an
	// empty CardID, which the backend rejects with HTTP 400.
	if c.CardID == "" {
		return "", false
	}

	// Age cap: applies even when card lookup fails, so a card-store outage
	// cannot extend a leaked container's lifetime indefinitely.
	if !c.StartedAt.IsZero() && time.Since(c.StartedAt) > maxAge {
		return "age_cap", true
	}

	card, err := svc.GetCard(ctx, c.Project, c.CardID)
	if err != nil {
		// Treat "not found" (positive signal, kill) distinctly from
		// transient store errors (skip this tick, retry next).
		if isCardNotFound(err) {
			return "no_such_card", true
		}

		logger.Warn("reconcile sweep: card lookup failed (skipping this container)",
			"project", c.Project, "card_id", c.CardID, "error", err)

		return "", false
	}

	if card == nil {
		return "no_such_card", true
	}

	if _, ok := terminalStates[card.State]; ok {
		return "terminal_state", true
	}

	return "", false
}

// isCardNotFound classifies a CardLookup error as a positive "this card does
// not exist" signal (which should trigger a kill) vs an arbitrary store
// failure (which should not). A real missing card must return
// storage.ErrCardNotFound; wrapped forms (fmt.Errorf("...: %w", err)) are
// also matched by errors.Is.
//
// Kept conservative: if a new error shape appears, we default to "not a
// not-found" so we never kill a container because a backing store was
// briefly unreachable.
func isCardNotFound(err error) bool {
	return errors.Is(err, storage.ErrCardNotFound)
}

// truncate shortens a Docker container ID for log lines without losing enough
// signal to identify it uniquely.
func truncate(id string) string {
	const shortLen = 12
	if len(id) > shortLen {
		return id[:shortLen]
	}

	return id
}
