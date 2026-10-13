package mcp

import (
	"slices"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// unvettedBodyPlaceholder replaces the body of an unvetted external card when
// it is read by a non-human agent. An unvetted card body may contain
// prompt-injection payloads imported from external systems (GitHub issues,
// Jira tickets), so it must not flow into an agent's context until a human
// reviews and flips `vetted: true`.
const unvettedBodyPlaceholder = "[unvetted - human review required before body is exposed to agents]"

// unvettedPlaceholderTitle is substituted for the title of an unvetted
// external card when it would otherwise be rendered to a non-human caller via
// an MCP prompt formatter (which has no agent identity available).
const unvettedPlaceholderTitle = "[unvetted - title redacted]"

// shouldRedact returns true if a non-human caller is reading an unvetted card.
// A card counts as unvetted only when both Vetted=false AND the caller is not
// a human agent (agentID does not match board.IsHumanAgentID). An empty
// agentID - and a bare "human:" with no suffix - both count as "non-human"
// because MCP prompt handlers do not have identity information available -
// see redactCardForPrompt.
func shouldRedact(card *board.Card, agentID string) bool {
	if card == nil {
		return false
	}

	return !card.Vetted && !board.IsHumanAgentID(agentID)
}

// redactUnvettedBody returns the card body as-is for humans or for vetted
// cards. For non-human callers reading an unvetted card, it returns a
// placeholder so prompt-injection payloads do not reach the agent.
//
// Only the body is redacted. ID, Title, State, Priority, Labels, Parent,
// DependsOn, Source, AssignedAgent, etc. remain visible - planning and
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
// card is nil. Only the body-carrying read tools (get_card, get_task_context)
// need this - every other card-bearing tool returns a CardSummary, which
// structurally cannot carry a body. The caller's agent_id is propagated so
// humans get full visibility.
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

// redactCardForPrompt returns a copy of the card with every untrusted-source
// field replaced with a fixed placeholder when the card is unvetted. Used by
// MCP prompts, which do not receive an agent_id argument and always run
// inside an agent context - so an empty agent identity is implied and unvetted
// cards are always redacted before the skill content is rendered.
//
// The redaction covers every field whose content may carry prompt-injection
// payloads: Body, Title, ActivityLog entries, Source.ExternalURL, and the
// free-form Context slice. Structural metadata (ID, State, Type, Priority) is
// preserved - the agent still needs to know the card exists and is blocked
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

// redactPlaybookDetail returns a copy of d with the title of every unvetted
// card entry replaced by the placeholder for a non-human caller. d may be
// shared with the service and is never mutated.
func redactPlaybookDetail(d *service.PlaybookDetail, agentID string) *service.PlaybookDetail {
	if d == nil || board.IsHumanAgentID(agentID) {
		return d
	}

	cp := *d
	cp.Entries = slices.Clone(d.Entries)

	for i := range cp.Entries {
		if cp.Entries[i].CardUnvetted {
			cp.Entries[i].CardTitle = unvettedPlaceholderTitle
		}
	}

	return &cp
}

// redactPlaybookSummary is the summary counterpart of redactPlaybookDetail;
// it replaces an unvetted frontier title on a copy of the Next pointer.
func redactPlaybookSummary(s service.PlaybookSummary, agentID string) service.PlaybookSummary {
	if s.Next == nil || !s.Next.Unvetted || board.IsHumanAgentID(agentID) {
		return s
	}

	next := *s.Next
	next.Title = unvettedPlaceholderTitle
	s.Next = &next

	return s
}
