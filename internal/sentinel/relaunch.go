package sentinel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// route is how a heal reruns its card.
type route int

const (
	routeLaunch   route = iota // alone, through launch
	routePlaybook              // through its playbook run's Stop then Play
	routeNone                  // not at all: the heal parks
)

// relaunchRoute picks the rerun's route. A playbook origin reruns only through
// its active run on this instance; any other origin reruns alone unless an
// active playbook run holds the card. pb is the playbook the card's active
// lock names, nil without one.
func relaunchRoute(run Run, card *board.Card, pb *service.PlaybookDetail, instance string) (route, string) {
	if run.Launch.Origin != OriginPlaybook {
		if card.PlaybookLock.Active() {
			return routeNone, "an active playbook run holds the card"
		}

		return routeLaunch, ""
	}

	switch {
	case pb == nil || !pb.Run.Active() || !holdsCard(pb, card):
		return routeNone, "its playbook run is not active"
	case pb.Run.Instance != instance:
		return routeNone, "its playbook run belongs to another instance"
	}

	return routePlaybook, ""
}

func holdsCard(pb *service.PlaybookDetail, card *board.Card) bool {
	return slices.ContainsFunc(pb.Entries, func(e service.PlaybookEntryDetail) bool {
		return e.Type == board.EntryTypeCard && e.Project == card.Project && e.Card == card.ID
	})
}

// relaunch reruns the card on its route. The attempt is counted and the heal
// saved at rerun, with RelaunchedAt taken, before anything launches, so the
// rerun's run row starts at or after it. A refused rerun parks the heal.
func (c *Controller) relaunch(ctx context.Context, h *Heal, card *board.Card, run Run) error {
	var pb *service.PlaybookDetail

	if c.cfg.Playbooks != nil && card.PlaybookLock.Active() {
		var err error
		if pb, err = c.cfg.Playbooks.Get(ctx, card.PlaybookLock.ID); err != nil {
			return fmt.Errorf("read playbook %s: %w", card.PlaybookLock.ID, err)
		}
	}

	rt, reason := relaunchRoute(run, card, pb, c.cfg.Instance)
	if rt == routeNone {
		return c.park(ctx, h, reason)
	}

	if err := c.cfg.Store.IncrementAttempts(ctx, h.Project, h.CardID); err != nil {
		return fmt.Errorf("count the heal attempt: %w", err)
	}

	now := c.cfg.Clock.Now()
	h.RelaunchedAt, h.CostBaseUSD = &now, cardCost(card)

	if err := c.saveStage(ctx, h, StageRerun, ""); err != nil {
		return fmt.Errorf("save the rerun: %w", err)
	}

	logErr := c.logActivity(ctx, h, relaunchText(h.Change, run.Attempts+1))

	if err := c.rerun(ctx, rt, card, pb); err != nil {
		return errors.Join(logErr, c.park(ctx, h, "rerun refused: "+err.Error()))
	}

	return logErr
}

// rerun launches the card alone from todo, or through its playbook run's Stop
// then Play, whose walker moves the card to todo itself.
func (c *Controller) rerun(ctx context.Context, rt route, card *board.Card, pb *service.PlaybookDetail) error {
	if rt == routePlaybook {
		if _, err := c.cfg.Runner.Stop(ctx, pb.ID, Agent); err != nil {
			return fmt.Errorf("stop playbook %s: %w", pb.ID, err)
		}

		if _, err := c.cfg.Runner.Play(ctx, pb.ID, Agent); err != nil {
			return fmt.Errorf("play playbook %s: %w", pb.ID, err)
		}

		return nil
	}

	c.mu.Lock()
	launch := c.relauncher
	c.mu.Unlock()

	if launch == nil {
		return errors.New("no relauncher is wired")
	}

	if card.State != board.StateTodo {
		if _, err := c.cfg.Cards.TransitionTo(ctx, card.Project, card.ID, board.StateTodo); err != nil {
			return fmt.Errorf("move %s to todo: %w", card.ID, err)
		}
	}

	return launch(ctx, card.Project, card.ID)
}

// cardCost is the card's cost with its subtasks', which a settled rerun's
// charge is measured from.
func cardCost(card *board.Card) float64 {
	cost := card.SubtaskCostUSD
	if card.TokenUsage != nil {
		cost += card.TokenUsage.EstimatedCostUSD
	}

	return cost
}

// relaunchText is the activity entry of a heal that reruns its card.
func relaunchText(change *Change, attempt int) string {
	how := "to resume"
	if change != nil && change.Fresh {
		how = "fresh"
	}

	return clipActivity(fmt.Sprintf("Sentinel %s; rerunning %s, attempt %d of %d",
		changeText(change), how, attempt, MaxHealAttempts))
}

// changeText names what a fix changed on the card.
func changeText(change *Change) string {
	var parts []string

	if change != nil {
		if change.SectionAfter != change.SectionBefore {
			parts = append(parts, "updated the ## "+SectionHeading+" section")
		}

		if !reflect.DeepEqual(change.VerifyBefore, change.VerifyAfter) {
			parts = append(parts, "changed verify")
		}

		if len(change.Retired) > 0 {
			parts = append(parts, "retired "+strings.Join(change.Retired, ", "))
		}
	}

	if len(parts) == 0 {
		return "changed nothing on the card"
	}

	return strings.Join(parts, ", ")
}
