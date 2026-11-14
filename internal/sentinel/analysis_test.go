package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestQueueStep(t *testing.T) {
	stoppedAt := epoch.Add(-20 * time.Minute)
	settled := state.Run{RunID: "r1", StoppedAt: &stoppedAt}
	idle := &board.Card{State: board.StateReview, Updated: stoppedAt}

	with := func(r state.Run, edit func(*state.Run)) state.Run {
		edit(&r)

		return r
	}

	tests := []struct {
		name string
		run  state.Run
		card *board.Card
		want queueAction
	}{
		{"settled run", settled, idle, queueRow},
		{"card gone", settled, nil, queueSkip},
		{"human stop", with(settled, func(r *state.Run) { r.HumanStop = true }), idle, queueSkip},
		{
			"human stop while the worker still reports busy", with(settled, func(r *state.Run) { r.HumanStop = true }),
			&board.Card{WorkerStatus: "running", Updated: stoppedAt}, queueSkip,
		},
		{"worker still running", settled, &board.Card{WorkerStatus: "running", Updated: stoppedAt}, queueWait},
		{"worker queued", settled, &board.Card{WorkerStatus: "queued", Updated: stoppedAt}, queueWait},
		{
			"no terminal status yet, card written just now", with(settled, func(r *state.Run) { r.StoppedAt = nil }),
			&board.Card{Updated: epoch}, queueWait,
		},
		{
			"no terminal status, card unwritten past the grace", with(settled, func(r *state.Run) { r.StoppedAt = nil }),
			&board.Card{Updated: epoch.Add(-settleGrace)}, queueRow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, queueStep(tt.run, tt.card, epoch))
		})
	}
}

func TestNextAnalysis(t *testing.T) {
	ago := func(d time.Duration) *time.Time {
		at := epoch.Add(-d)

		return &at
	}
	sent := func(d time.Duration) state.Dispatch {
		return state.Dispatch{Kind: protocol.RunKindAnalyse, Mode: "endpoint", CeilingUSD: 1, At: ago(d)}
	}
	answered := func(failure string) state.Dispatch {
		d := sent(time.Minute)
		d.AnsweredRunID, d.Failure = "rk-1", failure

		return d
	}
	investigation := &protocol.InvestigateResult{Cause: protocol.DiagnosisCard, Diagnosis: "the verify needs make generate first"}

	ready := analysisView{
		Row: state.Heal{
			ID: 7, Kind: state.HealKindAnalysis, Project: "alpha", CardID: "ALPHA-001", RunStarted: runStarted,
			RunID: "run-ALPHA-001", Stage: state.StageAnalyse, CreatedAt: epoch.Add(-time.Hour),
		},
		Card: &board.Card{ID: "ALPHA-001", Project: "alpha", State: board.StateDone},
		Run:  &state.Run{StartedAt: runStarted, Decided: true, Launch: state.LaunchFacts{Autonomous: true}},
		On:   true, Healing: true, GateOpen: true, Room: true, Now: epoch,
	}
	with := func(edits ...func(*analysisView)) analysisView {
		v := ready
		run := *ready.Run
		v.Run = &run

		for _, edit := range edits {
			edit(&v)
		}

		return v
	}
	dispatched := func(d state.Dispatch) func(*analysisView) { return func(v *analysisView) { v.Row.Dispatch = d } }
	cardGone := func(v *analysisView) { v.Card = nil }
	off := func(v *analysisView) { v.On = false }
	healAt := func(stage string, inv *protocol.InvestigateResult) func(*analysisView) {
		return func(v *analysisView) {
			v.Heal = &state.Heal{Kind: state.HealKindHeal, RunStarted: runStarted, Stage: stage, Investigation: inv}
		}
	}
	undecided := func(v *analysisView) { v.Run.Decided = false }

	wait := analysisStep{Action: analysisWait}
	send := analysisStep{Action: analysisDispatch}
	end := func(stage, reason string) analysisStep {
		return analysisStep{Action: analysisEnd, Stage: stage, Reason: reason}
	}
	neverReported := analysisStep{
		Action: analysisEnd, Stage: state.StageAnalyseFailed, Reason: reasonAnalysisUnreported, ChargeCeiling: true,
	}

	tests := []struct {
		name string
		v    analysisView
		want analysisStep
	}{
		{"ready", ready, send},

		{"in flight and listed", with(dispatched(sent(time.Hour)), func(v *analysisView) { v.Listed = true }), wait},
		{"in flight within the settle grace", with(dispatched(sent(time.Minute))), wait},
		{"in flight, unlisted past the settle grace", with(dispatched(sent(settleGrace))), neverReported},
		{"in flight beats a deleted card", with(dispatched(sent(time.Minute)), cardGone), wait},
		{"an unreported run still ends while analyses are off", with(dispatched(sent(settleGrace)), off), neverReported},

		{"answered with a result", with(dispatched(answered("")), func(v *analysisView) {
			v.Row.Analysis = &protocol.AnalyseResult{Findings: []protocol.FrictionFinding{}}
		}), end(state.StageAnalysed, "")},
		{"answered with a failure", with(dispatched(answered("run log not found"))), end(state.StageAnalyseFailed, "run log not found")},
		{"an answer still ends while analyses are off", with(dispatched(answered("run log not found")), off), end(state.StageAnalyseFailed, "run log not found")},
		{"an answer beats a deleted card", with(dispatched(answered("run log not found")), cardGone), end(state.StageAnalyseFailed, "run log not found")},

		{"card deleted", with(cardGone), end(state.StageAnalyseSkipped, reasonAnalysisCardGone)},
		{"a deleted card ends even while analyses are off", with(cardGone, off), end(state.StageAnalyseSkipped, reasonAnalysisCardGone)},

		{"queued more than 7 days ago", with(func(v *analysisView) { v.Row.CreatedAt = epoch.Add(-analysisMaxAge - time.Second) }), end(state.StageAnalyseSkipped, reasonAnalysisTooOld)},
		{"queued 7 days ago to the second", with(func(v *analysisView) { v.Row.CreatedAt = epoch.Add(-analysisMaxAge) }), send},

		{"the run's heal holds an investigation", with(healAt(state.StageParked, investigation)), end(state.StageAnalyseSkipped, reasonAnalysisInvestigated)},
		{"an investigation covers the run with healing off", with(healAt(state.StageApply, investigation), func(v *analysisView) { v.Healing = false }), end(state.StageAnalyseSkipped, reasonAnalysisInvestigated)},
		{"the run's heal is at investigate", with(healAt(state.StageInvestigate, nil)), wait},
		{"the run's heal is at investigate with healing off", with(healAt(state.StageInvestigate, nil), func(v *analysisView) { v.Healing = false }), send},
		{"the run's heal never investigated", with(healAt(state.StageParked, nil)), send},

		{"the run is still undecided", with(undecided), wait},
		{"undecided with healing off", with(undecided, func(v *analysisView) { v.Healing = false }), send},
		{"undecided, but the card ran again since", with(undecided, func(v *analysisView) { v.Run.StartedAt = runStarted.Add(time.Hour) }), send},
		{"a run not launched autonomous is never decided", with(undecided, func(v *analysisView) { v.Run.Launch.Autonomous = false }), send},
		{"no run row", with(func(v *analysisView) { v.Run = nil }), send},

		{"analyses off", with(off), wait},
		{"gate shut", with(func(v *analysisView) { v.GateOpen = false }), wait},
		{"no worker slot", with(func(v *analysisView) { v.Room = false }), wait},
		{"another analysis of the card is in flight", with(func(v *analysisView) { v.Held = true }), wait},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextAnalysis(tt.v))
		})
	}
}
