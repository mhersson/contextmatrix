package sentinel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// passReads is what a pass read before acting on heals.
type passReads struct {
	set        state.Settings
	now        time.Time
	mode       llmchannel.Mode
	windows    []protocol.SubscriptionUsage
	charged    float64                 // the endpoint ledger since 00:00 UTC
	spent      float64                 // what the daily cap counts: charged, plus work in flight, kept current through the pass
	heals      []state.Heal            // the open heals
	inFlight   []state.Heal            // every heal with a run kind in flight, whatever its stage
	containers []backend.ContainerInfo // listed only while a dispatch is in flight
	listFailed bool                    // a failed listing counts every dispatch as listed
	health     protocol.HealthResponse // read only when a dispatch may go out; zero leaves no room
}

type cardRef struct{ project, id string }

// pass advances every open heal, then opens heals for runs that settled, and
// records the status. A switched-off Sentinel reads nothing beyond its
// settings. A pass that moved a heal on, opened one, or found one changed
// under it nudges once, so the next step is decided at once.
func (c *Controller) pass(ctx context.Context) error {
	set, _, _, err := c.cfg.Store.SentinelSettings(ctx)
	if err != nil {
		return fmt.Errorf("read sentinel settings: %w", err)
	}

	// Kept before anything else can fail, so a switch turned on reads as on.
	c.mu.Lock()
	c.settings = set
	c.mu.Unlock()

	now := c.cfg.Clock.Now()
	in := statusInput{Settings: set, Active: set.Enabled, LastPass: now, Now: now, Tick: c.cfg.Tick}

	if !set.Enabled {
		c.record(in)

		return nil
	}

	r, err := c.read(ctx, set, now)
	if err != nil {
		return err
	}

	c.expireDispatches(ctx, r)

	open, again := c.advanceHeals(ctx, r)

	if set.Healing {
		opened, err := c.considerRuns(ctx, now, open)
		if err != nil {
			return err
		}

		again = again || opened
	}

	healed, err := c.cfg.Store.HealedSince(ctx, dayStart(now))
	if err != nil {
		return fmt.Errorf("count healed cards: %w", err)
	}

	in.GateOpen, in.GateReason = r.gate()
	in.Mode, in.Windows, in.NoRunKinds = r.mode, r.windows, c.noRunKinds(now)
	in.Healing, in.HealedToday, in.SpentToday = len(open), healed, r.charged
	c.record(in)

	if again {
		c.Nudge()
	}

	return nil
}

// read reads the budget gate's inputs and the open heals, then lists the
// agent's containers when a dispatch is in flight and reads its health when
// one may go out.
func (c *Controller) read(ctx context.Context, set state.Settings, now time.Time) (*passReads, error) {
	windows, err := c.cfg.Store.SubscriptionWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("read subscription windows: %w", err)
	}

	charged, err := c.cfg.Store.SpentSince(ctx, string(llmchannel.ModeEndpoint), dayStart(now))
	if err != nil {
		return nil, fmt.Errorf("read today's spend: %w", err)
	}

	heals, err := c.cfg.Store.OpenHeals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open heals: %w", err)
	}

	inFlight, err := c.cfg.Store.InFlightHeals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list in-flight heals: %w", err)
	}

	r := &passReads{
		set: set, now: now, mode: c.cfg.Channel.Effective(config.BackendNameAgent),
		windows: windows, charged: charged, spent: charged + inFlightReserve(inFlight), heals: heals, inFlight: inFlight,
	}
	log := ctxlog.Logger(ctx)

	if len(inFlight) > 0 {
		if r.containers, err = c.cfg.Backend.ListContainers(ctx); err != nil {
			log.Warn("sentinel: listing the agent's containers failed", "error", err)

			r.listFailed = true
		}
	}

	// The gate only shuts as the pass counts more spend, so it is never more
	// open than now.
	budget, _ := r.gate()
	if set.Healing && slices.ContainsFunc(heals, func(h state.Heal) bool { return mayDispatch(h, budget) }) {
		if r.health, err = c.cfg.Backend.Health(ctx); err != nil {
			log.Warn("sentinel: reading the agent's health failed", "error", err)
		}
	}

	return r, nil
}

// gate is the budget gate on what the pass has counted so far.
func (r *passReads) gate() (open bool, reason string) {
	return budgetGate(r.mode, r.set, r.spent, r.windows, r.now)
}

