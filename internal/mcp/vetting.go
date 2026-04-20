package mcp

import (
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// unvettedBodyPlaceholder replaces the body of an unvetted external card when
// it is read by a non-human agent. An unvetted card body may contain
// prompt-injection payloads imported from external systems (GitHub issues,
// Jira tickets), so it must not flow into an agent's context until a human
// reviews and flips `vetted: true`.
const unvettedBodyPlaceholder = "[unvetted — human review required before body is exposed to agents]"

// redactUnvettedBody returns the card body as-is for humans or for vetted
// cards. For non-human callers reading an unvetted card, it returns a
// placeholder so prompt-injection payloads do not reach the agent.
//
// Only the body is redacted. ID, Title, State, Priority, Labels, Parent,
// DependsOn, Source, AssignedAgent, etc. remain visible — planning and
// filtering must still work.
func redactUnvettedBody(card *board.Card, agentID string) string {
	if card == nil {
		return ""
	}

	if card.Vetted || strings.HasPrefix(agentID, "human:") {
		return card.Body
	}

	return unvettedBodyPlaceholder
}

// redactCardForAgent returns a shallow copy of card with Body replaced per
// redactUnvettedBody. The original card is left unmodified. Returns nil if
// card is nil. Use this when emitting a card through an MCP read tool.
func redactCardForAgent(card *board.Card, agentID string) *board.Card {
	if card == nil {
		return nil
	}

	if card.Vetted || strings.HasPrefix(agentID, "human:") {
		return card
	}

	redacted := *card
	redacted.Body = unvettedBodyPlaceholder

	return &redacted
}

// redactCardsForAgent applies redactCardForAgent to every card in the slice,
// returning a new slice. The input slice and its elements are not mutated.
// Returns nil on nil input.
func redactCardsForAgent(cards []*board.Card, agentID string) []*board.Card {
	if cards == nil {
		return nil
	}

	out := make([]*board.Card, len(cards))
	for i, c := range cards {
		out[i] = redactCardForAgent(c, agentID)
	}

	return out
}
