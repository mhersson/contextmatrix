package sentinel

import (
	"context"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// Mark actions, as the board icon names them.
const (
	markHealing = "healing"
	markHealed  = "healed"
	markEdited  = "edited"
	markParked  = "parked"
)

const maxMarkReason = 120

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
		if m, ok := markFor(h); ok {
			marks[id] = m
		}
	}

	return marks, nil
}

// markFor is the mark a card's latest heal earns; closed and failed forwards earn none.
func markFor(h Heal) (board.SentinelMark, bool) {
	mark := func(action, label string) (board.SentinelMark, bool) {
		return board.SentinelMark{Action: action, Label: label}, true
	}

	switch h.Stage {
	case StageInvestigate:
		return mark(markHealing, "Sentinel is investigating why the run stopped")
	case StageCheck:
		return mark(markHealing, "Sentinel is checking a fix")
	case StageApply:
		return mark(markHealing, "Sentinel is applying a fix")
	case StageUsageWait:
		return mark(markHealing, "Sentinel will rerun after the usage limit resets")
	case StageRerun:
		if h.Change != nil {
			return mark(markHealing, "Sentinel changed the card and is rerunning it")
		}

		return mark(markHealing, "Sentinel is rerunning the card unchanged")
	case StageHealed:
		return mark(markHealed, "Sentinel healed this card")
	case StageRerunFailed:
		return mark(markParked, "Sentinel's rerun did not pass")
	case StageParked:
		return mark(markParked, "Sentinel left this for a human: "+firstLine(h.Reason, maxMarkReason))
	case StageForwarded, StageForwardPassed:
		return mark(markEdited, "Sentinel applied a fix from "+h.SourceCard)
	}

	return board.SentinelMark{}, false
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
