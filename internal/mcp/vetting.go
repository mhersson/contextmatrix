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
// external card when it would otherwise reach a non-human caller: MCP prompt
// formatters (no agent identity), the playbook tools and the card tools.
const unvettedPlaceholderTitle = "[unvetted - title redacted]"

// shouldRedact returns true when a non-human caller reads a card that still
// needs vetting (card.NeedsVetting: imported, not yet vetted). Internal cards
// without a source are never redacted. An empty or bare "human:" agentID counts
// as non-human, as MCP prompt handlers carry no identity (see
// redactCardForPrompt).
func shouldRedact(card *board.Card, agentID string) bool {
	if card == nil {
		return false
	}

	return card.NeedsVetting() && !board.IsHumanAgentID(agentID)
}

// redactUnvettedBody returns the card body as-is for humans or for vetted
// cards. For non-human callers reading an unvetted card, it returns a
// placeholder so prompt-injection payloads do not reach the agent.
//
// The body is redacted here and the title by redactCardForAgent and
// summarizeCardForAgent. ID, State, Priority, Labels, Parent, DependsOn,
// Source, Vetted, AssignedAgent, etc. remain visible - planning and filtering
// must still work.
func redactUnvettedBody(card *board.Card, agentID string) string {
	if card == nil {
		return ""
	}

	if !shouldRedact(card, agentID) {
		return card.Body
	}

	return unvettedBodyPlaceholder
}

// redactCardForAgent returns a redacted copy of card for a non-human caller
// reading an unvetted card (see redactUntrustedFields); the original is left
// unmodified. Returns nil if card is nil. Used by the card-returning read tools
// (get_card, get_task_context); summary-returning tools use
// summarizeCardForAgent. Humans and vetted cards get the card unchanged.
func redactCardForAgent(card *board.Card, agentID string) *board.Card {
	if card == nil {
		return nil
	}

	if !shouldRedact(card, agentID) {
		return card
	}

	return redactUntrustedFields(card)
}

// redactUntrustedFields returns a copy of card with every field that can carry
// importer-supplied text replaced or cleared: Body, Title, BranchName (derived
// from the title), ActivityLog, Context and Source.ExternalURL. Structural
// metadata (ID, State, Type, Priority, ...) is preserved. card and its Source
// are never mutated. Shared by redactCardForAgent and redactCardForPrompt so
// the field list cannot drift.
func redactUntrustedFields(card *board.Card) *board.Card {
	cp := *card // shallow copy; slices and pointers are replaced below
	cp.Body = unvettedBodyPlaceholder
	cp.Title = unvettedPlaceholderTitle
	cp.BranchName = ""
	cp.ActivityLog = nil
	cp.Context = nil

	cp.Source = withoutExternalURL(cp.Source)

	return &cp
}

// withoutExternalURL returns a copy of src with ExternalURL cleared; src is
// never mutated. Part of the shared redaction field list.
func withoutExternalURL(src *board.Source) *board.Source {
	if src == nil {
		return nil
	}

	cp := *src
	cp.ExternalURL = ""

	return &cp
}

// summarizeCardForAgent is summarizeCard with the title replaced by the
// placeholder and Context and Source.ExternalURL cleared when shouldRedact
// applies. The summary is a fresh struct and Source is copied, so the source
// card is never mutated.
func summarizeCardForAgent(card *board.Card, agentID string) *CardSummary {
	s := summarizeCard(card)
	if s != nil && shouldRedact(card, agentID) {
		s.Title = unvettedPlaceholderTitle
		s.BranchName = "" // the branch slug is derived from the title
		s.Context = nil
		s.Source = withoutExternalURL(s.Source)
	}

	return s
}

// summarizeCardsForAgent applies summarizeCardForAgent to each card and keeps
// nil vs empty like summarizeCards.
func summarizeCardsForAgent(cards []*board.Card, agentID string) []*CardSummary {
	if cards == nil {
		return nil
	}

	out := make([]*CardSummary, len(cards))
	for i, c := range cards {
		out[i] = summarizeCardForAgent(c, agentID)
	}

	return out
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

	return redactUntrustedFields(card)
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
