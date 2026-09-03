package boardmerge

import (
	"slices"

	"github.com/mhersson/contextmatrix/internal/board"
)

// applyInvariants re-checks a merged card against the invariants the service
// enforces at write time, so a card produced by a merge always looks like one
// the service could have produced through normal operation. Repairs are
// applied in place on card and audited; if the repaired card still fails
// ValidateCard, the remote side is kept verbatim with an audit entry
// recording the discarded local version.
func applyInvariants(card, theirs *board.Card, project string, c Context) (*board.Card, []Resolution) {
	var res []Resolution

	path := project + "/tasks/" + card.ID + ".md"

	repair := func(detail string) {
		res = append(res, Resolution{Path: path, CardID: card.ID, Rule: RuleInvariantRepair, Detail: detail})
		card.ActivityLog = append(card.ActivityLog, auditEntry(c, RuleInvariantRepair, detail))
	}

	exists := func(id string) bool { return c.CardExists != nil && c.CardExists(project, id) }

	// Mirrors enforceTerminalStateInvariants (internal/service/service_transitions.go):
	// only not_planned releases the claim on a state change. done deliberately
	// keeps it, so the holder's ReleaseCard call can still flush deferred
	// commits and a re-claim by that holder is a heartbeat refresh, not a new
	// agent taking finished work.
	if card.State == board.StateNotPlanned && (card.AssignedAgent != "" || card.LastHeartbeat != nil ||
		card.ClaimedVia != "" || card.ClaimedAt != nil) {
		card.ClearClaim()

		repair("not_planned clears the agent claim")
	}

	if card.Parent != "" && !exists(card.Parent) {
		repair("parent " + card.Parent + " no longer exists; cleared")
		card.Parent = ""
	}

	card.ActivityLog = board.TrimActivityLog(card.ActivityLog)

	if c.Project == nil {
		return card, res // nothing more to check without the project config
	}

	cfg, err := c.Project(project)
	if err != nil || cfg == nil {
		return card, res // nothing more to check without the project config
	}

	switch {
	case card.Parent == "" && card.Type == board.SubtaskType && len(cfg.Types) > 0:
		card.Type = cfg.Types[0]
		repair("subtask without a parent reset to type " + card.Type)
	case card.Parent != "" && card.Type != board.SubtaskType:
		card.Type = board.SubtaskType

		repair("card with a parent forced to subtask type")
	}

	card.DependsOn = keepExisting(card.DependsOn, exists, func(id string) { repair("dropped dangling depends_on " + id) })
	card.Subtasks = keepExisting(card.Subtasks, exists, func(id string) { repair("dropped dangling subtask " + id) })
	card.ActivityLog = board.TrimActivityLog(card.ActivityLog)

	if err := board.NewValidator().ValidateCard(cfg, card); err != nil {
		fallback := *theirs
		fallback.ActivityLog = board.TrimActivityLog(append(slices.Clone(theirs.ActivityLog),
			auditEntry(c, RuleInvariantFallback,
				"merged card invalid ("+err.Error()+"); remote version kept, local changes in commit "+c.OursCommit)))
		res = append(res, Resolution{Path: path, CardID: card.ID, Rule: RuleInvariantFallback, Detail: err.Error()})

		return &fallback, res
	}

	return card, res
}

func keepExisting(ids []string, exists func(string) bool, dropped func(string)) []string {
	if ids == nil {
		return nil
	}

	out := ids[:0:0]

	for _, id := range ids {
		if exists(id) {
			out = append(out, id)
		} else {
			dropped(id)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
