package runner

import (
	"context"
	"log/slog"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
)

// EndSessionClient is the subset of *Client needed by the subscriber. Allows
// tests to inject a fake.
type EndSessionClient interface {
	EndSession(ctx context.Context, p EndSessionPayload) error
}

// CardGetter is the subset of *service.CardService needed by the subscriber.
type CardGetter interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
}

// terminalStates is the hardcoded set of card states that signal the
// interactive session should end. Matches the built-in terminal semantics
// (board.StateDone is terminal in every shipped template;
// board.StateNotPlanned is always terminal per board validation).
//
// board.StateStalled is intentionally NOT terminal here: a stalled card
// means the agent's heartbeat timed out, which typically implies the
// container is already dead or about to be killed by the runner's container
// timeout — firing /end-session would be a best-effort no-op.
var terminalStates = map[string]struct{}{
	board.StateDone:       {},
	board.StateNotPlanned: {},
}

// activeRunnerStatuses are the runner_status values that indicate a container
// is currently running for this card (so closing its stdin is meaningful).
//
// Invariant relied on by this subscriber: the runner only transitions
// runner_status to "completed"/"failed"/"killed" *after* ContainerWait
// returns — i.e. the container has already exited. A card in one of those
// states therefore has no live container to end, so filtering on
// {queued, running} here is both necessary (to avoid spurious calls) and
// safe (we don't miss live containers).
var activeRunnerStatuses = map[string]struct{}{
	"queued":  {},
	"running": {},
}

// StartEndSessionSubscriber wires an event-bus subscriber that calls
// /end-session on the runner whenever a card reaches a terminal state and
// has been released. Blocks only until initial subscription is set up; the
// goroutine runs until ctx is canceled.
//
// Each candidate event is handled in a short-lived goroutine so a slow
// webhook call never blocks the bus pump (events.Bus drops events for slow
// subscribers — see internal/events/bus.go).
func StartEndSessionSubscriber(ctx context.Context, bus *events.Bus, svc CardGetter, client EndSessionClient, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	if bus == nil || svc == nil || client == nil {
		logger.Warn("end-session subscriber not started: missing dependency",
			"bus_nil", bus == nil, "svc_nil", svc == nil, "client_nil", client == nil)

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

				if e.Type != events.CardReleased && e.Type != events.CardStateChanged {
					continue
				}

				go handleEndSessionEvent(ctx, svc, client, logger, e.Project, e.CardID)
			}
		}
	}()
}

func handleEndSessionEvent(ctx context.Context, svc CardGetter, client EndSessionClient, logger *slog.Logger, project, cardID string) {
	card, err := svc.GetCard(ctx, project, cardID)
	if err != nil {
		logger.Warn("end-session: could not load card",
			"project", project, "card_id", cardID, "error", err)

		return
	}

	if !shouldEndSession(card) {
		return
	}

	if err := client.EndSession(ctx, EndSessionPayload{CardID: cardID, Project: project}); err != nil {
		logger.Warn("end-session webhook failed",
			"project", project, "card_id", cardID, "error", err)

		return
	}

	logger.Info("end-session webhook sent",
		"project", project, "card_id", cardID,
		"state", card.State, "runner_status", card.RunnerStatus)
}

func shouldEndSession(card *board.Card) bool {
	if card == nil {
		return false
	}

	if card.AssignedAgent != "" {
		return false
	}

	if _, ok := activeRunnerStatuses[card.RunnerStatus]; !ok {
		return false
	}

	if _, ok := terminalStates[card.State]; !ok {
		return false
	}

	return true
}
