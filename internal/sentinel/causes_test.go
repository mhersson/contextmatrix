package sentinel

import (
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

func TestCauseTableCoversEveryStopCause(t *testing.T) {
	keys := []string{
		protocol.StopCauseDone, protocol.StopCauseEnded, protocol.StopCauseReviewParked,
		protocol.StopCauseGatesParked, protocol.StopCauseUsageLimit, protocol.StopCauseBudgetExceeded,
		protocol.StopCauseContextLimit, protocol.StopCauseMaxTurns, protocol.StopCauseToolchainMissing,
		protocol.StopCauseNoModel, protocol.StopCauseVerifyParked, protocol.StopCauseEmptySubtask,
		protocol.StopCauseSplitOverflow, protocol.StopCauseError,
		protocol.StopCauseKilled, protocol.StopCauseShutdown, protocol.StopCauseTimeout,
		protocol.StopCauseInfrastructure,
		CauseStalled, CauseUnknown, CauseHumanStop, CauseReviewCap, CauseForwardFixFailed,
	}

	for _, k := range keys {
		n := 0

		for _, row := range causeTable {
			if row.Key == k {
				n++
			}
		}

		assert.Equal(t, 1, n, "rows for %q", k)
	}

	assert.Len(t, causeTable, len(keys), "a row with no listed key")
}

func TestResponseFor(t *testing.T) {
	tests := []struct {
		key, response string
		verifyStopped bool
	}{
		{"toolchain_missing", RespInvestigateFix, true},
		{"verify_parked", RespInvestigateFix, true},
		{"empty_subtask", RespInvestigateFix, false},
		{"max_turns", RespInvestigateFix, false},
		{"context_limit", RespInvestigateFix, false},
		{"review_parked", RespInvestigateFix, false},
		{"review_cap", RespPark, false},
		{"usage_limit", RespUsageRerun, false},
		{"forward_fix_failed", RespPark, false},
		{"budget_exceeded", RespPark, false},
		{"no_model", RespPark, false},
		{"split_overflow", RespPark, false},
		{"gates_parked", RespInvestigatePark, false},
		{"error", RespInvestigatePark, false},
		{"shutdown", RespInvestigatePark, false},
		{"timeout", RespInvestigatePark, false},
		{"infrastructure", RespInvestigatePark, false},
		{"killed", RespInvestigatePark, false},
		{"ended", RespInvestigatePark, false},
		{"stalled", RespInvestigatePark, false},
		{"unknown", RespInvestigatePark, false},
		{"done", RespIgnore, false},
		{"human_stop", RespIgnore, false},
		{"brand_new_cause", RespInvestigatePark, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			row := responseFor(tt.key)
			assert.Equal(t, tt.response, row.Response)
			assert.Equal(t, tt.response == RespPark, row.Reason != "")
			assert.Equal(t, tt.verifyStopped, row.VerifyStopped)
		})
	}
}
