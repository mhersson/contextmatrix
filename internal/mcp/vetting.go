package mcp

import (
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// unvettedPlaceholderBody is substituted for the body of an unvetted external
// card when it would otherwise be rendered to a non-human caller.
const unvettedPlaceholderBody = "[unvetted — human review required]"

// unvettedPlaceholderTitle is substituted for the title of an unvetted
// external card when it would otherwise be rendered to a non-human caller.
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

// redactCardForPrompt returns a copy of the card with every untrusted-source
// field replaced with a fixed placeholder when the caller is non-human and
// the card is unvetted. Used by MCP prompts which do not receive an agent_id
// argument — for those prompts, the agentID is passed as "" so unvetted cards
// are always redacted before the skill content is rendered.
//
// The redaction covers every field whose content may carry prompt-injection
// payloads: Body, Title, ActivityLog entries, Source.ExternalURL,
// Source.System (external label may be injection-crafted), and the free-form
// Context slice. Structural metadata (ID, State, Type, Priority) is
// preserved — the agent still needs to know the card exists and is blocked
// on vetting.
//
// When the card is safe to render in full (vetted, or caller is human),
// the original pointer is returned unchanged so callers avoid a copy.
func redactCardForPrompt(card *board.Card, agentID string) *board.Card {
	if !shouldRedact(card, agentID) {
		return card
	}

	cp := *card // shallow copy; Card contains slices/pointers we handle below
	cp.Body = unvettedPlaceholderBody
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
