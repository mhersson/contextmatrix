package jirawriteback

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/jira"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	workerPoll = 30 * time.Second
	batchSize  = 20
	// maxAttempts is about 24 h of backoff: 1, 2, 4, 8, 16, 32 min, then hourly.
	maxAttempts   = 29
	baseBackoff   = time.Minute
	maxBackoff    = time.Hour
	maxProbeDelay = 15 * time.Minute
	maxReason     = 500
	// systemAgent attributes the card log lines this package writes.
	systemAgent = "system:jira-writeback"
)

func (w *Writeback) runWorker(ctx context.Context) {
	ticker := w.clk.NewTicker(workerPoll)
	defer ticker.Stop()

	for {
		w.drain(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
		case <-w.wake:
		}
	}
}

// drain processes one batch of due rows. A full batch that made progress
// nudges the worker to continue at once.
func (w *Writeback) drain(ctx context.Context) {
	if w.paused.Load() && !w.probe(ctx) {
		return
	}

	rows, err := w.outbox.DueJiraWritebacks(ctx, w.clk.Now().Unix(), batchSize)
	if err != nil {
		w.logger.Warn("jira writeback: list due rows", "error", err)

		return
	}

	progressed := 0

	for _, row := range rows {
		if ctx.Err() != nil || w.paused.Load() {
			return
		}

		if w.process(ctx, row) {
			progressed++
		}
	}

	if len(rows) == batchSize && progressed > 0 {
		w.nudge()
	}
}

// process attempts one row and records the outcome. It reports whether the
// row moved on (finished or rescheduled).
func (w *Writeback) process(ctx context.Context, row opsqlite.JiraWriteback) bool {
	if row.Kind == KindStarted {
		pendingDone, err := w.outbox.HasPendingJiraWriteback(ctx, row.Project, row.CardID, KindDone)
		if err != nil {
			w.logger.Warn("jira writeback: check pending done", "project", row.Project, "card_id", row.CardID, "error", err)

			return false
		}

		if pendingDone {
			return w.finish(ctx, row, opsqlite.JiraWritebackSkipped, "superseded by done")
		}
	}

	out, err := w.resolver.apply(ctx, row.IssueKey, row.Kind)

	switch {
	case err == nil && out == outcomeSkipped:
		w.writeSucceeded()

		return w.finish(ctx, row, opsqlite.JiraWritebackSkipped, "")
	case err == nil:
		w.writeSucceeded()

		return w.finish(ctx, row, opsqlite.JiraWritebackDone, "")
	case ctx.Err() != nil:
		// Shutdown cut the call: the row stays pending at no attempt cost, and
		// the status guards make a POST that did land a no-op next time.
		return false
	case errors.Is(err, jira.ErrUnauthorized):
		// Working credentials mean this write is what Jira refuses (a token
		// without write scope): retrying would loop forever.
		if _, perr := w.api.Myself(ctx); perr == nil {
			return w.fail(ctx, row, fmt.Sprintf("insufficient scope: Jira refused the write although the credentials work: %v", err))
		}

		if ctx.Err() != nil {
			return false
		}

		w.pause(err)

		return false
	case retryable(err) && row.Attempts+1 < maxAttempts:
		return w.retry(ctx, row, err)
	case retryable(err):
		return w.fail(ctx, row, fmt.Sprintf("gave up after %d attempts: %v", row.Attempts+1, err))
	default:
		return w.fail(ctx, row, err.Error())
	}
}

// retryable separates Jira or network trouble, which backs off, from answers
// that will not change on retry (404, 4xx, no transition path).
func retryable(err error) bool {
	if errors.Is(err, jira.ErrRateLimited) {
		return true
	}

	if se, ok := errors.AsType[*jira.StatusError](err); ok {
		return se.StatusCode >= 500
	}

	if errors.Is(err, jira.ErrNotFound) || errors.Is(err, jira.ErrInvalidRef) || errors.Is(err, errNoTransition) {
		return false
	}

	return true
}

func retryDelay(attempt int, err error) time.Duration {
	d := min(baseBackoff<<min(attempt-1, 6), maxBackoff)
	d = time.Duration(float64(d) * (0.8 + 0.4*rand.Float64())) //nolint:gosec // non-security jitter

	if rl, ok := errors.AsType[*jira.RateLimitError](err); ok && rl.RetryAfter > d {
		d = rl.RetryAfter
	}

	return d
}

