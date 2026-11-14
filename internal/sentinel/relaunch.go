package sentinel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

// route is how a heal reruns its card.
type route int

const (
	routeLaunch   route = iota // alone, through launch
	routePlaybook              // through its playbook run's Stop then Play
	routeNone                  // not at all: the heal parks
)

// relaunchPlan is a heal's rerun route, why it has none, and the playbook
// whose run a playbook route goes through.
type relaunchPlan struct {
	route    route
	reason   string
	playbook *service.PlaybookDetail
}

// planRelaunch reads the playbook the card's active lock names and decides
// the rerun's route.
func (c *Controller) planRelaunch(ctx context.Context, card *board.Card, run state.Run) (relaunchPlan, error) {
	pb, err := c.activePlaybook(ctx, card)
	if err != nil {
		return relaunchPlan{}, err
	}

	rt, reason := relaunchRoute(run, card, pb, c.cfg.Instance)

	return relaunchPlan{route: rt, reason: reason, playbook: pb}, nil
}

// relaunchRoute picks the rerun's route. A playbook origin reruns only through
// its active run on this instance, while that run is on the card, so its Stop
// normally finds no other card's worker to kill; any other origin reruns alone
// unless an active playbook run holds the card. pb is the playbook the card's
// active lock names, nil without one.
func relaunchRoute(run state.Run, card *board.Card, pb *service.PlaybookDetail, instance string) (route, string) {
	if run.Launch.Origin != state.OriginPlaybook {
		if card.PlaybookLock.Active() {
			return routeNone, "an active playbook run holds the card"
		}

		return routeLaunch, ""
	}

	switch {
	case pb == nil || !pb.Run.Active():
		return routeNone, "its playbook run is not active"
	case pb.Run.Instance != instance:
		return routeNone, "its playbook run belongs to another instance"
	case !runOn(pb, card):
		return routeNone, "its playbook run has moved on"
	}

	return routePlaybook, ""
}

// runOn reports whether pb's run is on card's entry.
func runOn(pb *service.PlaybookDetail, card *board.Card) bool {
	return slices.ContainsFunc(pb.Entries, func(e service.PlaybookEntryDetail) bool {
		return e.ID == pb.Run.Entry && e.Type == board.EntryTypeCard && e.Project == card.Project && e.Card == card.ID
	})
}

// usageRerun reruns the card after a usage limit, resuming where its run
// stopped: nothing is written to the card.
func (c *Controller) usageRerun(ctx context.Context, h *state.Heal, card *board.Card, run state.Run) error {
	plan, err := c.planRelaunch(ctx, card, run)
	if err != nil {
		return err
	}

	return c.relaunch(ctx, h, card, run, plan)
}

// relaunch reruns the card on plan's route; a plan with no route parks. The
// rerun is counted and the heal saved at rerun, with RelaunchedAt taken,
// before anything launches, so the rerun's run row starts at or after it. A
// refused rerun parks the heal, so its park is its one activity entry; the
// rerun's own entry is written once the rerun is launched.
func (c *Controller) relaunch(ctx context.Context, h *state.Heal, card *board.Card, run state.Run, plan relaunchPlan) error {
	if plan.route == routeNone {
		return c.park(ctx, h, plan.reason)
	}

	text, err := c.countRerun(ctx, h, run)
	if err != nil {
		return err
	}

	now := c.cfg.Clock.Now()
	h.RelaunchedAt, h.CostBaseUSD = &now, cardCost(card)

	if err := c.saveStage(ctx, h, state.StageRerun, ""); err != nil {
		return fmt.Errorf("save the rerun: %w", err)
	}

	if err := c.rerun(ctx, plan, card); err != nil {
		return c.park(ctx, h, "rerun refused: "+err.Error())
	}

	return c.logActivity(ctx, h, text)
}

// countRerun counts h's rerun on the card's run row and returns its activity
// entry. A usage-limit rerun has its own cap, so it spends no heal attempt.
func (c *Controller) countRerun(ctx context.Context, h *state.Heal, run state.Run) (string, error) {
	if h.Response == RespUsageRerun {
		if err := c.cfg.Store.IncrementUsageReruns(ctx, h.Project, h.CardID); err != nil {
			return "", fmt.Errorf("count the usage-limit rerun: %w", err)
		}

		return usageRerunText(run.UsageReruns + 1), nil
	}

	if err := c.cfg.Store.IncrementAttempts(ctx, h.Project, h.CardID); err != nil {
		return "", fmt.Errorf("count the heal attempt: %w", err)
	}

	return relaunchText(h.Change, run.Attempts+1), nil
}

// rerun moves the card to todo as Sentinel, guarded on card, then launches it
// alone or through its playbook run's Stop then Play. The walker relaunches a
// resumed run's card only from todo or after a failed, killed, parked or
// stalled run, and a run that ended completed without a park leaves no worker
// status. Every error's text is safe to commit: the launch's is sanitized
// where it is made.
func (c *Controller) rerun(ctx context.Context, plan relaunchPlan, card *board.Card) error {
	if card.State != board.StateTodo {
		if _, err := c.cfg.Cards.TransitionToAs(ctx, card.Project, card.ID, board.StateTodo, Agent, card.Updated); err != nil {
			return refused(ctx, "cannot move "+card.ID+" to todo", err)
		}
	}

	if plan.route == routePlaybook {
		pb := plan.playbook

		if _, err := c.cfg.Runner.Stop(ctx, pb.ID, Agent); err != nil {
			return refused(ctx, "cannot stop playbook "+pb.ID, err)
		}

		if _, err := c.cfg.Runner.Play(ctx, pb.ID, Agent); err != nil {
			return refused(ctx, "cannot play playbook "+pb.ID, err)
		}

		return nil
	}

	c.mu.Lock()
	launch := c.relauncher
	c.mu.Unlock()

	if launch == nil {
		return errors.New("no relauncher is wired")
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
func relaunchText(change *state.Change, attempt int) string {
	how := "to resume"
	if change != nil && change.Fresh {
		how = "fresh"
	}

	return clipActivity(fmt.Sprintf("Sentinel %s; rerunning %s, attempt %d of %d",
		changeText(change), how, attempt, state.MaxHealAttempts))
}

// usageRerunText is the activity entry of a heal that reruns its card after a
// usage limit.
func usageRerunText(rerun int) string {
	return fmt.Sprintf("Sentinel rerunning to resume after a usage limit, usage-limit rerun %d of %d", rerun, state.MaxUsageReruns)
}

// changeText names what a fix changed on the card.
func changeText(change *state.Change) string {
	var parts []string
	if change != nil {
		parts = changeParts(*change)
	}

	if len(parts) == 0 {
		return "changed nothing on the card"
	}

	return strings.Join(parts, ", ")
}

// changeParts names each way the fix altered the card.
func changeParts(c state.Change) []string {
	var parts []string

	if c.SectionAfter != c.SectionBefore {
		parts = append(parts, "updated the ## "+SectionHeading+" section")
	}

	if !reflect.DeepEqual(c.VerifyBefore, c.VerifyAfter) {
		parts = append(parts, "changed verify")
	}

	if len(c.Retired) > 0 {
		parts = append(parts, "retired "+strings.Join(c.Retired, ", "))
	}

	return parts
}
