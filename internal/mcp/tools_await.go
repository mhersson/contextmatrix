package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// defaultAwaitMax bounds a single blocking wait when the server was wired
	// without an explicit cap. Mirrors the config default.
	defaultAwaitMax = 8 * time.Minute

	// awaitRecheckInterval is the safety net for events the bus dropped: it
	// publishes without blocking, so a burst larger than a subscriber's
	// 64-event buffer is discarded rather than queued. Re-listing on this
	// cadence bounds how long a lost transition can hide, and doubles as the
	// beat for progress keep-alives.
	awaitRecheckInterval = 30 * time.Second

	// awaitHeartbeatInterval is how often the wait refreshes the caller's own
	// claim. Well inside the 30m default heartbeat timeout, so a caller blocked
	// across several full windows still never stalls.
	awaitHeartbeatInterval = 4 * time.Minute
)

type awaitSubtasksInput struct {
	Project        string `json:"project,omitempty" jsonschema:"project name"`
	ParentID       string `json:"parent_id" jsonschema:"required,parent card ID"`
	AgentID        string `json:"agent_id,omitempty" jsonschema:"caller identity; when it holds a claim on the parent, the wait refreshes that claim's heartbeat"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"max seconds to block (server-capped by await_max, default = the cap); on timeout re-call to keep waiting"`
}

type awaitSubtasksOutput struct {
	ParentID      string         `json:"parent_id"`
	Completed     bool           `json:"completed"`
	TimedOut      bool           `json:"timed_out,omitempty"`
	Counts        map[string]int `json:"counts"`
	Stalled       []string       `json:"stalled,omitempty"`
	WaitedSeconds int            `json:"waited_seconds"`
}

func registerAwaitSubtasks(server *mcp.Server, svc *service.CardService, bus *events.Bus, awaitMax time.Duration) {
	if awaitMax <= 0 {
		awaitMax = defaultAwaitMax
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "await_subtasks",
		Description: "Block until all subtasks of parent_id reach a terminal state, any subtask stalls, " +
			"or the timeout passes. Returns state counts either way. Cheaper than polling: one call " +
			"replaces a sleep-and-check loop, and it refreshes your claim's heartbeat while you wait. " +
			"On timed_out=true simply call it again.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input awaitSubtasksInput) (*mcp.CallToolResult, awaitSubtasksOutput, error) {
		out, err := awaitSubtasks(ctx, req, svc, bus, awaitMax, input)

		return nil, out, err
	})
}

// awaitSubtasks blocks until the parent's subtasks are all terminal, one of them
// stalls, the caller's deadline passes, or the caller goes away.
func awaitSubtasks(
	ctx context.Context,
	req *mcp.CallToolRequest,
	svc *service.CardService,
	bus *events.Bus,
	awaitMax time.Duration,
	input awaitSubtasksInput,
) (awaitSubtasksOutput, error) {
	parentID := strings.ToUpper(strings.TrimSpace(input.ParentID))

	project, err := resolveProject(ctx, svc, input.Project, parentID)
	if err != nil {
		return awaitSubtasksOutput{}, err
	}

	// Confirm the parent exists before waiting on it. A typo'd ID lists zero
	// subtasks, which is indistinguishable from "every subtask finished" - the
	// wait would return completed=true and the caller would move on.
	if _, err := svc.GetCard(ctx, project, parentID); err != nil {
		return awaitSubtasksOutput{}, fmt.Errorf("await_subtasks: load parent %s: %w", parentID, err)
	}

	// The service's clock is the same one stall detection reads, so the
	// heartbeat refreshes below cannot drift away from the timeout they exist
	// to prevent.
	clk := svc.Clock()
	start := clk.Now()

	// Subscribe before the first check so no transition can slip through the
	// gap between checking and waiting.
	evCh, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	recheck := clk.NewTicker(awaitRecheckInterval)
	defer recheck.Stop()

	heartbeat := clk.NewTicker(awaitHeartbeatInterval)
	defer heartbeat.Stop()

	waiter := &subtaskWaiter{
		svc:       svc,
		clk:       clk,
		project:   project,
		parentID:  parentID,
		agentID:   input.AgentID,
		start:     start,
		deadline:  start.Add(awaitWindow(input.TimeoutSeconds, awaitMax)),
		events:    evCh,
		recheck:   recheck,
		heartbeat: heartbeat,
		progress:  newAwaitProgress(req),
	}

	return waiter.run(ctx)
}

// awaitWindow is the effective blocking window: what the caller asked for,
// clamped to the server's cap. An omitted or non-positive request takes the
// full cap.
func awaitWindow(timeoutSeconds int, awaitMax time.Duration) time.Duration {
	if timeoutSeconds <= 0 {
		return awaitMax
	}

	if requested := time.Duration(timeoutSeconds) * time.Second; requested < awaitMax {
		return requested
	}

	return awaitMax
}

// subtaskWaiter holds the state of one blocking wait.
type subtaskWaiter struct {
	svc      *service.CardService
	clk      clock.Clock
	project  string
	parentID string
	agentID  string
	start    time.Time
	deadline time.Time

	events    <-chan events.Event
	recheck   clock.Ticker
	heartbeat clock.Ticker
	progress  *awaitProgress
}

// wakeReason is why the blocking select returned.
type wakeReason int

const (
	// wakeRecheck means something may have changed; re-read the board.
	wakeRecheck wakeReason = iota
	wakeDeadline
	wakeCancelled
)

