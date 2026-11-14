package sentinel

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
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
