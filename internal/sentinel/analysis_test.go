package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
