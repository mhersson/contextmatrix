package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, tc.want, IsHumanAgentID(tc.in))
		})
	}
}

func TestClaimHeldBy(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		agent    string
		instance string
		held     bool
		foreign  bool
	}{
		{"unclaimed", Card{}, "a", "lap-a", false, false},
		{"private board agent match", Card{AssignedAgent: "a"}, "a", "", true, false},
		{"private board agent mismatch", Card{AssignedAgent: "a"}, "b", "", false, false},
		{"own instance", Card{AssignedAgent: "a", ClaimedVia: "lap-a"}, "a", "lap-a", true, false},
		{"other instance same agent", Card{AssignedAgent: "a", ClaimedVia: "lap-b"}, "a", "lap-a", false, true},
		{"legacy claim honoured anywhere", Card{AssignedAgent: "a"}, "a", "lap-a", true, false},
		{"legacy claim other agent", Card{AssignedAgent: "a"}, "b", "lap-a", false, false},
		{"foreign claim on private board is not foreign", Card{AssignedAgent: "a", ClaimedVia: "lap-b"}, "a", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.held, tt.card.ClaimHeldBy(tt.agent, tt.instance))
			assert.Equal(t, tt.foreign, tt.card.ClaimedElsewhere(tt.instance))
		})
	}
}

func TestClearClaim(t *testing.T) {
	now := time.Now()
	c := Card{AssignedAgent: "a", ClaimedVia: "lap-a", ClaimedAt: &now, LastHeartbeat: &now, ClaimEpoch: 3}
	c.ClearClaim()
	assert.Empty(t, c.AssignedAgent)
	assert.Empty(t, c.ClaimedVia)
	assert.Nil(t, c.ClaimedAt)
	assert.Nil(t, c.LastHeartbeat)
	assert.Equal(t, 3, c.ClaimEpoch, "the epoch is the caller's to bump")
}
