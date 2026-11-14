package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestBudgetGate(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	at := func(hhmm string) time.Time {
		hm, err := time.Parse("15:04", hhmm)
		require.NoError(t, err)

		return time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.UTC)
	}
	win := func(util float64, status, resets string) []protocol.SubscriptionUsage {
		return []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: util, Status: status, ResetsAt: at(resets)}}
	}

	s := state.DefaultSettings()
	s.DailyCapUSD = 10
	s.Headroom = 0.85

	rows := []struct {
		name   string
		mode   llmchannel.Mode
		spent  float64
		w      []protocol.SubscriptionUsage
		open   bool
		reason string
	}{
		{"endpoint under cap", llmchannel.ModeEndpoint, 9.99, nil, true, ""},
		{"endpoint at cap", llmchannel.ModeEndpoint, 10, nil, false, "daily cap reached"},
		{"endpoint ignores windows", llmchannel.ModeEndpoint, 0, win(1.00, "rejected", "13:00"), true, ""},
		{"cc below threshold", llmchannel.ModeClaudeCode, 0, win(0.84, "allowed", "13:00"), true, ""},
		{"cc at threshold", llmchannel.ModeClaudeCode, 0, win(0.85, "allowed", "13:00"), false, "waiting for subscription headroom"},
		{"cc rejected", llmchannel.ModeClaudeCode, 0, win(0.10, "rejected", "13:00"), false, "waiting for subscription headroom"},
		{"cc window already reset", llmchannel.ModeClaudeCode, 0, win(1.00, "rejected", "11:59"), true, ""},
		{"cc window resetting now counts as reset", llmchannel.ModeClaudeCode, 0, win(1.00, "rejected", "12:00"), true, ""},
		{
			"cc one live window over closes", llmchannel.ModeClaudeCode, 0,
			append(win(0.10, "allowed", "13:00"), win(0.90, "allowed_warning", "14:00")...), false, "waiting for subscription headroom",
		},
		{"cc spend ignored", llmchannel.ModeClaudeCode, 500, nil, true, ""},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			open, reason := budgetGate(tc.mode, s, tc.spent, tc.w, now)
			assert.Equal(t, tc.open, open)
			assert.Equal(t, tc.reason, reason)
		})
	}
}

func TestRoomFor(t *testing.T) {
	rows := []struct {
		running, limit int
		room           bool
	}{
		{3, 4, false},
		{2, 4, true},
		{0, 1, false},
	}
	for _, tc := range rows {
		assert.Equal(t, tc.room, roomFor(protocol.HealthResponse{RunningContainers: tc.running, MaxConcurrent: tc.limit}),
			"%d of %d running", tc.running, tc.limit)
	}
}

func TestInFlightReserve(t *testing.T) {
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	dispatch := func(mode string, ceiling float64) state.Heal {
		return state.Heal{Dispatch: state.Dispatch{Kind: protocol.RunKindInvestigate, Mode: mode, CeilingUSD: ceiling, At: &at}}
	}

	tests := []struct {
		name  string
		heals []state.Heal
		want  float64
	}{
		{"nothing in flight", nil, 0},
		{"each endpoint dispatch's ceiling", []state.Heal{dispatch("endpoint", 2), dispatch("endpoint", 1.5)}, 3.5},
		{"a Claude Code dispatch charges no endpoint spend", []state.Heal{dispatch("claude_code", 2), dispatch("endpoint", 2)}, 2},
		{"a check runs no model", []state.Heal{{Dispatch: state.Dispatch{Kind: protocol.RunKindCheck, At: &at}}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, inFlightReserve(tc.heals), 1e-9)
		})
	}
}

func TestRerunAccrued(t *testing.T) {
	rerun := func(stage, mode string, card *board.Card) healView {
		return healView{Heal: state.Heal{Stage: stage, CostBaseUSD: 0.5}, Card: card, Run: state.Run{Mode: mode}}
	}
	costs := &board.Card{TokenUsage: &board.TokenUsage{EstimatedCostUSD: 2}, SubtaskCostUSD: 0.5}

	tests := []struct {
		name string
		view healView
		want float64
	}{
		{"a rerun on the endpoint counts its cost so far", rerun(state.StageRerun, "endpoint", costs), 2},
		{"a rerun on Claude Code counts nothing", rerun(state.StageRerun, "claude_code", costs), 0},
		{"a rerun below its base counts nothing", rerun(state.StageRerun, "endpoint", &board.Card{}), 0},
		{"a heal not rerunning counts nothing", rerun(state.StageApply, "endpoint", costs), 0},
		{"a deleted card counts nothing", rerun(state.StageRerun, "endpoint", nil), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, rerunAccrued(tc.view), 1e-9)
		})
	}
}

func TestAnalysisGate(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	win := func(util float64, resets time.Time) []protocol.SubscriptionUsage {
		return []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: util, Status: "allowed", ResetsAt: resets}}
	}

	s := state.DefaultSettings()
	s.DailyCapUSD = 20
	s.Headroom = 0.85

	rows := []struct {
		name  string
		mode  llmchannel.Mode
		spent float64
		w     []protocol.SubscriptionUsage
		open  bool
	}{
		{"endpoint under half the cap", llmchannel.ModeEndpoint, 9.99, nil, true},
		{"endpoint at half the cap", llmchannel.ModeEndpoint, 10, nil, false},
		{"endpoint ignores windows", llmchannel.ModeEndpoint, 0, win(1, now.Add(time.Hour)), true},
		{"cc below headroom", llmchannel.ModeClaudeCode, 0, win(0.84, now.Add(time.Hour)), true},
		{"cc at headroom", llmchannel.ModeClaudeCode, 0, win(0.85, now.Add(time.Hour)), false},
		{"cc window already reset", llmchannel.ModeClaudeCode, 0, win(1, now.Add(-time.Minute)), true},
		{"cc spend ignored", llmchannel.ModeClaudeCode, 500, nil, true},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.open, analysisGate(tc.mode, s, tc.spent, tc.w, now))
		})
	}
}

func TestAnalyseCeiling(t *testing.T) {
	s := state.DefaultSettings()
	s.InvestigateCeilingUSD, s.AnalyseCeilingUSD = 2, 1

	assert.InDelta(t, 1.0, analyseCeiling(llmchannel.ModeEndpoint, s), 0)
	assert.InDelta(t, 2.0, analyseCeiling(llmchannel.ModeClaudeCode, s), 0, "Claude Code takes healing's per-run bound")
}
