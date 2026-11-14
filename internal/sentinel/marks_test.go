package sentinel

import (
	"context"
	"strings"
	"testing"

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
			1: {ID: 1, Project: "p", CardID: "P-001", Stage: state.StageParked, Reason: "old"},
			2: {ID: 2, Project: "p", CardID: "P-001", Stage: state.StageHealed},
			3: {ID: 3, Project: "p", CardID: "P-002", Stage: state.StageClosed},
			4: {ID: 4, Project: "other", CardID: "O-001", Stage: state.StageHealed},
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