// run is the wait loop. Every wakeup re-reads the board rather than trusting an
// event payload: the events say "something moved", the store says what the
// states actually are.
func (w *subtaskWaiter) run(ctx context.Context) (awaitSubtasksOutput, error) {
	for {
		out, err := w.snapshot(ctx)
		if err != nil {
			return awaitSubtasksOutput{}, err
		}

		out.WaitedSeconds = elapsedSeconds(w.start, w.clk.Now())

		// A stalled subtask ends the wait early: the caller can respawn it now
		// instead of sitting out the rest of the window.
		if out.Completed || len(out.Stalled) > 0 {
			return out, nil
		}

		remaining := w.deadline.Sub(w.clk.Now())
		if remaining <= 0 {
			out.TimedOut = true

			return out, nil
		}

		switch w.wait(ctx, w.clk.After(remaining)) {
		case wakeCancelled:
			// Caller gone or server shutting down: hand back the snapshot we
			// already hold rather than an error, so even an abandoned wait
			// reports the counts it observed.
			out.WaitedSeconds = elapsedSeconds(w.start, w.clk.Now())

			return out, nil

		case wakeDeadline:
			out.TimedOut = true
			out.WaitedSeconds = elapsedSeconds(w.start, w.clk.Now())

			return out, nil

		case wakeRecheck:
		}
	}
}

// wait blocks until something worth re-reading the board for happens. Heartbeat
// ticks are handled in place: refreshing the caller's claim is not a reason to
// re-list the subtasks.
func (w *subtaskWaiter) wait(ctx context.Context, timeout <-chan time.Time) wakeReason {
	for {
		select {
		case <-ctx.Done():
			return wakeCancelled

		case ev, ok := <-w.events:
			if !ok {
				// Bus closed underneath us. Drop the channel so this case stops
				// spinning; the recheck ticker carries the wait to its deadline.
				w.events = nil

				continue
			}

			if w.relevant(ev) {
				return wakeRecheck
			}

		case <-w.recheck.C():
			w.progress.notify(ctx, w.clk.Now().Sub(w.start))

			return wakeRecheck

		case <-w.heartbeat.C():
			w.refreshCallerClaim(ctx)

		case <-timeout:
			return wakeDeadline
		}
	}
}

// snapshot reads the parent's subtasks and derives the answer from their states.
// Completed is vacuously true for a parent with no subtasks - there is nothing
// to wait for, and blocking would only burn the window.
func (w *subtaskWaiter) snapshot(ctx context.Context) (awaitSubtasksOutput, error) {
	cards, err := w.svc.ListCards(ctx, w.project, storage.CardFilter{Parent: w.parentID})
	if err != nil {
		return awaitSubtasksOutput{}, fmt.Errorf("list subtasks: %w", err)
	}

	out := awaitSubtasksOutput{
		ParentID:  w.parentID,
		Completed: true,
		Counts:    make(map[string]int, len(cards)),
	}

	for _, card := range cards {
		out.Counts[card.State]++

		if card.State == board.StateStalled {
			out.Stalled = append(out.Stalled, card.ID)
		}

		if !board.IsTerminalState(card.State) {
			out.Completed = false
		}
	}

	slices.Sort(out.Stalled)

	return out, nil
}

// relevant reports whether an event can change the verdict for the project being
// awaited. Log, usage, and claim events churn the bus on every agent turn;
// re-listing the board for those would turn a cheap wait back into a busy poll.
func (w *subtaskWaiter) relevant(ev events.Event) bool {
	if ev.Project != w.project {
		return false
	}

	switch ev.Type {
	case events.CardStateChanged, events.CardStalled, events.CardCreated, events.CardDeleted:
		return true
	default:
		return false
	}
}

// refreshCallerClaim keeps the caller's own claim alive while it blocks here, so
// an orchestrator awaiting its subtasks never stalls its own parent card.
//
// An unclaimed parent, or one claimed by somebody else, is a normal way to call
// this tool - a human or a sibling agent may await subtasks it does not own - so
// neither outcome is an error and neither may end the wait.
func (w *subtaskWaiter) refreshCallerClaim(ctx context.Context) {
	if w.agentID == "" {
		return
	}

	if _, err := w.svc.HeartbeatCard(ctx, w.project, w.parentID, w.agentID); err != nil {
		if errors.Is(err, lock.ErrNotClaimed) || errors.Is(err, lock.ErrAgentMismatch) {
			return
		}

		ctxlog.Logger(ctx).Warn("await_subtasks could not refresh claim heartbeat",
			"project", w.project,
			"card_id", w.parentID,
			"agent_id", w.agentID,
			"error", err,
		)
	}
}

// elapsedSeconds is the time spent in the wait, rounded to whole seconds.
func elapsedSeconds(start, now time.Time) int {
	elapsed := now.Sub(start)
	if elapsed <= 0 {
		return 0
	}

	return int(elapsed.Round(time.Second) / time.Second)
}

// awaitProgress emits MCP progress notifications during a long wait. Proxies
// between the agent and the server may cut an idle connection well before the
// server's own deadline; a periodic notification keeps bytes flowing on the
// stream. It is best-effort in both directions: clients that sent no progress
// token get nothing (newAwaitProgress returns nil), and a failed send is logged
// rather than ending an otherwise healthy wait.
type awaitProgress struct {
	session *mcp.ServerSession
	token   any
	sent    float64
}

func newAwaitProgress(req *mcp.CallToolRequest) *awaitProgress {
	if req == nil || req.Session == nil || req.Params == nil {
		return nil
	}

	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}

	return &awaitProgress{session: req.Session, token: token}
}

func (p *awaitProgress) notify(ctx context.Context, elapsed time.Duration) {
	if p == nil {
		return
	}

	p.sent++

	err := p.session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: p.token,
		Progress:      p.sent,
		Message:       fmt.Sprintf("awaiting subtasks, %s elapsed", elapsed.Round(time.Second)),
	})
	if err != nil {
		ctxlog.Logger(ctx).Debug("await_subtasks progress notification failed", "error", err)
	}
}
