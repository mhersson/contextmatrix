package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// createdAt dates a card; before the stored record it is the record's own
// card, after it the record belongs to an earlier card with the same ID.
func createdAt(at time.Time) func(*board.Card, *state.Run) {
	return func(c *board.Card, _ *state.Run) { c.Created = at }
}

func TestRecordCardCutsARecordOlderThanItsCard(t *testing.T) {
	tests := []struct {
		name    string
		created time.Time
		wantNil bool
	}{
		{"the record's own card", runStarted.Add(-time.Hour), false},
		{"an unset created time", time.Time{}, false},
		{"a newer card with the same ID", runStarted.Add(time.Hour), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, createdAt(tc.created))

			card, err := e.ctl.recordCard(context.Background(), "alpha", "ALPHA-001", runStarted)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNil, card == nil)
		})
	}
}

func TestPassDecidesARunOlderThanItsCardWithoutAHeal(t *testing.T) {
	tests := []struct {
		name      string
		created   time.Time
		wantHeals int
	}{
		{"the run's own card", runStarted.Add(-time.Hour), 1},
		{"a newer card with the same ID", runStarted.Add(time.Hour), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, createdAt(tc.created))

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Len(t, e.store.allHeals(), tc.wantHeals)
			assert.True(t, e.store.run("alpha", "ALPHA-001").Decided)
		})
	}
}

func TestPassClosesAHealOlderThanItsCardWithoutWritingTheCard(t *testing.T) {
	tests := []struct {
		name       string
		created    time.Time
		wantClosed bool
	}{
		{"the heal's own card", epoch.Add(-time.Hour), false},
		{"a newer card with the same ID", epoch.Add(-time.Minute), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, createdAt(tc.created))
			h := e.openHeal(t, "ALPHA-001", state.StageInvestigate)

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, tc.wantClosed, state.Terminal(e.store.heal(h.ID).Stage))
			assert.Empty(t, e.cards.patches)
			assert.Empty(t, e.cards.logged)
		})
	}
}

func TestForwardRowOlderThanItsCardIsNotJudged(t *testing.T) {
	tests := []struct {
		name      string
		created   time.Duration // from the forward row's creation
		wantStage string
		wantCause bool
	}{
		{"the forward's own card", -time.Hour, state.StageForwardFailed, true},
		{"a newer card with the same ID", time.Second, state.StageForwarded, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := forwarded(t, protocol.StopCauseVerifyParked, "failed")
			created := forwardRows(e, "ALPHA-020")[0].CreatedAt.Add(tc.created)

			for _, c := range e.cards.cards {
				if c.ID == "ALPHA-020" {
					c.Created = created
				}
			}

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, tc.wantStage, forwardRows(e, "ALPHA-020")[0].Stage)

			heals := cardHeals(e, "ALPHA-020")
			require.Len(t, heals, 1)
			assert.Equal(t, tc.wantCause, heals[0].Cause == CauseForwardFixFailed)
		})
	}
}

func TestQueueAnalysesSkipsARunOlderThanItsCard(t *testing.T) {
	tests := []struct {
		name     string
		created  time.Time
		wantRows int
	}{
		{"the run's own card", runStarted.Add(-time.Hour), 1},
		{"a newer card with the same ID", runStarted.Add(time.Hour), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.store.set(analysing())
			e.stopped("ALPHA-001", protocol.StopCauseDone, createdAt(tc.created))

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Len(t, analysisRows(e), tc.wantRows)
			assert.True(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued)
		})
	}
}

func TestPassSkipsAnAnalysisOlderThanItsCard(t *testing.T) {
	tests := []struct {
		name      string
		created   time.Time
		wantStage string
	}{
		{"the analysis's own card", epoch.Add(-time.Hour), state.StageAnalyse},
		{"a newer card with the same ID", epoch.Add(-30 * time.Second), state.StageAnalyseSkipped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.store.set(analysing())
			e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed, createdAt(tc.created))
			row := e.queued(t, "ALPHA-001")

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, tc.wantStage, e.store.heal(row.ID).Stage)
		})
	}
}

func TestSourceTextsSkipsAFindingOlderThanItsCard(t *testing.T) {
	e := newPassEnv(t)
	e.cards.cards = append(e.cards.cards, &board.Card{
		ID: "ALPHA-001", Project: "alpha", Title: "Fix the build", Body: "Make the build pass.\n", Created: epoch,
	})
	earlier := state.Finding{ID: 1, Project: "alpha", CardID: "ALPHA-001", CreatedAt: epoch.Add(-time.Hour)}
	own := state.Finding{ID: 2, Project: "alpha", CardID: "ALPHA-001", CreatedAt: epoch.Add(time.Hour)}

	texts, err := e.ctl.sourceTexts(context.Background(), []state.Finding{earlier, own})
	require.NoError(t, err)

	assert.Empty(t, texts[1], "a finding older than its card has no source text")
	assert.NotEmpty(t, texts[2])
}