// listed reports whether the agent lists h's in-flight run kind.
func (r *passReads) listed(h state.Heal) bool {
	return r.listFailed || slices.ContainsFunc(r.containers, func(ci backend.ContainerInfo) bool {
		return ci.Project == h.Project && ci.CardID == h.CardID && ci.Kind == h.Dispatch.Kind
	})
}

// held lists the run kinds another heal of h's card still has in flight.
func (r *passReads) held(h state.Heal) []string {
	var kinds []string

	for _, o := range r.inFlight {
		if o.ID != h.ID && o.Project == h.Project && o.CardID == h.CardID {
			kinds = append(kinds, o.Dispatch.Kind)
		}
	}

	return kinds
}

// expireDispatches fails each finished heal's run kind that can no longer
// report and charges its ceiling, so the card's open heal may send that kind
// again. Open heals' run kinds are left to the heal flow.
func (c *Controller) expireDispatches(ctx context.Context, r *passReads) {
	kept := make([]state.Heal, 0, len(r.inFlight))

	for _, h := range r.inFlight {
		if !state.Terminal(h.Stage) || !unreported(h.Dispatch, r.listed(h), r.now) {
			kept = append(kept, h)

			continue
		}

		failed := h

		// ErrHealChanged: its report arrived since the pass read it.
		if err := c.failUnreported(ctx, &failed); err != nil && !errors.Is(err, state.ErrHealChanged) {
			ctxlog.Logger(ctx).Warn("sentinel: unreported run kind not expired",
				"project", h.Project, "card_id", h.CardID, "heal_id", h.ID, "error", err)

			kept = append(kept, h)
		}
	}

	r.inFlight = kept
}

// advanceHeals reads every open heal's view, so the daily cap counts every
// rerun's cost so far before any heal decides, then acts on each heal's next
// step. It returns the cards whose heal is still open, and whether one moved
// on or changed under the pass. A heal whose read failed stays open for the
// next pass to decide.
func (c *Controller) advanceHeals(ctx context.Context, r *passReads) (open map[cardRef]bool, again bool) {
	open = make(map[cardRef]bool, len(r.heals))
	views := make([]healView, 0, len(r.heals))

	for _, h := range r.heals {
		v, err := c.view(ctx, r, h)
		if err != nil {
			ctxlog.Logger(ctx).Warn("sentinel: heal not advanced",
				"project", h.Project, "card_id", h.CardID, "heal_id", h.ID, "error", err)

			open[cardRef{h.Project, h.CardID}] = true

			continue
		}

		r.spent += rerunAccrued(v)
		views = append(views, v)
	}

	for i := range views {
		stillOpen, moved := c.advance(ctx, r, &views[i])
		if stillOpen {
			open[cardRef{views[i].Heal.Project, views[i].Heal.CardID}] = true
		}

		again = again || moved
	}

	return open, again
}

// advance acts on one heal's next step, judged on the budget and worker slots
// as the heals before it left them. A heal whose step failed, or that changed
// since it was read, or whose card did, stays open for the next pass to
// decide.
func (c *Controller) advance(ctx context.Context, r *passReads, v *healView) (open, again bool) {
	h := v.Heal
	log := ctxlog.Logger(ctx).With("project", h.Project, "card_id", h.CardID, "heal_id", h.ID)

	v.Budget, _ = r.gate()
	v.Room = roomFor(r.health)

	s := nextStep(*v)

	err := c.act(ctx, r, v, s)

	switch {
	case errors.Is(err, state.ErrHealChanged), errors.Is(err, service.ErrCardChanged):
		return true, true
	case err != nil:
		log.Warn("sentinel: heal step failed", "stage", v.Heal.Stage, "error", err)

		return true, false
	}

	return !state.Terminal(v.Heal.Stage), s.Kind == stepAdvance
}

