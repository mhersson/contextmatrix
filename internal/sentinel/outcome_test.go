package sentinel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestActivityText(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		keeps  string // the part of the reason the text must carry
	}{
		{"short reason", "budget exceeded", "budget exceeded"},
		{"edit items", "edited since launch: title edited, verify edited", "title edited, verify edited"},
		{"a 32 KiB diagnosis", "card diagnosis: " + strings.Repeat("ab", 16<<10), "card diagnosis: abab"},
		{"cut inside a rune", strings.Repeat("é", 4000), "éé"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := activityText(state.Heal{Stage: state.StageParked, Reason: tc.reason})

			assert.Contains(t, text, tc.keeps)
			assert.LessOrEqual(t, len(text), maxActivityText, "the card service refuses a longer message")
			assert.True(t, utf8.ValidString(text))
		})
	}
}

func TestParkWritesOneEntry(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)

	require.NoError(t, e.ctl.park(context.Background(), h, "budget exceeded"))

	stored := e.store.heal(1)
	assert.Equal(t, state.StageParked, stored.Stage)
	assert.Equal(t, "budget exceeded", stored.Reason)
	assert.Equal(t, epoch, stored.UpdatedAt)

	entries := e.cards.logged["ALPHA-001"]
	require.Len(t, entries, 1)
	assert.Equal(t, Agent, entries[0].Agent)
	assert.Equal(t, "sentinel", entries[0].Action)
	assert.Equal(t, activityText(stored), entries[0].Message)
}

func TestParkStaleHealWritesNothing(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)
	h.Version = 3 // a callback wrote the row since h was read

	require.ErrorIs(t, e.ctl.park(context.Background(), h, "budget exceeded"), state.ErrHealChanged)
	assert.Equal(t, state.StageInvestigate, e.store.heal(1).Stage)
	assert.Empty(t, e.cards.logged)
}

func TestParkUnreportedChargesTheCeilingOnce(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)
	at := epoch.Add(-11 * time.Minute)
	h.Dispatch = state.Dispatch{Kind: protocol.RunKindInvestigate, Mode: "claude_code", CeilingUSD: 2, At: &at}
	e.store.putHeal(*h)

	require.NoError(t, e.ctl.parkUnreported(context.Background(), h, "the investigate run did not report"))

	stored := e.store.heal(1)
	assert.Equal(t, state.StageParked, stored.Stage)
	assert.False(t, inFlight(stored.Dispatch), "saved with the park, so no late report matches it")
	assert.Equal(t, []ledgerRow{{HealID: 1, Mode: "claude_code", USD: 2, At: epoch}}, e.store.charges())
	assert.Len(t, e.cards.logged["ALPHA-001"], 1)
}

func TestParkUnreportedStaleHealChargesNothing(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)
	at := epoch.Add(-11 * time.Minute)
	h.Dispatch = state.Dispatch{Kind: protocol.RunKindInvestigate, Mode: "endpoint", CeilingUSD: 2, At: &at}
	e.store.putHeal(*h)
	h.Version = 3 // the report was recorded and charged since h was read

	require.ErrorIs(t, e.ctl.parkUnreported(context.Background(), h, "the investigate run did not report"), state.ErrHealChanged)
	assert.Empty(t, e.store.charges())
	assert.Empty(t, e.cards.logged)
}

func TestCloseHealWritesNoEntry(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageCheck)

	require.NoError(t, e.ctl.closeHeal(context.Background(), h, "the card was closed"))

	stored := e.store.heal(1)
	assert.Equal(t, state.StageClosed, stored.Stage)
	assert.Equal(t, "the card was closed", stored.Reason)
	assert.Empty(t, e.cards.logged)
}

// projectEvents drains the project-level sentinel.changed events (empty card ID) delivered so far as "<project>".
func projectEvents(ch <-chan events.Event) []string {
	_, projects := changedEvents(ch)

	return projects
}

// changedEvents drains the sentinel.changed events delivered so far: the card ones as "<project>/<card>", the project ones as "<project>".
func changedEvents(ch <-chan events.Event) (cards, projects []string) {
	for {
		select {
		case ev := <-ch:
			switch {
			case ev.Type != events.SentinelChanged || ev.Project == "":
			case ev.CardID == "":
				projects = append(projects, ev.Project)
			default:
				cards = append(cards, ev.Project+"/"+ev.CardID)
			}
		default:
			return cards, projects
		}
	}
}

// cardEvents drains the per-card sentinel.changed events delivered so far as "<project>/<card>".
func cardEvents(ch <-chan events.Event) []string {
	var out []string

	for {
		select {
		case ev := <-ch:
			if ev.Type == events.SentinelChanged && ev.CardID != "" {
				out = append(out, ev.Project+"/"+ev.CardID)
			}
		default:
			return out
		}
	}
}

func TestHealWritesPublishTheCardEvent(t *testing.T) {
	e := newDispatchEnv(t)
	ch, unsubscribe := e.bus.Subscribe()
	t.Cleanup(unsubscribe)

	h := e.heal(state.StageRerun)
	require.NoError(t, e.ctl.saveStage(context.Background(), h, state.StageRerunFailed, ""))
	assert.Equal(t, []string{"alpha/ALPHA-001"}, cardEvents(ch), "a stage save")

	h.Version = 9 // stale: the save is refused
	require.ErrorIs(t, e.ctl.saveStage(context.Background(), h, state.StageParked, "r"), state.ErrHealChanged)
	assert.Empty(t, cardEvents(ch), "a refused save")

	_, err := e.ctl.openHeal(context.Background(), state.Heal{Kind: state.HealKindForward, Project: "alpha", CardID: "ALPHA-002"})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha/ALPHA-002"}, cardEvents(ch), "an opened heal")
}

func TestAFailedFindingWritePublishesNothing(t *testing.T) {
	e := newDispatchEnv(t)
	ch, unsubscribe := e.bus.Subscribe()
	t.Cleanup(unsubscribe)

	e.store.outcomeErr = errors.New("disk full")
	f := state.Finding{ID: 1, Project: "alpha", CardID: "ALPHA-001"}

	require.Error(t, e.ctl.setFindingOutcome(context.Background(), f, state.FindingDropped, "r", "", 0, epoch))
	assert.Empty(t, cardEvents(ch))
}
