package board_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestIsHumanAgentID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"bare prefix", "human:", false},
		{"missing colon", "human", false},
		{"alice", "human:alice", true},
		{"web ID", "human:web-12345678", true},
		{"non-human agent", "agent:foo", false},
		{"wrong placement", ":human:alice", false},
		{"prefix only with whitespace suffix", "human: ", true},
		{"uppercase prefix not accepted", "Human:alice", false},
		{"worker agent", "worker-7", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, board.IsHumanAgentID(tc.in))
		})
	}
}
