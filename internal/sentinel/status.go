package sentinel

import (
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

// Status states.
const (
	StateOff        = "off"
	StateWatching   = "watching"
	StateWorking    = "working"
	StatePaused     = "paused"
	StateNotRunning = "not_running"
)

// staleTicks is how many ticks without a pass make the controller read as
// stopped.
const staleTicks = 3

// Status is what Sentinel is doing, as the sidebar strip shows it.
type Status struct {
	Enabled       bool                         `json:"enabled"`
	State         string                       `json:"state"`
	Reason        string                       `json:"reason,omitempty"`
	Mode          string                       `json:"mode"`
	Healing       int                          `json:"healing"`
	HealedToday   int                          `json:"healed_today"`
	SpentTodayUSD float64                      `json:"spent_today_usd"`
	DailyCapUSD   float64                      `json:"daily_cap_usd"`
	Headroom      float64                      `json:"headroom"`
	Windows       []protocol.SubscriptionUsage `json:"windows,omitempty"`
}

// statusInput is everything a Status is derived from.
type statusInput struct {
	Settings     Settings
	NoController bool // no execution backend, so no controller exists
	LastPass     time.Time
	Now          time.Time
	Tick         time.Duration
	Mode         llmchannel.Mode
	GateOpen     bool
	GateReason   string
	Windows      []protocol.SubscriptionUsage
	Healing      int
}

// NotRunningStatus is what the API reports when no controller exists.
func NotRunningStatus(s Settings) Status {
	return summarize(statusInput{Settings: s, NoController: true})
}

// summarize derives the status. The first matching state wins: off, not
// running, paused, working, watching.
func summarize(in statusInput) Status {
	st := Status{
		Enabled:     in.Settings.Enabled,
		Mode:        string(in.Mode),
		Healing:     in.Healing,
		DailyCapUSD: in.Settings.DailyCapUSD,
		Headroom:    in.Settings.Headroom,
	}

	if in.Mode == llmchannel.ModeClaudeCode {
		for _, w := range in.Windows {
			if !windowReset(w, in.Now) {
				st.Windows = append(st.Windows, w)
			}
		}
	}

	switch {
	case !in.Settings.Enabled:
		st.State = StateOff
	case in.NoController:
		st.State, st.Reason = StateNotRunning, "no execution backend"
	case in.Now.Sub(in.LastPass) > staleTicks*in.Tick:
		st.State, st.Reason = StateNotRunning, "controller stopped"
	case !in.GateOpen:
		st.State, st.Reason = StatePaused, in.GateReason
	case in.Healing > 0:
		st.State = StateWorking
	default:
		st.State = StateWatching
	}

	return st
}
