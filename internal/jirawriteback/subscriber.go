package jirawriteback

import (
	"context"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
)

func kindFor(newState string) string {
	switch newState {
	case board.StateInProgress:
		return KindStarted
	case board.StateDone:
		return KindDone
	}

	return ""
}

func (w *Writeback) runSubscriber(ctx context.Context, ch <-chan events.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}

			w.handleEvent(ctx, e)
		}
	}
}

// handleEvent enqueues the write a state change owes Jira. Synced events
// count like local ones: any instance with writeback enabled writes, and the
// worker's status guards turn duplicate writes into no-ops.
func (w *Writeback) handleEvent(ctx context.Context, e events.Event) {
	if e.Type != events.CardStateChanged {
		return
	}

	newState, _ := e.Data["new_state"].(string)

	kind := kindFor(newState)
	if kind == "" {
		return
	}

	card, err := w.cards.GetCard(ctx, e.Project, e.CardID)
	if err != nil {
		w.logger.Warn("jira writeback: read card", "project", e.Project, "card_id", e.CardID, "error", err)

		return
	}

	if key := w.jiraKey(card); key != "" {
		w.enqueue(ctx, e.Project, card.ID, kind, key)
	}
}