// view reads what nextStep needs for h but the budget and worker slots: the
// card fresh, its run row, the edit check, and at stage apply the fix checks
// on a card diagnosis's fix, which trimFix may apply without its section.
func (c *Controller) view(ctx context.Context, r *passReads, h state.Heal) (healView, error) {
	v := healView{Heal: h, Healing: r.set.Healing, Now: r.now}

	card, err := c.cfg.Cards.GetCard(ctx, h.Project, h.CardID)

	switch {
	case gone(err):
		return v, nil
	case err != nil:
		return v, fmt.Errorf("read card: %w", err)
	}

	run, err := c.cfg.Store.CardRunRow(ctx, h.Project, h.CardID)
	if err != nil {
		return v, fmt.Errorf("read run row: %w", err)
	}

	if run != nil {
		v.Run = *run
	}

	v.Card = card
	v.Listed = inFlight(h.Dispatch) && r.listed(h)
	v.Held = r.held(h)
	v.Edited = checkEdits(fixContext{Card: card, Snapshot: v.Run.Launch.Snapshot})

	if fix := healFix(h); h.Stage == state.StageApply && fix != nil {
		var vs []violation

		if v.Subtasks, vs, err = c.vet(ctx, h, card, v.Run, *fix); err != nil {
			return v, err
		}

		kept, dropped, ok := trimFix(*fix, vs)

		switch {
		case !ok:
			v.Rejected = texts(vs)
		case len(dropped) > 0:
			// The heal's next save, version-guarded like every other, stores it.
			v.Heal = withFix(v.Heal, kept, dropped)
		}
	}

	return v, nil
}

// vet runs the fix checks with the card's subtasks and its project's verify
// read fresh, and returns the subtasks for the fix to decide its rerun by.
func (c *Controller) vet(ctx context.Context, h state.Heal, card *board.Card, run state.Run, fix protocol.CardFix) ([]*board.Card, []violation, error) {
	project, err := c.cfg.Cards.GetProject(ctx, h.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("read project: %w", err)
	}

	subtasks, err := c.cfg.Cards.ListCards(ctx, h.Project, storage.CardFilter{Parent: card.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("list subtasks: %w", err)
	}

	return subtasks, vetFix(fixContext{
		Card: card, Snapshot: run.Launch.Snapshot, Subtasks: subtasks, SentSubtasks: h.SentSubtasks,
		Project: project.Verify, Cause: h.Cause, Fix: fix, Check: h.Check,
	}), nil
}

// act carries out s for the heal v holds.
func (c *Controller) act(ctx context.Context, r *passReads, v *healView, s step) error {
	h := &v.Heal

	switch s.Kind {
	case stepDispatch:
		var err error

		switch s.RunKind {
		case protocol.RunKindInvestigate:
			err = c.dispatchInvestigate(ctx, h, r.set.InvestigateCeilingUSD)
		case protocol.RunKindCheck:
			err = c.dispatchCheck(ctx, h)
		default:
			ctxlog.Logger(ctx).Warn("sentinel: no dispatch for this run kind",
				"project", h.Project, "card_id", h.CardID, "kind", s.RunKind)

			return nil
		}

		// The run may hold a worker slot now, and may charge its ceiling, which
		// the next heal's room and budget count.
		if inFlight(h.Dispatch) {
			r.health.RunningContainers++
			r.spent += inFlightReserve([]state.Heal{*h})
		}

		return err
	case stepAdvance:
		return c.saveStage(ctx, h, s.Stage, "")
	case stepPark:
		if s.ChargeCeiling {
			return c.parkUnreported(ctx, h, s.Reason)
		}

		return c.park(ctx, h, s.Reason)
	case stepClose:
		return c.closeHeal(ctx, h, s.Reason)
	case stepApply:
		return c.apply(ctx, h, v.Card, v.Run, v.Subtasks)
	case stepUsageRerun:
		return c.usageRerun(ctx, h, v.Card, v.Run)
	case stepSettle:
		return c.settle(ctx, v, s)
	}

	return nil
}

// gone reports whether a card read failed because the card or its project no
// longer exists.
func gone(err error) bool {
	return errors.Is(err, storage.ErrCardNotFound) || errors.Is(err, storage.ErrProjectNotFound)
}

// record keeps in as the last pass and publishes sentinel.changed when its
// status differs from the one last published.
func (c *Controller) record(in statusInput) {
	st := summarize(in)

	c.mu.Lock()
	c.last = in
	changed := !reflect.DeepEqual(st, c.published)
	c.published = st
	c.mu.Unlock()

	if changed {
		c.cfg.Bus.Publish(events.Event{Type: events.SentinelChanged, Timestamp: in.LastPass.UTC()})
	}
}
