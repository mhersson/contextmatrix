package playbookrun

import (
	"context"
	"errors"
	"fmt"
	"slices"

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

	r.setWatched(id, d)

	if !d.Runnable || !d.Run.Active() || d.Run.Instance != r.cfg.Instance {
		return true
	}

	if r.holdOnLiveWorker(ctx, d) {
		return false
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
			// The reason is committed to the boards repo and rendered in the
			// UI, so it stays a fixed phrase; the raw error is for operators.
			ctxlog.Logger(ctx).Error("playbook run: card read failed",
				"playbook", d.ID, "project", e.Project, "card", e.Card, "error", err)
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("read %s/%s failed", e.Project, e.Card))

			return false
		}

		// Re-checked on the fresh card: the detail marked this entry
		// incomplete, but the card may have finished between the two reads.
		if board.IsTerminalState(card.State) {
			continue
		}

		if r.workerLive(card) {
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

		// An unvetted import holds the run like a manual gate. The entry
		// stays unset so the pass after a human vets the card launches it.
		if card.NeedsVetting() {
			r.setRun(ctx, d, board.RunStatusWaiting, "", card.ID+" not vetted")

			return false
		}

		r.launchEntry(ctx, d, e, card)

		return false
	}

	return r.setRun(ctx, d, board.RunStatusCompleted, "", "")
}

// holdOnLiveWorker keeps the run on a live worker and reports whether it
// did: run.entry's when its card has one, else the first in stored order. The
// run is re-pointed only when its entry is unset, complete or gone; an entry
// it already triggered keeps its markers so the walk waits on it later.
func (r *Runner) holdOnLiveWorker(ctx context.Context, d *service.PlaybookDetail) bool {
	entry := r.liveEntry(ctx, d.ID, rawEntries(d), d.Run.Entry)
	if entry == "" {
		return false
	}

	if cur := findEntry(d, d.Run.Entry); entry != d.Run.Entry && cur != nil && !cur.Complete {
		return true
	}

	r.setRun(ctx, d, board.RunStatusRunning, entry, "")

	return true
}

// liveEntry is the card entry whose card is not terminal and has a live
// worker: prefer when it qualifies, else the first in order; "" when none.
// A terminal card's worker is finishing and is let go; a failed read is idle.
func (r *Runner) liveEntry(ctx context.Context, playbookID string, entries []board.PlaybookEntry, prefer string) string {
	live := func(e *board.PlaybookEntry) bool {
		if e.Type != board.EntryTypeCard {
			return false
		}

		card, err := r.cfg.Cards.GetCard(ctx, e.Project, e.Card)
		if err != nil {
			if !errors.Is(err, storage.ErrCardNotFound) && !errors.Is(err, storage.ErrProjectNotFound) {
				ctxlog.Logger(ctx).Warn("playbook run: card read failed, treated as idle",
					"playbook", playbookID, "project", e.Project, "card", e.Card, "error", err)
			}

			return false
		}

		return !board.IsTerminalState(card.State) && r.workerLive(card)
	}

	if prefer != "" {
		for i := range entries {
			if entries[i].ID == prefer && live(&entries[i]) {
				return prefer
			}
		}
	}

	for i := range entries {
		if entries[i].ID != prefer && live(&entries[i]) {
			return entries[i].ID
		}
	}

	return ""
}

// rawEntries returns d's stored entries.
func rawEntries(d *service.PlaybookDetail) []board.PlaybookEntry {
	out := make([]board.PlaybookEntry, len(d.Entries))
	for i := range d.Entries {
		out[i] = d.Entries[i].PlaybookEntry
	}

	return out
}

// workerLive reports whether card has a queued or running worker, or a claim
// granted by another instance.
func (r *Runner) workerLive(card *board.Card) bool {
	return card.WorkerStatus == "queued" || card.WorkerStatus == "running" || r.cfg.Cards.ClaimedElsewhere(card)
}

