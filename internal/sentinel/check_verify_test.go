package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckVerify(t *testing.T) {
	project := &board.VerifyConfig{Command: "make test", TimeoutSeconds: 600}
	finished := &protocol.CheckResult{Trial: protocol.TrialFinished}

	tests := []struct {
		name  string
		card  *board.VerifyConfig
		fix   *protocol.VerifyConfig
		check *protocol.CheckResult
		want  []string
	}{
		{"no verify in the fix", nil, nil, nil, nil},
		{
			"unchanged command, spacing and timeout", nil,
			&protocol.VerifyConfig{Command: "make  test", TimeoutSeconds: 900}, finished, nil,
		},
		{
			"human command kept, timeout raised",
			&board.VerifyConfig{Command: "make lint && make test"},
			&protocol.VerifyConfig{Command: "make lint && make test", TimeoutSeconds: 900}, finished, nil,
		},
		{
			"changed to a candidate", nil,
			&protocol.VerifyConfig{Command: "go test ./..."},
			&protocol.CheckResult{Trial: protocol.TrialFinished, Candidates: []string{"go test ./..."}}, nil,
		},
		{
			"changed to a non-candidate", nil,
			&protocol.VerifyConfig{Command: "go test ./..."}, finished,
			[]string{"command not declared by the repo"},
		},
		{
			"trial rejected", nil,
			&protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 1},
			&protocol.CheckResult{Trial: protocol.TrialRejected},
			[]string{"trial run: rejected"},
		},
		{
			"trial toolchain", nil,
			&protocol.VerifyConfig{Command: "make test"},
			&protocol.CheckResult{Trial: protocol.TrialToolchain},
			[]string{"trial run: toolchain"},
		},
		{
			"env removed",
			&board.VerifyConfig{Env: []string{"A", "B"}},
			&protocol.VerifyConfig{Env: []string{"A"}}, finished, nil,
		},
		{
			"env added",
			&board.VerifyConfig{Env: []string{"A", "B"}},
			&protocol.VerifyConfig{Env: []string{"A", "C"}}, finished,
			[]string{"adds env C"},
		},
		{
			"command falls back to the project command", &board.VerifyConfig{Command: "make lint"},
			&protocol.VerifyConfig{TimeoutSeconds: 5}, finished,
			[]string{"command not declared by the repo"},
		},
		{
			"not trial-run", nil,
			&protocol.VerifyConfig{TimeoutSeconds: 900}, nil,
			[]string{"verify not trial-run"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkVerify(fixContext{
				Card:    &board.Card{Verify: tt.card},
				Project: project,
				Fix:     protocol.CardFix{Verify: tt.fix},
				Check:   tt.check,
			})
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("empty env list does not clear the project env", func(t *testing.T) {
		got := checkVerify(fixContext{
			Card:    &board.Card{Verify: &board.VerifyConfig{Env: []string{"A"}}},
			Project: &board.VerifyConfig{Env: []string{"A", "S"}},
			Fix:     protocol.CardFix{Verify: &protocol.VerifyConfig{Env: []string{}}},
			Check:   finished,
		})
		assert.Equal(t, []string{"adds env S"}, got)
	})

	t.Run("env added when only the project supplies env", func(t *testing.T) {
		got := checkVerify(fixContext{
			Card:    &board.Card{},
			Project: &board.VerifyConfig{Command: "make test"},
			Fix:     protocol.CardFix{Verify: &protocol.VerifyConfig{Env: []string{"S"}}},
			Check:   finished,
		})
		assert.Equal(t, []string{"adds env S"}, got)
	})

	t.Run("nothing declared anywhere", func(t *testing.T) {
		for _, fix := range []*protocol.VerifyConfig{
			{Env: []string{}},
			{TimeoutSeconds: 30},
			{Command: "go test ./..."},
		} {
			assert.NotPanics(t, func() {
				checkVerify(fixContext{Card: &board.Card{}, Fix: protocol.CardFix{Verify: fix}, Check: finished})
			})
		}
	})

	t.Run("removes the command", func(t *testing.T) {
		got := checkVerify(fixContext{
			Card:  &board.Card{Verify: &board.VerifyConfig{Command: "make lint"}},
			Fix:   protocol.CardFix{Verify: &protocol.VerifyConfig{TimeoutSeconds: 5}},
			Check: finished,
		})
		assert.Equal(t, []string{"removes the verify command"}, got)
	})
}
