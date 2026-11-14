package sentinel

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkFor(t *testing.T) {
	long := strings.Repeat("x", 300)

	tests := []struct {
		name       string
		heal       Heal
		action     string // "" = no mark
		contains   string
		notContain string
	}{
		{name: "investigate", heal: Heal{Stage: StageInvestigate}, action: "healing"},
		{name: "check", heal: Heal{Stage: StageCheck}, action: "healing"},
		{name: "apply", heal: Heal{Stage: StageApply}, action: "healing"},
		{name: "usage wait", heal: Heal{Stage: StageUsageWait}, action: "healing"},
		{name: "rerun unchanged", heal: Heal{Stage: StageRerun}, action: "healing"},
		{name: "rerun changed", heal: Heal{Stage: StageRerun, Change: &Change{}}, action: "healing"},
		{name: "healed", heal: Heal{Stage: StageHealed}, action: "healed"},
		{name: "rerun failed", heal: Heal{Stage: StageRerunFailed}, action: "parked"},
		{name: "parked", heal: Heal{Stage: StageParked, Reason: "needs a key\nsecond line"}, action: "parked", contains: "needs a key", notContain: "second line"},
		{name: "parked long reason", heal: Heal{Stage: StageParked, Reason: long}, action: "parked", contains: strings.Repeat("x", 120), notContain: strings.Repeat("x", 121)},
		{name: "forwarded", heal: Heal{Stage: StageForwarded, SourceCard: "PROJ-007"}, action: "edited", contains: "PROJ-007"},
		{name: "forward passed", heal: Heal{Stage: StageForwardPassed, SourceCard: "PROJ-008"}, action: "edited", contains: "PROJ-008"},
		{name: "closed", heal: Heal{Stage: StageClosed}},
		{name: "forward failed", heal: Heal{Stage: StageForwardFailed}},
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

	changed, _ := markFor(Heal{Stage: StageRerun, Change: &Change{}})
	unchanged, _ := markFor(Heal{Stage: StageRerun})
	assert.NotEqual(t, changed.Label, unchanged.Label)
}

func TestSentinelMarks(t *testing.T) {
	ctx := context.Background()
	st := &fakeStore{
		settings: Settings{Enabled: true},
		heals: map[int64]Heal{
			1: {ID: 1, Project: "p", CardID: "P-001", Stage: StageParked, Reason: "old"},
			2: {ID: 2, Project: "p", CardID: "P-001", Stage: StageHealed},
			3: {ID: 3, Project: "p", CardID: "P-002", Stage: StageClosed},
			4: {ID: 4, Project: "other", CardID: "O-001", Stage: StageHealed},
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
