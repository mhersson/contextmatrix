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
