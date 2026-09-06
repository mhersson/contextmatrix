package playbookrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// pass runs one reconcile of the playbook's run: find the frontier entry and
// act on it. It is idempotent, so restarts, resumes and duplicate events are
// harmless. It returns true when the walker should stop: the run is gone,
// not active, owned elsewhere, or just completed.
func (r *Runner) pass(ctx context.Context, id string) bool {
	d, err := r.cfg.Playbooks.Get(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrPlaybookNotFound) {
			return true
		}

		ctxlog.Logger(ctx).Warn("playbook run: read failed, will retry", "playbook", id, "error", err)

		return false
	}

	if !d.Runnable || !d.Run.Active() || d.Run.Instance != r.cfg.Instance {
		return true
	}

	for i := range d.Entries {
		e := &d.Entries[i]
		if e.Complete {
			continue
		}

		if e.Type == board.EntryTypeManual {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, "awaiting check-off")

			return false
		}

		if e.Missing {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("%s/%s no longer exists", e.Project, e.Card))

			return false
		}

		card, err := r.cfg.Cards.GetCard(ctx, e.Project, e.Card)
		if err != nil {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("read %s/%s: %v", e.Project, e.Card, err))

			return false
		}

		if board.IsTerminalState(card.State) {
			continue
		}

		if card.WorkerStatus == "queued" || card.WorkerStatus == "running" || r.cfg.Cards.ClaimedElsewhere(card) {
			r.setRun(ctx, d, board.RunStatusRunning, e.ID, "")

			return false
		}

		needsHuman := card.WorkerStatus == "failed" || card.WorkerStatus == "killed" ||
			card.WorkerStatus == "parked" || card.State == board.StateStalled

		// The entry this run already triggered failed: wait for a human.
		// Play clears the entry, which is what makes a resume launch again.
		if needsHuman && d.Run.Entry == e.ID {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, humanReason(card))

			return false
		}

		if card.State != board.StateTodo && !needsHuman {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("%s is in %s without a worker", card.ID, card.State))

			return false
		}

		// A run already waiting on this entry stays waiting: the launch failed,
		// or a human is fixing the card, and only Play, which clears the entry,
		// may trigger it again. A running run on a todo card with a matching
		// entry is the crash between persist and trigger and does launch.
		if d.Run.Entry == e.ID && d.Run.Status == board.RunStatusWaiting {
			return false
		}

		r.launchEntry(ctx, d, e, card)

		return false
	}

	return r.setRun(ctx, d, board.RunStatusCompleted, "", "")
}

// launchEntry brings the card back to todo when needed, re-asserts the
// forced settings, persists the entry as running, then triggers. Persisting
// before the trigger means a crash in between leaves a todo card with no
// worker on the recorded entry, which the next pass launches again.
func (r *Runner) launchEntry(ctx context.Context, d *service.PlaybookDetail, e *service.PlaybookEntryDetail, card *board.Card) {
	if card.State != board.StateTodo {
		if _, err := r.cfg.Cards.TransitionTo(ctx, e.Project, e.Card, board.StateTodo); err != nil {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("move %s to todo: %v", card.ID, err))

			return
		}
	}

	if !card.HasPlaybookSettings(d.Branch) {
		if _, err := r.cfg.Cards.ForcePlaybookSettings(ctx, e.Project, e.Card, d.ID, d.Branch, RunnerAgent); err != nil {
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("force settings on %s: %v", card.ID, err))

			return
		}
	}

	if !r.setRun(ctx, d, board.RunStatusRunning, e.ID, "") {
		return
	}

	launch := r.launcher()
	if launch == nil {
		r.setRun(ctx, d, board.RunStatusWaiting, e.ID, ErrNoBackend.Error())

		return
	}

	opts := LaunchOptions{CreateBaseBranch: true, BaseBranchFrom: d.BaseBranch}
	if err := launch(ctx, e.Project, e.Card, opts); err != nil {
		r.setRun(ctx, d, board.RunStatusWaiting, e.ID, err.Error())
	}
}

// setRun persists a status change and mirrors it into d.Run. A write that
// would change nothing is skipped, which keeps passes idempotent. It reports
// whether the run block now carries the requested state.
func (r *Runner) setRun(ctx context.Context, d *service.PlaybookDetail, status, entry, reason string) bool {
	if d.Run.Status == status && d.Run.Entry == entry && d.Run.Reason == reason {
		return true
	}

	now := r.cfg.Clock.Now().UTC()
	run := *d.Run
	run.Status = status
	run.Entry = entry
	run.Reason = reason
	run.UpdatedAt = now
	run.EndedAt = nil

	if status == board.RunStatusCompleted {
		run.EndedAt = &now
	}

	if _, err := r.cfg.Playbooks.SetRun(ctx, d.ID, &run, RunnerAgent); err != nil {
		ctxlog.Logger(ctx).Error("playbook run: persist failed", "playbook", d.ID, "status", status, "entry", entry, "error", err)

		return false
	}

	d.Run = &run

	return true
}

// humanReason names why the current card needs a human: the parked reason
// from the activity log when there is one, else the worker or card state.
func humanReason(card *board.Card) string {
	switch {
	case card.WorkerStatus == "parked":
		for i := len(card.ActivityLog) - 1; i >= 0; i-- {
			if card.ActivityLog[i].Action == "parked" && card.ActivityLog[i].Message != "" {
				return card.ID + " parked: " + card.ActivityLog[i].Message
			}
		}

		return card.ID + " parked"
	case card.State == board.StateStalled:
		return card.ID + " stalled"
	default:
		return card.ID + " worker " + card.WorkerStatus
	}
}
