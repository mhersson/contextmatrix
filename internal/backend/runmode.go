package backend

import (
	"context"
	"log/slog"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
)

// CardRunCloser ends the recorded run of a card that started at or before
// startedNotAfter.
type CardRunCloser interface {
	CloseCardRun(ctx context.Context, project, cardID string, startedNotAfter, at time.Time) error
}

// endsRun reports whether the event marks a card's worker as finished.
func endsRun(e events.Event) bool {
	switch e.Type {
	case events.WorkerFailed, events.WorkerKilled, events.WorkerParked:
		return true
	case events.CardUpdated:
		status, _ := e.Data["worker_status"].(string)

		return status == "completed"
	}

	return false
}

// StartCardRunCloser closes a card's recorded run when its worker fails, is
// killed, parks or completes. Closing is best-effort: a failure is logged and
// the run stays open until the card is rerun. The goroutine runs until ctx is
// canceled.
func StartCardRunCloser(ctx context.Context, bus *events.Bus, store CardRunCloser, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	if bus == nil || store == nil {
		logger.Warn("card run closer not started: missing dependency",
			"bus_nil", bus == nil, "store_nil", store == nil)

		return
	}

	ch, unsubscribe := bus.Subscribe()

	go func() {
		defer unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return

			case e, ok := <-ch:
				if !ok {
					return
				}

				if !endsRun(e) {
					continue
				}

				at := e.Timestamp
				if at.IsZero() {
					at = time.Now()
				}

				if err := store.CloseCardRun(ctx, e.Project, e.CardID, at, at); err != nil {
					logger.Warn("close card run failed",
						"project", e.Project, "card_id", e.CardID, "error", err)
				}
			}
		}
	}()
}
