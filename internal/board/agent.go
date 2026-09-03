package board

import "strings"

// HumanAgentIDPrefix is the prefix that marks an agent ID as belonging to a
// human user (e.g. "human:alice", "human:web-12345678").
const HumanAgentIDPrefix = "human:"

// IsHumanAgentID reports whether id is a non-empty human-prefixed agent ID.
// The check is intentionally weak per the trust model - it enforces a workflow
// contract ("only humans may invoke this"), not a security boundary. The
// non-empty-suffix requirement keeps audit values meaningful: a bare "human:"
// token would otherwise pass and persist as a useless owner string.
func IsHumanAgentID(id string) bool {
	return strings.HasPrefix(id, HumanAgentIDPrefix) && len(id) > len(HumanAgentIDPrefix)
}

// ClaimHeldBy reports whether agentID holds the card's claim as seen from
// instance. On a private board (empty instance) the agent ID alone decides.
// On a shared board the claim must also have been granted by this instance,
// because the agent backend derives its agent ID from the card ID, and two
// instances running one card present the same agent. A claim with no
// claimed_via predates shared boards and is honoured by any instance.
func (c *Card) ClaimHeldBy(agentID, instance string) bool {
	if c.AssignedAgent == "" || c.AssignedAgent != agentID {
		return false
	}

	return instance == "" || c.ClaimedVia == "" || c.ClaimedVia == instance
}

// ClaimedElsewhere reports whether another instance granted the card's
// current claim. Always false on a private board.
func (c *Card) ClaimedElsewhere(instance string) bool {
	return instance != "" && c.AssignedAgent != "" && c.ClaimedVia != "" && c.ClaimedVia != instance
}

// ClearClaim drops the claim tuple. The epoch is left for the caller, which
// knows whether the change must win a merge.
func (c *Card) ClearClaim() {
	c.AssignedAgent, c.ClaimedVia = "", ""
	c.ClaimedAt, c.LastHeartbeat = nil, nil
}
