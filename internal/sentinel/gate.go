package sentinel

import (
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// Pause reasons the budget gate gives.
const (
	reasonDailyCap = "daily cap reached"
	reasonHeadroom = "waiting for subscription headroom"
)

// budgetGate reports whether Sentinel may start paid work now. The endpoint
// channel is bounded by the daily cap. On Claude Code cost does not count:
// every window that has not reset must be below the headroom threshold.
func budgetGate(
	mode llmchannel.Mode, s state.Settings, spentToday float64, windows []protocol.SubscriptionUsage, now time.Time,
) (open bool, reason string) {
	if mode != llmchannel.ModeClaudeCode {
		if spentToday < s.DailyCapUSD {
			return true, ""
		}

		return false, reasonDailyCap
	}

	for _, w := range windows {
		if !windowReset(w, now) && (w.Status == "rejected" || w.Utilization >= s.Headroom) {
			return false, reasonHeadroom
		}
	}

	return true, ""
}

// dayStart is 00:00 UTC on now's day, where the daily cap's day begins.
func dayStart(now time.Time) time.Time {
	y, m, d := now.UTC().Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// windowReset reports whether w's reset time has passed, which makes its
// reading stale.
func windowReset(w protocol.SubscriptionUsage, now time.Time) bool {
	return !w.ResetsAt.After(now)
}

// roomFor reports whether a run kind may take a worker slot: it must leave
// one free for card runs.
func roomFor(h protocol.HealthResponse) bool {
	return h.RunningContainers+1 < h.MaxConcurrent
}
