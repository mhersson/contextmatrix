package sentinel

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestMarkFor(t *testing.T) {
	long := strings.Repeat("x", 300)

	tests := []struct {
		name       string
		heal       state.Heal
		action     string // "" = no mark
		contains   string
		notContain string
	}{
		{name: "investigate", heal: state.Heal{Stage: state.StageInvestigate}, action: "healing"},
		{name: "check", heal: state.Heal{Stage: state.StageCheck}, action: "healing"},
		{name: "apply", heal: state.Heal{Stage: state.StageApply}, action: "healing"},
		{name: "usage wait", heal: state.Heal{Stage: state.StageUsageWait}, action: "healing"},
		{name: "rerun unchanged", heal: state.Heal{Stage: state.StageRerun}, action: "healing"},
		{name: "rerun empty change", heal: state.Heal{Stage: state.StageRerun, Change: &state.Change{}}, action: "healing"},
		{name: "rerun section change", heal: state.Heal{Stage: state.StageRerun, Change: &state.Change{SectionAfter: "new"}}, action: "healing"},
		{name: "healed", heal: state.Heal{Stage: state.StageHealed}, action: "healed"},
		{name: "rerun failed", heal: state.Heal{Stage: state.StageRerunFailed}, action: "parked"},
		{name: "parked", heal: state.Heal{Stage: state.StageParked, Reason: "needs a key\nsecond line"}, action: "parked", contains: "needs a key", notContain: "second line"},
		{name: "parked long reason", heal: state.Heal{Stage: state.StageParked, Reason: long}, action: "parked", contains: strings.Repeat("x", 120), notContain: strings.Repeat("x", 121)},
		{name: "forwarded", heal: state.Heal{Stage: state.StageForwarded, SourceCard: "PROJ-007"}, action: "edited", contains: "PROJ-007"},
		{name: "forward passed", heal: state.Heal{Stage: state.StageForwardPassed, SourceCard: "PROJ-008"}, action: "edited", contains: "PROJ-008"},
		{name: "closed", heal: state.Heal{Stage: state.StageClosed}},
		{name: "forward failed", heal: state.Heal{Stage: state.StageForwardFailed}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := markFor(tt.heal)
			if tt.action == "" {
				assert.False(t, ok)

				return
			}

			require.True(t, ok)
			assert.Equal(t, tt.action, m.Action)
			assert.NotEmpty(t, m.Label)

			if tt.contains != "" {
				assert.Contains(t, m.Label, tt.contains)
			}

			if tt.notContain != "" {
				assert.NotContains(t, m.Label, tt.notContain)
			}
		})
	}

	changed, _ := markFor(state.Heal{Stage: state.StageRerun, Change: &state.Change{SectionAfter: "new"}})
	unchanged, _ := markFor(state.Heal{Stage: state.StageRerun})
	empty, _ := markFor(state.Heal{Stage: state.StageRerun, Change: &state.Change{}})

	assert.NotEqual(t, changed.Label, unchanged.Label)
	assert.Equal(t, unchanged.Label, empty.Label)
}

func TestSentinelMarks(t *testing.T) {
	ctx := context.Background()
	st := &fakeStore{
		settings: state.Settings{Enabled: true},
		heals: map[int64]state.Heal{
			1: {ID: 1, Kind: state.HealKindHeal, Project: "p", CardID: "P-001", Stage: state.StageParked, Reason: "old"},
			2: {ID: 2, Kind: state.HealKindHeal, Project: "p", CardID: "P-001", Stage: state.StageHealed},
			3: {ID: 3, Kind: state.HealKindHeal, Project: "p", CardID: "P-002", Stage: state.StageClosed},
			4: {ID: 4, Kind: state.HealKindHeal, Project: "other", CardID: "O-001", Stage: state.StageHealed},
		},
	}
	ctl := New(Config{Store: st, Pricer: ratePricer{}})

	marks, err := ctl.SentinelMarks(ctx, "p")
	require.NoError(t, err)
	require.Len(t, marks, 1)
	assert.Equal(t, "healed", marks["P-001"].Action)

	st.mu.Lock()
	st.settings.Enabled = false
	st.mu.Unlock()

	marks, err = ctl.SentinelMarks(ctx, "p")
	require.NoError(t, err)
	assert.Empty(t, marks)
}

// parkCases are the park rule's rows, shared by the board mark and the holds.
func parkCases(parkedAt time.Time) []struct {
	name   string
	heal   state.Heal
	run    *state.Run
	parked bool
} {
	relaunched := parkedAt.Add(-time.Minute)

	return []struct {
		name   string
		heal   state.Heal
		run    *state.Run
		parked bool
	}{
		{name: "parked, no run row", heal: state.Heal{Stage: state.StageParked, Reason: "needs a key"}, parked: true},
		{name: "parked, run started before the park", heal: state.Heal{Stage: state.StageParked}, run: &state.Run{StartedAt: parkedAt.Add(-time.Hour)}, parked: true},
		{name: "relaunched after the park", heal: state.Heal{Stage: state.StageParked}, run: &state.Run{StartedAt: parkedAt.Add(time.Hour)}},
		{name: "rerun_failed keeps its own rerun", heal: state.Heal{Stage: state.StageRerunFailed, RelaunchedAt: &relaunched}, run: &state.Run{StartedAt: relaunched}, parked: true},
	}
}

