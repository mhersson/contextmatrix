package sentinel

import (
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
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

	s := DefaultSettings()
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
