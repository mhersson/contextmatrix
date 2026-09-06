package playbookrun

import (
	"context"
	"runtime/debug"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
)

// walker is one goroutine's handle: its cancel and a one-slot nudge that
// Ensure fills when a Play arrives while the walker may be about to exit.
type walker struct {
	cancel context.CancelFunc
	nudge  chan struct{}
}

// walk is one playbook's loop: a pass, then wait for a nudge. Nudges are a
// bus event for one of the playbook's cards or for the playbook itself, a
// Play, and the tick, which guarantees progress because the bus drops events
// on a full buffer and replays nothing across a restart.
func (r *Runner) walk(ctx context.Context, id string, w *walker) {
	defer r.wg.Done()
	defer r.forget(id, w)

	if ctx.Err() != nil {
		return
	}

	ch, unsubscribe := r.cfg.Bus.Subscribe()
	defer unsubscribe()

	ticker := r.cfg.Clock.NewTicker(r.cfg.Tick)
	defer ticker.Stop()

	for {
		if r.safePass(ctx, id) {
			// A Play that landed during the pass keeps this walker alive.
			if r.release(id, w) {
				continue
			}

			return
		}

		if !r.waitNudge(ctx, id, ch, ticker, w) {
			return
		}
	}
}

// release decides whether a walker whose pass reported done may exit. A
// nudge pending here means Play wrote a fresh run while the pass was reading
// the old one; the decision and the removal happen under the lock Ensure
// holds, so such a Play can never be left without a walker.
func (r *Runner) release(id string, w *walker) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-w.nudge:
		return true
	default:
	}

	r.dropLocked(id, w)

	return false
}

// forget drops the walker on any exit path release did not cover, which is
// cancellation. It is generation-safe: a walker started later for the same
// playbook keeps its entry.
func (r *Runner) forget(id string, w *walker) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dropLocked(id, w)
}

// dropLocked removes w's entry and cancels its context when w is still the
// current walker for id. Callers hold r.mu.
func (r *Runner) dropLocked(id string, w *walker) {
	if r.walkers[id] == w {
		delete(r.walkers, id)
		w.cancel()
	}
}

// safePass runs one pass with panic recovery so a bug in one run never
// takes the process down; a panic counts as "not done" and the tick retries.
func (r *Runner) safePass(ctx context.Context, id string) (done bool) {
	defer func() {
		if rec := recover(); rec != nil {
			ctxlog.Logger(ctx).Error("playbook run: pass panicked", "playbook", id, "panic", rec, "stack", string(debug.Stack()))

			done = false
		}
	}()

	return r.pass(ctx, id)
}

// waitNudge blocks until a Play, a relevant event, a tick, or cancellation.
// It returns false on cancellation.
func (r *Runner) waitNudge(ctx context.Context, id string, ch <-chan events.Event, ticker clockTicker, w *walker) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.nudge:
			return true
		case <-ticker.C():
			return true
		case ev, ok := <-ch:
			if !ok {
				return false
			}

			if r.relevant(ctx, id, ev) {
				return true
			}
		}
	}
}

// relevant reports whether ev concerns this playbook: a playbook event with
// its id, or a card event for one of its card entries. The entry set is read
// fresh so an entry added mid-run is watched too.
func (r *Runner) relevant(ctx context.Context, id string, ev events.Event) bool {
	if strings.HasPrefix(string(ev.Type), "playbook.") {
		evID, _ := ev.Data["id"].(string)

		return evID == id
	}

	if ev.CardID == "" {
		return false
	}

	d, err := r.cfg.Playbooks.Get(ctx, id)
	if err != nil {
		return true // let the pass decide; it handles a vanished playbook
	}

	for _, e := range d.Entries {
		if e.Type == board.EntryTypeCard && e.Project == ev.Project && e.Card == ev.CardID {
			return true
		}
	}

	return false
}
