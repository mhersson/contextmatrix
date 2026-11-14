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
// channel is bounded by the daily cap, against spentToday: what is charged and
// what work in flight will charge. On Claude Code cost does not count: every
// window that has not reset must be below the headroom threshold.
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

// analysisGate reports whether an analysis may start now. On the endpoint
// channel analyses stop at half the daily cap, so they never starve healing;
// on Claude Code they follow the budget gate's headroom rule.
func analysisGate(mode llmchannel.Mode, s state.Settings, spent float64, windows []protocol.SubscriptionUsage, now time.Time) bool {
	if mode != llmchannel.ModeClaudeCode {
		return spent < s.DailyCapUSD/2
	}

	open, _ := budgetGate(mode, s, spent, windows, now)

	return open
}

// analyseCeiling is an analysis's max_cost_usd. Claude Code is exempt from
// analyse_ceiling_usd, so it takes healing's own per-run bound there.
func analyseCeiling(mode llmchannel.Mode, s state.Settings) float64 {
	if mode == llmchannel.ModeClaudeCode {
		return s.InvestigateCeilingUSD
	}

	return s.AnalyseCeilingUSD
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

// inFlightReserve is what the run kinds in flight will charge the endpoint
// ledger at most: each endpoint dispatch's ceiling.
func inFlightReserve(heals []state.Heal) float64 {
	var usd float64

	for _, h := range heals {
		if h.Dispatch.Mode == string(llmchannel.ModeEndpoint) {
			usd += h.Dispatch.CeilingUSD
		}
	}

	return usd
}

// rerunAccrued is what a heal's rerun on the endpoint has cost so far: the
// charge its settle would make now.
func rerunAccrued(v healView) float64 {
	if v.Heal.Stage != state.StageRerun || v.Card == nil || v.Run.Mode != string(llmchannel.ModeEndpoint) {
		return 0
	}

	return rerunCharge(v.Heal, v.Card)
}
