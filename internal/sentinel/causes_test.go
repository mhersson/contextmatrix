package sentinel

import (
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

// TestResponseFor holds the one list of cause keys: every key the table must
// map and its response. A new cause is one row in causeTable plus its row here.
func TestResponseFor(t *testing.T) {
	tests := []struct {
		key, response string
		verifyStopped bool
	}{
		{protocol.StopCauseToolchainMissing, RespInvestigateFix, true},
		{protocol.StopCauseVerifyParked, RespInvestigateFix, true},
		{protocol.StopCauseEmptySubtask, RespInvestigateFix, false},
		{protocol.StopCauseMaxTurns, RespInvestigateFix, false},
		{protocol.StopCauseContextLimit, RespInvestigateFix, false},
		{protocol.StopCauseReviewParked, RespInvestigateFix, false},
		{CauseReviewCap, RespPark, false},
		{protocol.StopCauseUsageLimit, RespUsageRerun, false},
		{CauseForwardFixFailed, RespPark, false},
		{protocol.StopCauseBudgetExceeded, RespPark, false},
		{protocol.StopCauseNoModel, RespPark, false},
		{protocol.StopCauseSplitOverflow, RespPark, false},
		{protocol.StopCauseGatesParked, RespInvestigatePark, false},
		{protocol.StopCauseError, RespInvestigatePark, false},
		{protocol.StopCauseShutdown, RespInvestigatePark, false},
		{protocol.StopCauseTimeout, RespInvestigatePark, false},
		{protocol.StopCauseInfrastructure, RespInvestigatePark, false},
		{protocol.StopCauseKilled, RespInvestigatePark, false},
		{protocol.StopCauseEnded, RespInvestigatePark, false},
		{CauseStalled, RespInvestigatePark, false},
		{CauseUnknown, RespInvestigatePark, false},
		{protocol.StopCauseDone, RespIgnore, false},
		{CauseHumanStop, RespIgnore, false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			rows := 0

			for _, row := range causeTable {
				if row.Key == tt.key {
					rows++
				}
			}

			assert.Equal(t, 1, rows, "one table row per key")

			row := responseFor(tt.key)
			assert.Equal(t, tt.response, row.Response)
			assert.Equal(t, tt.response == RespPark, row.Reason != "")
			assert.Equal(t, tt.verifyStopped, row.VerifyStopped)
		})
	}

	assert.Len(t, causeTable, len(tests), "a table row with no listed key")

	t.Run("a key the table lacks reads as unknown", func(t *testing.T) {
		assert.Equal(t, responseFor(CauseUnknown), responseFor("brand_new_cause"))
	})
}
