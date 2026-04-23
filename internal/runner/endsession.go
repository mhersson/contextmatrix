package runner

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
)

// EndSessionClient is the subset of *Client needed by the subscriber. Allows
// tests to inject a fake.
type EndSessionClient interface {
	EndSession(ctx context.Context, p EndSessionPayload) error
	Kill(ctx context.Context, p KillPayload) error
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

// sourceSubscriber / sourceSweep label which caller observed the card, so
// operators can tell from a single log line whether the fast path (event
// subscriber) or the backstop (reconcile sweep) cleaned up the container.
const (
	sourceSubscriber = "subscriber"
	sourceSweep      = "sweep"
)

func handleEndSessionEvent(ctx context.Context, svc CardGetter, client EndSessionClient, logger *slog.Logger, project, cardID string) {
	card, err := svc.GetCard(ctx, project, cardID)
	if err != nil {
		logger.Warn("end-session: could not load card",
			"source", sourceSubscriber,
			"project", project, "card_id", cardID, "error", err)

		return
	}

	if !shouldEndSession(card) {
		return
	}

	endSessionAndKill(ctx, client, logger, project, cardID, card.State, card.RunnerStatus, sourceSubscriber)
}

// endSessionAndKill runs the /end-session → /kill sequence against the runner
// for a card the caller has already verified via shouldEndSession. Both the
// event subscriber and the reconcile sweep call this so the runner sees a
// single consistent termination protocol regardless of which path observed
// the terminal state first.
//
// state / runnerStatus are carried through to the terminal log line so
// operators can see which card disposition triggered the kill without having
// to correlate with a separate GetCard.
//
// source labels the caller ("subscriber" or "sweep") in every log line so a
// leaked container can be traced back to the path that handled it.
func endSessionAndKill(ctx context.Context, client EndSessionClient, logger *slog.Logger, project, cardID, state, runnerStatus, source string) {
	// Phase 1: try graceful stdin close. This is a no-op for autonomous
	// (non-interactive) containers, where stdin was never attached — the
	// webhook returns 409 and we fall through to the kill step below. For
	// interactive containers where stdin is still open it signals EOF to
	// claude; claude may or may not exit on EOF (stream-json mode has been
	// observed keeping the process alive well past EOF), so we always follow
	// up with a force-kill below to guarantee the container is gone.
	endSessionErr := client.EndSession(ctx, EndSessionPayload{CardID: cardID, Project: project})
	if endSessionErr != nil && !isExpectedEndSessionErr(endSessionErr) {
		logger.Warn("end-session webhook failed",
			"source", source,
			"project", project, "card_id", cardID, "error", endSessionErr)
	}

	// Phase 2: force-kill the container so a terminal-state card never leaves
	// a live container behind, even if claude ignored the EOF. /kill is
	// idempotent — it returns 200 no-op when the container is already gone
	// (e.g. claude did exit on EOF, or reportCompleted already cleaned up).
	if err := client.Kill(ctx, KillPayload{CardID: cardID, Project: project}); err != nil {
		logger.Warn("kill webhook failed after end-session",
			"source", source,
			"project", project, "card_id", cardID, "error", err)

		return
	}

	logger.Info("end-session + kill sent",
		"source", source,
		"project", project, "card_id", cardID,
		"state", state, "runner_status", runnerStatus,
		"end_session_err", endSessionErr)
}

// statusCoder is implemented by runner webhook errors that carry an HTTP
// status code. Lets the subscriber classify expected-vs-warning responses
// without depending on the concrete error type.
type statusCoder interface {
	HTTPStatusCode() int
}

// isExpectedEndSessionErr returns true for runner responses that are normal
// in the terminal-state flow and don't warrant a warning:
//
//   - 404: no container tracked (already cleaned up — kill is a no-op)
//   - 409: no stdin attached (autonomous container — never had stdin)
//   - 410: stdin already closed (prior /promote or prior /end-session)
//
// All three are handled by the follow-up /kill step, which is idempotent.
func isExpectedEndSessionErr(err error) bool {
	var sc statusCoder
	if !errors.As(err, &sc) {
		return false
	}

	switch sc.HTTPStatusCode() {
	case 404, 409, 410:
		return true
	}

	return false
}

// shouldEndSession returns true when the card's shape indicates the
// associated runner container should be terminated: the card is in a
// terminal state (done / not_planned) AND the agent claim has been released.
//
// runner_status is deliberately NOT consulted. The older predicate gated on
// runner_status ∈ {queued, running}, which silently hid every container
// whose runner_status had drifted away from Docker reality (runner callbacks
// flip the field before Docker cleanup actually succeeds). The reconcile
// sweep now owns the ground-truth "is this container still running?"
// question by asking the runner directly; the subscriber is a fast-path
// accelerator that fires on release events and relies on the runner's
// idempotent /kill to turn spurious calls into 200 no-ops.
//
// See docs/remote-execution.md for the full rationale.
func shouldEndSession(card *board.Card) bool {
	if card == nil {
		return false
	}

	if card.AssignedAgent != "" {
		return false
	}

	if _, ok := terminalStates[card.State]; !ok {
		return false
	}

	return true
}