func TestCardHolds(t *testing.T) {
	ctx := context.Background()
	parkedAt := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	build := func(h state.Heal, run *state.Run, enabled bool) *Controller {
		h.ID, h.Kind, h.Project, h.CardID, h.UpdatedAt = 1, state.HealKindHeal, "p", "P-001", parkedAt
		st := &fakeStore{settings: state.Settings{Enabled: enabled}, heals: map[int64]state.Heal{1: h}}

		if run != nil {
			run.Project, run.CardID = "p", "P-001"
			st.runs = []state.Run{*run}
		}

		return New(Config{Store: st, Pricer: ratePricer{}})
	}

	t.Run("sentinel off", func(t *testing.T) {
		holds, err := build(state.Heal{Stage: state.StageParked}, nil, false).CardHolds(ctx, "p")
		require.NoError(t, err)
		assert.Nil(t, holds)
	})

	for _, stage := range []string{state.StageInvestigate, state.StageCheck, state.StageApply, state.StageUsageWait, state.StageRerun} {
		t.Run("healing "+stage, func(t *testing.T) {
			holds, err := build(state.Heal{Stage: stage}, nil, true).CardHolds(ctx, "p")
			require.NoError(t, err)
			assert.Equal(t, map[string]CardHold{"P-001": {Healing: true}}, holds)
		})
	}

	for _, tt := range parkCases(parkedAt) {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.heal
			h.UpdatedAt = parkedAt
			holds, err := build(h, tt.run, true).CardHolds(ctx, "p")
			require.NoError(t, err)

			if !tt.parked {
				assert.Empty(t, holds)

				return
			}

			want, _ := markFor(tt.heal)
			assert.Equal(t, map[string]CardHold{"P-001": {Reason: want.Label, Since: parkedAt}}, holds)
		})
	}

	for _, stage := range []string{state.StageHealed, state.StageClosed, state.StageForwarded} {
		t.Run("absent "+stage, func(t *testing.T) {
			holds, err := build(state.Heal{Stage: stage}, nil, true).CardHolds(ctx, "p")
			require.NoError(t, err)
			assert.Empty(t, holds)
		})
	}
}

func TestSentinelMarksPark(t *testing.T) {
	ctx := context.Background()
	parkedAt := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	for _, tt := range parkCases(parkedAt) {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.heal
			h.ID, h.Kind, h.Project, h.CardID, h.UpdatedAt = 1, state.HealKindHeal, "p", "P-001", parkedAt
			st := &fakeStore{settings: state.Settings{Enabled: true}, heals: map[int64]state.Heal{1: h}}

			if tt.run != nil {
				run := *tt.run
				run.Project, run.CardID = "p", "P-001"
				st.runs = []state.Run{run}
			}

			marks, err := New(Config{Store: st, Pricer: ratePricer{}}).SentinelMarks(ctx, "p")
			require.NoError(t, err)

			if tt.parked {
				assert.Equal(t, "parked", marks["P-001"].Action)
			} else {
				assert.Empty(t, marks)
			}
		})
	}
}

func TestSentinelMarksIgnoreAnalysisRows(t *testing.T) {
	st := &fakeStore{
		settings: state.Settings{Enabled: true},
		heals: map[int64]state.Heal{
			1: {ID: 1, Kind: state.HealKindHeal, Project: "p", CardID: "P-001", Stage: state.StageParked, Reason: "needs a key"},
			2: {ID: 2, Kind: state.HealKindAnalysis, Project: "p", CardID: "P-001", Stage: state.StageAnalyse},
			3: {ID: 3, Kind: state.HealKindAnalysis, Project: "p", CardID: "P-002", Stage: state.StageAnalyse},
		},
	}
	ctl := New(Config{Store: st, Pricer: ratePricer{}})

	marks, err := ctl.SentinelMarks(context.Background(), "p")
	require.NoError(t, err)
	require.Len(t, marks, 1, "an analysis alone marks no card")
	assert.Equal(t, "parked", marks["P-001"].Action, "a newer analysis does not replace the parked heal")
}

func TestSentinelMarksMarksCreatedCards(t *testing.T) {
	st := &fakeStore{
		settings: state.Settings{Enabled: true},
		heals: map[int64]state.Heal{
			1: {ID: 1, Kind: state.HealKindHeal, Project: "p", CardID: "P-002", Stage: state.StageParked, Reason: "needs a key"},
		},
		findings: []state.Finding{
			{ID: 1, Project: "p", State: state.FindingCarded, Output: "P-001"},
			{ID: 2, Project: "p", State: state.FindingCarded, Output: "P-002"},
			{ID: 3, Project: "p", State: state.FindingDuplicate, Output: "P-003"},
			{ID: 4, Project: "other", State: state.FindingCarded, Output: "O-001"},
		},
	}
	ctl := New(Config{Store: st, Pricer: ratePricer{}})

	marks, err := ctl.SentinelMarks(context.Background(), "p")
	require.NoError(t, err)
	require.Len(t, marks, 2)
	assert.Equal(t, markCreated, marks["P-001"].Action)
	assert.Equal(t, createdLabel, marks["P-001"].Label)
	assert.Equal(t, markParked, marks["P-002"].Action, "a heal mark wins")
}
