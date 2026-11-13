package sentinel

import (
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

func TestSummarize(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	quiet := statusInput{
		Settings: enabledSettings(), LastPass: now, Now: now, Tick: 30 * time.Second,
		Mode: llmchannel.ModeEndpoint, GateOpen: true,
	}
	with := func(f func(in *statusInput)) statusInput {
		in := quiet
		f(&in)

		return in
	}

	rows := []struct {
		name   string
		in     statusInput
		state  string
		reason string
	}{
		{"off", with(func(in *statusInput) { in.Settings.Enabled = false }), StateOff, ""},
		{"off wins over a stopped controller", with(func(in *statusInput) {
			in.Settings.Enabled = false
			in.Now = now.Add(time.Hour)
		}), StateOff, ""},
		{"enabled without controller", with(func(in *statusInput) { in.NoController = true }), StateNotRunning, "no execution backend"},
		{"last pass 91 s ago", with(func(in *statusInput) { in.Now = now.Add(91 * time.Second) }), StateNotRunning, "controller stopped"},
		{"last pass 90 s ago", with(func(in *statusInput) { in.Now = now.Add(90 * time.Second) }), StateWatching, ""},
		{"gate closed", with(func(in *statusInput) {
			in.GateOpen = false
			in.GateReason = "daily cap reached"
		}), StatePaused, "daily cap reached"},
		{"paused wins over healing", with(func(in *statusInput) {
			in.GateOpen = false
			in.GateReason = "waiting for subscription headroom"
			in.Healing = 2
		}), StatePaused, "waiting for subscription headroom"},
		{"healing", with(func(in *statusInput) { in.Healing = 2 }), StateWorking, ""},
		{"quiet", quiet, StateWatching, ""},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			st := summarize(tc.in)
			assert.Equal(t, tc.state, st.State)
			assert.Equal(t, tc.reason, st.Reason)
			assert.Equal(t, tc.in.Settings.Enabled, st.Enabled)
		})
	}
}

func TestSummarizeCarriesModeCapsAndLiveWindows(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	live := protocol.SubscriptionUsage{Window: "five_hour", Utilization: 0.62, Status: "allowed", ResetsAt: now.Add(time.Hour)}
	reset := protocol.SubscriptionUsage{Window: "seven_day", Utilization: 1, Status: "rejected", ResetsAt: now}
	in := statusInput{
		Settings: enabledSettings(), LastPass: now, Now: now, Tick: 30 * time.Second,
		Mode: llmchannel.ModeClaudeCode, GateOpen: true, Windows: []protocol.SubscriptionUsage{live, reset}, Healing: 1,
	}

	st := summarize(in)
	assert.Equal(t, "claude_code", st.Mode)
	assert.InDelta(t, 20.0, st.DailyCapUSD, 0)
	assert.InDelta(t, 0.85, st.Headroom, 0)
	assert.Equal(t, 1, st.Healing)
	assert.Equal(t, []protocol.SubscriptionUsage{live}, st.Windows, "a window that has reset is no reading")

	in.Mode = llmchannel.ModeEndpoint
	assert.Nil(t, summarize(in).Windows, "windows gate only Claude Code")
}

func TestNotRunningStatus(t *testing.T) {
	assert.Equal(t, StateOff, NotRunningStatus(DefaultSettings()).State)

	st := NotRunningStatus(enabledSettings())
	assert.True(t, st.Enabled)
	assert.Equal(t, StateNotRunning, st.State)
	assert.Equal(t, "no execution backend", st.Reason)
}
