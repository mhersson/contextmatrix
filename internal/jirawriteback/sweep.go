package jirawriteback

import (
	"context"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	sweepInterval = time.Hour
	sweepWindow   = 24 * time.Hour
)

func (w *Writeback) runSweep(ctx context.Context) {
	ticker := w.clk.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		w.sweep(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
		}
	}
}

// sweep enqueues writes the subscriber never saw: a full bus buffer, a large
// synced pull that emits no per-card events, a crash. Only cards updated in
// the last 24 h count, so turning writeback on never backfills old moves.
func (w *Writeback) sweep(ctx context.Context) {
	cutoff := w.clk.Now().Add(-sweepWindow)

	projects, err := w.cards.ListProjects(ctx)
	if err != nil {
		w.logger.Warn("jira writeback sweep: list projects", "error", err)

		return
	}

	for _, p := range projects {
		cards, err := w.cards.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			w.logger.Warn("jira writeback sweep: list cards", "project", p.Name, "error", err)

			continue
		}

		for _, c := range cards {
			key := w.jiraKey(c)
			if key == "" || c.Updated.Before(cutoff) {
				continue
			}

			for _, kind := range sweepKinds(c.State) {
				w.enqueue(ctx, p.Name, c.ID, kind, key)
			}
		}
	}
}

func sweepKinds(state string) []string {
	switch state {
	case board.StateDone:
		return []string{KindDone}
	case board.StateInProgress, board.StateReview, board.StateStalled:
		return []string{KindStarted}
	}

	// Custom states, including a project's initial one, are never inferred
	// as started: imports land in the first state.
	return nil
}
