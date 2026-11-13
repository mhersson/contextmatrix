package sentinel

import (
	protocol "github.com/mhersson/contextmatrix-protocol"
)

// What Sentinel does about a cause.
const (
	RespInvestigateFix  = "investigate_fix"  // investigate, then fix the card and rerun
	RespInvestigatePark = "investigate_park" // investigate, then park with the diagnosis
	RespPark            = "park"             // park with the row's reason, no investigation
	RespUsageRerun      = "usage_rerun"      // wait for headroom, then rerun
	RespIgnore          = "ignore"
)

// Cause keys only CM knows; the rest are protocol stop causes.
const (
	CauseStalled          = "stalled"
	CauseUnknown          = "unknown"
	CauseHumanStop        = "human_stop"
	CauseReviewCap        = "review_cap"
	CauseForwardFixFailed = "forward_fix_failed"
)

type causeRow struct {
	Key, Response string
	Reason        string // the park reason of a RespPark row
}

// causeTable is the one place a cause maps to a response: a new cause is one
// row here plus its row in the tests.
var causeTable = []causeRow{
	{Key: protocol.StopCauseToolchainMissing, Response: RespInvestigateFix},
	{Key: protocol.StopCauseVerifyParked, Response: RespInvestigateFix},
	{Key: protocol.StopCauseEmptySubtask, Response: RespInvestigateFix},
	{Key: protocol.StopCauseMaxTurns, Response: RespInvestigateFix},
	{Key: protocol.StopCauseContextLimit, Response: RespInvestigateFix},
	{Key: protocol.StopCauseReviewParked, Response: RespInvestigateFix},
	{Key: CauseReviewCap, Response: RespPark, Reason: "review rounds used up"},
	{Key: protocol.StopCauseUsageLimit, Response: RespUsageRerun},
	{Key: CauseForwardFixFailed, Response: RespPark, Reason: "a fix Sentinel forwarded to this card did not hold"},
	{Key: protocol.StopCauseBudgetExceeded, Response: RespPark, Reason: "budget exceeded"},
	{Key: protocol.StopCauseNoModel, Response: RespPark, Reason: "no model available"},
	{Key: protocol.StopCauseSplitOverflow, Response: RespPark, Reason: "the split overflowed"},
	{Key: protocol.StopCauseGatesParked, Response: RespInvestigatePark},
	{Key: protocol.StopCauseError, Response: RespInvestigatePark},
	{Key: protocol.StopCauseShutdown, Response: RespInvestigatePark},
	{Key: protocol.StopCauseTimeout, Response: RespInvestigatePark},
	{Key: protocol.StopCauseInfrastructure, Response: RespInvestigatePark},
	{Key: protocol.StopCauseKilled, Response: RespInvestigatePark},
	{Key: protocol.StopCauseEnded, Response: RespInvestigatePark},
	{Key: CauseStalled, Response: RespInvestigatePark},
	{Key: CauseUnknown, Response: RespInvestigatePark},
	{Key: protocol.StopCauseDone, Response: RespIgnore},
	{Key: CauseHumanStop, Response: RespIgnore},
}

// responseFor returns the row for key; a key the table lacks (a newer agent's
// cause) reads as CauseUnknown.
func responseFor(key string) causeRow {
	var unknown causeRow

	for _, row := range causeTable {
		if row.Key == key {
			return row
		}

		if row.Key == CauseUnknown {
			unknown = row
		}
	}

	return unknown
}