func (w *Writeback) retry(ctx context.Context, row opsqlite.JiraWriteback, cause error) bool {
	now := w.clk.Now()
	attempts := row.Attempts + 1
	next := now.Add(retryDelay(attempts, cause))

	if err := w.outbox.RetryJiraWriteback(ctx, row.Project, row.CardID, row.Kind, attempts, next.Unix(), truncate(cause.Error()), now.Unix()); err != nil {
		w.logger.Warn("jira writeback: record retry", "project", row.Project, "card_id", row.CardID, "error", err)

		return false
	}

	w.logger.Info("jira writeback: will retry", "project", row.Project, "card_id", row.CardID, "issue", row.IssueKey,
		"kind", row.Kind, "attempt", attempts, "next", next, "error", cause)

	return true
}

func (w *Writeback) finish(ctx context.Context, row opsqlite.JiraWriteback, state, note string) bool {
	if err := w.outbox.FinishJiraWriteback(ctx, row.Project, row.CardID, row.Kind, state, note, w.clk.Now().Unix()); err != nil {
		w.logger.Warn("jira writeback: record outcome", "project", row.Project, "card_id", row.CardID, "error", err)

		return false
	}

	w.logger.Info("jira writeback", "project", row.Project, "card_id", row.CardID, "issue", row.IssueKey, "kind", row.Kind, "state", state)

	return true
}

// fail leaves one line on the card, then records the permanent failure. A
// line that cannot be written keeps the row pending so a later drain retries
// it; a card or project that is gone cannot take one, so the row finishes.
func (w *Writeback) fail(ctx context.Context, row opsqlite.JiraWriteback, reason string) bool {
	reason = truncate(reason)

	// WithoutCancel: the line goes through the commit queue, which outlives
	// this worker during shutdown.
	if _, err := w.cards.AddSystemLogEntry(context.WithoutCancel(ctx), row.Project, row.CardID, board.ActivityEntry{
		Agent:   systemAgent,
		Action:  "jira-writeback",
		Message: fmt.Sprintf("Jira writeback (%s) failed: %s", row.Kind, reason),
	}); err != nil {
		if !errors.Is(err, storage.ErrCardNotFound) && !errors.Is(err, storage.ErrProjectNotFound) {
			w.logger.Warn("jira writeback: card log line", "project", row.Project, "card_id", row.CardID, "error", err)

			return false
		}
	}

	if !w.finish(ctx, row, opsqlite.JiraWritebackFailed, reason) {
		return false
	}

	w.logger.Warn("jira writeback failed", "project", row.Project, "card_id", row.CardID, "issue", row.IssueKey,
		"kind", row.Kind, "reason", reason)

	return true
}

// writeSucceeded re-arms the pause warning and the probe backoff: the
// credentials demonstrably work for writes.
func (w *Writeback) writeSucceeded() {
	w.pauseWarned = false
	w.probeCount = 0
}

// pause stops draining until Myself succeeds again. Rows keep their place
// and attempt count, so an expired token loses nothing.
func (w *Writeback) pause(err error) {
	if w.paused.CompareAndSwap(false, true) {
		if w.pauseWarned {
			// Resumed but no write succeeded since: keep backing off.
			w.probeCount++
		} else {
			w.pauseWarned = true
			w.logger.Warn("jira writeback paused: Jira rejected the credentials", "error", err)
		}
	}

	w.probeAt = w.clk.Now().Add(probeDelay(w.probeCount))
}

// probe re-checks the credentials once their backoff has elapsed and
// reports whether draining may resume.
func (w *Writeback) probe(ctx context.Context) bool {
	if w.clk.Now().Before(w.probeAt) {
		return false
	}

	if _, err := w.api.Myself(ctx); err != nil {
		w.probeCount++
		w.probeAt = w.clk.Now().Add(probeDelay(w.probeCount))

		return false
	}

	w.paused.Store(false)
	w.logger.Info("jira writeback resumed")

	return true
}

func probeDelay(n int) time.Duration {
	return min(time.Minute<<min(n, 4), maxProbeDelay)
}

func truncate(s string) string {
	if len(s) <= maxReason {
		return s
	}

	return strings.ToValidUTF8(s[:maxReason], "")
}