// launchEntry brings the card back to todo when needed, re-asserts the
// forced settings, persists the entry as running, then triggers. Persisting
// before the trigger means a crash in between leaves a todo card with no
// worker on the recorded entry, which the next pass launches again.
func (r *Runner) launchEntry(ctx context.Context, d *service.PlaybookDetail, e *service.PlaybookEntryDetail, card *board.Card) {
	if card.State != board.StateTodo {
		if _, err := r.cfg.Cards.TransitionTo(ctx, e.Project, e.Card, board.StateTodo); err != nil {
			ctxlog.Logger(ctx).Error("playbook run: transition to todo failed",
				"playbook", d.ID, "project", e.Project, "card", card.ID, "error", err)
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, "move "+card.ID+" to todo")

			return
		}
	}

	if !card.HasPlaybookSettings(d.Branch) {
		if _, err := r.cfg.Cards.ForcePlaybookSettings(ctx, e.Project, e.Card, d.ID, d.Branch, RunnerAgent); err != nil {
			ctxlog.Logger(ctx).Error("playbook run: forcing playbook settings failed",
				"playbook", d.ID, "project", e.Project, "card", card.ID, "error", err)
			r.setRun(ctx, d, board.RunStatusWaiting, e.ID, fmt.Sprintf("force settings on %s failed", card.ID))

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

	// A Stop that cancelled this walker after the running write gets no
	// trigger. Once sent, the trigger runs to its outcome despite a later
	// cancel: an aborted webhook may still start a worker the reverted card
	// hides, while a finished one leaves a queued card Stop can kill.
	if ctx.Err() != nil {
		return
	}

	opts := LaunchOptions{CreateBaseBranch: true, BaseBranchFrom: d.BaseBranch}
	if err := launch(context.WithoutCancel(ctx), e.Project, e.Card, opts); err != nil {
		r.setRun(ctx, d, board.RunStatusWaiting, e.ID, err.Error())
	}
}

// setRun persists a status change and mirrors it into d.Run. A write that
// would change nothing is skipped, which keeps passes idempotent. The write
// is a compare-and-swap against the snapshot the pass decided from, so a
// Stop or a Play that landed mid-pass is never overwritten. It reports
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

	if _, err := r.cfg.Playbooks.SetRunIf(ctx, d.ID, unchangedSince(d.Run, d.ID), &run, RunnerAgent); err != nil {
		if errors.Is(err, ErrRunChanged) {
			ctxlog.Logger(ctx).Info("playbook run: changed underneath, pass abandoned",
				"playbook", d.ID, "status", status, "entry", entry)

			return false
		}

		ctxlog.Logger(ctx).Error("playbook run: persist failed", "playbook", d.ID, "status", status, "entry", entry, "error", err)

		return false
	}

	// The mirror carries the new UpdatedAt, so a later write in the same
	// pass compares against what this one stored rather than the stale read.
	d.Run = &run

	return true
}

// unchangedSince builds the compare-and-swap guard for a write decided from
// snapshot: the stored block must still be active and identical in every
// field a Play or a Stop would move.
func unchangedSince(snapshot *board.PlaybookRun, id string) func(*board.PlaybookRun) error {
	want := *snapshot

	return func(cur *board.PlaybookRun) error {
		if cur == nil || !cur.Active() || cur.Instance != want.Instance || cur.Status != want.Status ||
			cur.Entry != want.Entry || cur.Reason != want.Reason || !cur.UpdatedAt.Equal(want.UpdatedAt) {
			return fmt.Errorf("%w: %s", ErrRunChanged, id)
		}

		return nil
	}
}

// humanReason names why the current card needs a human: the parked reason
// from the activity log when there is one, else the worker or card state.
func humanReason(card *board.Card) string {
	switch {
	case card.WorkerStatus == "parked":
		for _, v := range slices.Backward(card.ActivityLog) {
			if v.Action == "parked" && v.Message != "" {
				return card.ID + " parked: " + v.Message
			}
		}

		return card.ID + " parked"
	case card.State == board.StateStalled:
		return card.ID + " stalled"
	default:
		return card.ID + " worker " + card.WorkerStatus
	}
}
