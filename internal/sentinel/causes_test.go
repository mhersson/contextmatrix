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
	}{
		{"toolchain_missing", RespInvestigateFix},
		{"verify_parked", RespInvestigateFix},
		{"empty_subtask", RespInvestigateFix},
		{"max_turns", RespInvestigateFix},
		{"context_limit", RespInvestigateFix},
		{"review_parked", RespInvestigateFix},
		{"review_cap", RespPark},
		{"usage_limit", RespUsageRerun},
		{"forward_fix_failed", RespPark},
		{"budget_exceeded", RespPark},
		{"no_model", RespPark},
		{"split_overflow", RespPark},
		{"gates_parked", RespInvestigatePark},
		{"error", RespInvestigatePark},
		{"shutdown", RespInvestigatePark},
		{"timeout", RespInvestigatePark},
		{"infrastructure", RespInvestigatePark},
		{"killed", RespInvestigatePark},
		{"ended", RespInvestigatePark},
		{"stalled", RespInvestigatePark},
		{"unknown", RespInvestigatePark},
		{"done", RespIgnore},
		{"human_stop", RespIgnore},
		{"brand_new_cause", RespInvestigatePark},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			row := responseFor(tt.key)
			assert.Equal(t, tt.response, row.Response)
			assert.Equal(t, tt.response == RespPark, row.Reason != "")
		})
	}
}
