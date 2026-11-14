package sentinel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// Mark actions, as the board icon names them.
const (
	markHealing = "healing"
	markHealed  = "healed"
	markEdited  = "edited"
	markParked  = "parked"
	markCreated = "created"
	// markAnalysing is a card whose run an analysis is reading.
	markAnalysing = "analysing"
)

const maxMarkReason = 120

// createdLabel is the mark of a card Sentinel created from a finding.
const createdLabel = "Sentinel created this card from a run's friction"

const analysingLabel = "Sentinel is analysing the last run"

// SentinelMarks answers each card's board mark from its latest heal. It
// answers none while Sentinel is off.
func (c *Controller) SentinelMarks(ctx context.Context, project string) (map[string]board.SentinelMark, error) {
	set, _, _, err := c.cfg.Store.SentinelSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("sentinel settings: %w", err)
	}

	if !set.Enabled {
		return nil, nil
	}

	latest, err := c.cfg.Store.LatestHeals(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("latest heals: %w", err)
	}

	marks := make(map[string]board.SentinelMark, len(latest))

	for id, h := range latest {
		m, ok := markFor(h)
		if !ok {
			continue
		}

		if m.Action == markParked {
			stands, err := c.parkStands(ctx, project, h)
			if err != nil {
				return nil, err
			}

			if !stands {
				continue
			}
		}

		marks[id] = m
	}

	if err := c.addCreatedMarks(ctx, project, marks); err != nil {
		return nil, err
	}

	if err := c.addAnalysingMarks(ctx, project, marks); err != nil {
		return nil, err
	}

	return marks, nil
}

// withAnalysis is the mark a card shows while an analysis of it is in flight,
// given the mark it would otherwise show. A working or static heal and a
// parked mark keep their place; healed, edited, created and no mark give way.
func withAnalysis(current board.SentinelMark, has bool) board.SentinelMark {
	if has && (current.Working || current.Action == markHealing || current.Action == markParked) {
		return current
	}

	return board.SentinelMark{Action: markAnalysing, Label: analysingLabel, Working: true}
}

// analysisInFlight is whether an open analysis row has been sent and not answered.
func analysisInFlight(h state.Heal) bool {
	d := h.Dispatch

	return d.At != nil && d.AnsweredRunID == "" && d.Failure == ""
}

// addAnalysingMarks overlays the analysing mark on each card of the project
// with an analysis in flight, by withAnalysis's precedence.
func (c *Controller) addAnalysingMarks(ctx context.Context, project string, marks map[string]board.SentinelMark) error {
	open, err := c.cfg.Store.OpenAnalyses(ctx)
	if err != nil {
		return fmt.Errorf("open analyses: %w", err)
	}

	for _, h := range open {
		if h.Project != project || !analysisInFlight(h) {
			continue
		}

		current, has := marks[h.CardID]
		marks[h.CardID] = withAnalysis(current, has)
	}

	return nil
}

// CardHold is where Sentinel stands on a card a human may need to act on.
type CardHold struct {
	Healing bool      // a heal is open short of parking: nothing waits for a human yet
	Reason  string    // set when parked: the board mark's label
	Since   time.Time // set when parked: when the heal parked
}

// CardHolds answers, per card with a latest heal, whether Sentinel is still
// healing it or has parked it for a human. It answers none while Sentinel is off.
func (c *Controller) CardHolds(ctx context.Context, project string) (map[string]CardHold, error) {
	set, _, _, err := c.cfg.Store.SentinelSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("sentinel settings: %w", err)
	}

	if !set.Enabled {
		return nil, nil
	}

	latest, err := c.cfg.Store.LatestHeals(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("latest heals: %w", err)
	}

	holds := make(map[string]CardHold, len(latest))

	for id, h := range latest {
		switch h.Stage {
		case state.StageInvestigate, state.StageCheck, state.StageApply, state.StageUsageWait, state.StageRerun:
			holds[id] = CardHold{Healing: true}
		case state.StageParked, state.StageRerunFailed:
			stands, err := c.parkStands(ctx, project, h)
			if err != nil {
				return nil, err
			}

			if stands {
				m, _ := markFor(h)
				holds[id] = CardHold{Reason: m.Label, Since: h.UpdatedAt}
			}
		}
	}

	return holds, nil
}

// parkStands is whether a parked heal still holds: it ends when the card is
// launched again. A terminal heal's UpdatedAt is when it parked.
func (c *Controller) parkStands(ctx context.Context, project string, h state.Heal) (bool, error) {
	run, err := c.cfg.Store.CardRunRow(ctx, project, h.CardID)
	if err != nil {
		return false, fmt.Errorf("read run row: %w", err)
	}

	return run == nil || !run.StartedAt.After(h.UpdatedAt), nil
}

// markFor is the mark a card's latest heal earns; closed and failed forwards earn none.
func markFor(h state.Heal) (board.SentinelMark, bool) {
	mark := func(action, label string) (board.SentinelMark, bool) {
		return board.SentinelMark{Action: action, Label: label, Working: working(h.Stage)}, true
	}

	switch h.Stage {
	case state.StageInvestigate:
		return mark(markHealing, "Sentinel is investigating why the run stopped")
	case state.StageCheck:
		return mark(markHealing, "Sentinel is checking a fix")
	case state.StageApply:
		return mark(markHealing, "Sentinel is applying a fix")
	case state.StageUsageWait:
		return mark(markHealing, "Sentinel will rerun after the usage limit resets")
	case state.StageRerun:
		if h.Change != nil && len(changeParts(*h.Change)) > 0 {
			return mark(markHealing, "Sentinel changed the card and is rerunning it")
		}

		return mark(markHealing, "Sentinel is rerunning the card unchanged")
	case state.StageHealed:
		return mark(markHealed, "Sentinel healed this card")
	case state.StageRerunFailed:
		return mark(markParked, "Sentinel's rerun did not pass")
	case state.StageParked:
		return mark(markParked, "Sentinel left this for a human: "+firstLine(h.Reason, maxMarkReason))
	case state.StageForwarded, state.StageForwardPassed:
		return mark(markEdited, "Sentinel applied a fix from "+h.SourceCard)
	}

	return board.SentinelMark{}, false
}

// working is whether Sentinel is acting on the card in stage: a heal waiting
// for the usage limit to reset is not.
func working(stage string) bool {
	switch stage {
	case state.StageInvestigate, state.StageCheck, state.StageApply, state.StageRerun:
		return true
	}

	return false
}

// firstLine is s up to its first line break, cut to max runes.
func firstLine(s string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	r := []rune(line)
	if len(r) > limit {
		return string(r[:limit])
	}

	return line
}

// addCreatedMarks marks each card Sentinel created from a finding that has no
// heal mark.
func (c *Controller) addCreatedMarks(ctx context.Context, project string, marks map[string]board.SentinelMark) error {
	carded, err := c.cfg.Store.CardedFindings(ctx, project)
	if err != nil {
		return fmt.Errorf("carded findings: %w", err)
	}

	for id := range carded {
		if _, ok := marks[id]; !ok {
			marks[id] = board.SentinelMark{Action: markCreated, Label: createdLabel}
		}
	}

	return nil
}
