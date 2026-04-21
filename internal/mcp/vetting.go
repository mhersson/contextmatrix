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

// unvettedPlaceholderTitle is substituted for the title of an unvetted
// external card when it would otherwise be rendered to a non-human caller via
// an MCP prompt formatter (which has no agent identity available).
const unvettedPlaceholderTitle = "[unvetted — title redacted]"

// shouldRedact returns true if a non-human caller is reading an unvetted card.
// A card counts as unvetted only when both Vetted=false AND the caller is not
// a human agent (agentID not prefixed with "human:"). An empty agentID is
// treated as "non-human" because MCP prompt handlers do not have identity
// information available — see redactCardForPrompt.
func shouldRedact(card *board.Card, agentID string) bool {
	if card == nil {
		return false
	}

	return !card.Vetted && !strings.HasPrefix(agentID, "human:")
}

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

	if !shouldRedact(card, agentID) {
		return card.Body
	}

	return unvettedBodyPlaceholder
}

// redactCardForAgent returns a shallow copy of card with Body replaced per
// redactUnvettedBody. The original card is left unmodified. Returns nil if
// card is nil. Use this when emitting a card through an MCP read tool — the
// caller's agent_id is propagated so humans get full visibility.
func redactCardForAgent(card *board.Card, agentID string) *board.Card {
	if card == nil {
		return nil
	}

	if !shouldRedact(card, agentID) {
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

// redactCardForPrompt returns a copy of the card with every untrusted-source
// field replaced with a fixed placeholder when the card is unvetted. Used by
// MCP prompts, which do not receive an agent_id argument and always run
// inside an agent context — so an empty agent identity is implied and unvetted
// cards are always redacted before the skill content is rendered.
//
// The redaction covers every field whose content may carry prompt-injection
// payloads: Body, Title, ActivityLog entries, Source.ExternalURL, and the
// free-form Context slice. Structural metadata (ID, State, Type, Priority) is
// preserved — the agent still needs to know the card exists and is blocked
// on vetting.
//
// When the card is safe to render in full (vetted), the original pointer is
// returned unchanged so callers avoid a copy.
func redactCardForPrompt(card *board.Card) *board.Card {
	if !shouldRedact(card, "") {
		return card
	}

	cp := *card // shallow copy; Card contains slices/pointers we handle below
	cp.Body = unvettedBodyPlaceholder
	cp.Title = unvettedPlaceholderTitle
	cp.ActivityLog = nil // log entries can carry injected text as "messages"
	cp.Context = nil     // free-form strings supplied by external importers

	if cp.Source != nil {
		src := *cp.Source
		src.ExternalURL = ""
		cp.Source = &src
	}

	return &cp
}
