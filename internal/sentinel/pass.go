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
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// passReads is what a pass read before acting on heals.
type passReads struct {
	set        Settings
	now        time.Time
	mode       llmchannel.Mode
	windows    []protocol.SubscriptionUsage
	spent      float64 // the endpoint ledger since 00:00 UTC
	budget     bool
	gateReason string
	heals      []Heal
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

	now := c.cfg.Clock.Now()
	in := statusInput{Settings: set, LastPass: now, Now: now, Tick: c.cfg.Tick}

	if !set.Enabled {
		c.record(in)

		return nil
	}

	r, err := c.read(ctx, set, now)
	if err != nil {
		return err
	}

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

	in.Mode, in.Windows, in.GateOpen, in.GateReason = r.mode, r.windows, r.budget, r.gateReason
	in.NoRunKinds = c.noRunKinds(now)
	in.Healing, in.HealedToday, in.SpentToday = len(open), healed, r.spent
	c.record(in)

	if again {
		c.Nudge()
	}

	return nil
}

// read reads the budget gate's inputs and the open heals, then lists the
// agent's containers when a dispatch is in flight and reads its health when
// one may go out.
func (c *Controller) read(ctx context.Context, set Settings, now time.Time) (*passReads, error) {
	windows, err := c.cfg.Store.SubscriptionWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("read subscription windows: %w", err)
	}

	spent, err := c.cfg.Store.SpentSince(ctx, string(llmchannel.ModeEndpoint), dayStart(now))
	if err != nil {
		return nil, fmt.Errorf("read today's spend: %w", err)
	}

	heals, err := c.cfg.Store.OpenHeals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open heals: %w", err)
	}

	r := &passReads{
		set: set, now: now, mode: c.cfg.Channel.Effective(config.BackendNameAgent),
		windows: windows, spent: spent, heals: heals,
	}
	r.budget, r.gateReason = budgetGate(r.mode, set, spent, windows, now)
	log := ctxlog.Logger(ctx)

	if slices.ContainsFunc(heals, func(h Heal) bool { return inFlight(h.Dispatch) }) {
		if r.containers, err = c.cfg.Backend.ListContainers(ctx); err != nil {
			log.Warn("sentinel: listing the agent's containers failed", "error", err)

			r.listFailed = true
		}
	}

	if set.Healing && slices.ContainsFunc(heals, func(h Heal) bool { return mayDispatch(h, r.budget) }) {
		if r.health, err = c.cfg.Backend.Health(ctx); err != nil {
			log.Warn("sentinel: reading the agent's health failed", "error", err)
		}
	}

	return r, nil
}

// listed reports whether the agent lists h's in-flight run kind.
func (r *passReads) listed(h Heal) bool {
	return r.listFailed || slices.ContainsFunc(r.containers, func(ci backend.ContainerInfo) bool {
		return ci.Project == h.Project && ci.CardID == h.CardID && ci.Kind == h.Dispatch.Kind
	})
}

// advanceHeals acts on each open heal's next step. It returns the cards whose
// heal is still open, and whether one moved on or changed under the pass.
func (c *Controller) advanceHeals(ctx context.Context, r *passReads) (open map[cardRef]bool, again bool) {
	open = make(map[cardRef]bool, len(r.heals))

	for _, h := range r.heals {
		stillOpen, moved := c.advance(ctx, r, h)
		if stillOpen {
			open[cardRef{h.Project, h.CardID}] = true
		}

		again = again || moved
	}

	return open, again
}

// advance acts on one heal's next step. A heal whose read or step failed, or
// that changed since it was read, or whose card did, stays open for the next
// pass to decide.
func (c *Controller) advance(ctx context.Context, r *passReads, h Heal) (open, again bool) {
	log := ctxlog.Logger(ctx).With("project", h.Project, "card_id", h.CardID, "heal_id", h.ID)

	v, err := c.view(ctx, r, h)
	if err != nil {
		log.Warn("sentinel: heal not advanced", "error", err)

		return true, false
	}

	s := nextStep(v)

	err = c.act(ctx, r, &v, s)

	switch {
	case errors.Is(err, ErrHealChanged), errors.Is(err, service.ErrCardChanged):
		return true, true
	case err != nil:
		log.Warn("sentinel: heal step failed", "stage", v.Heal.Stage, "error", err)

		return true, false
	}

	return !Terminal(v.Heal.Stage), s.Kind == stepAdvance
}

// view reads what nextStep needs for h: the card fresh, its run row, the edit
// check, and at stage apply the fix checks on a card diagnosis's fix.
func (c *Controller) view(ctx context.Context, r *passReads, h Heal) (healView, error) {
	v := healView{Heal: h, Healing: r.set.Healing, Budget: r.budget, Room: roomFor(r.health), Now: r.now}

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
	v.Edited = checkEdits(fixContext{Card: card, Snapshot: v.Run.Launch.Snapshot})

	if fix := healFix(h); h.Stage == StageApply && fix != nil {
		v.Rejected, err = c.vet(ctx, h, card, v.Run, *fix)
	}

	return v, err
}

// vet runs the fix checks with the card's subtasks and its project's verify
// read fresh.
func (c *Controller) vet(ctx context.Context, h Heal, card *board.Card, run Run, fix protocol.CardFix) ([]string, error) {
	project, err := c.cfg.Cards.GetProject(ctx, h.Project)
	if err != nil {
		return nil, fmt.Errorf("read project: %w", err)
	}

	subtasks, err := c.cfg.Cards.ListCards(ctx, h.Project, storage.CardFilter{Parent: card.ID})
	if err != nil {
		return nil, fmt.Errorf("list subtasks: %w", err)
	}

	return vetFix(fixContext{
		Card: card, Snapshot: run.Launch.Snapshot, Subtasks: subtasks, SentSubtasks: h.SentSubtasks,
		Project: project.Verify, Fix: fix, Check: h.Check,
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

		// The run may hold a worker slot now, which the next heal's room counts.
		if inFlight(h.Dispatch) {
			r.health.RunningContainers++
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
		return c.apply(ctx, h, v.Card, v.Run)
	case stepUsageRerun:
		return c.usageRerun(ctx, h, v.Card, v.Run)
	case stepSettle:
		return c.settle(ctx, h, v.Card, v.Run, s.Stage)
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
