package boardmerge

import (
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

// claimTuple is the set of fields one instance owns as a unit while it holds
// a card: the claim identity, its liveness, and the run state that belongs
// to the run.
type claimTuple struct {
	assignedAgent, claimedVia, workerStatus, state, phase string
	claimedAt, lastHeartbeat                              *time.Time
	epoch                                                 int
}

func tupleOf(c *board.Card) claimTuple {
	return claimTuple{
		assignedAgent: c.AssignedAgent, claimedVia: c.ClaimedVia, workerStatus: c.WorkerStatus,
		state: c.State, phase: c.Phase, claimedAt: c.ClaimedAt, lastHeartbeat: c.LastHeartbeat,
		epoch: c.ClaimEpoch,
	}
}

func (t claimTuple) applyTo(c *board.Card) {
	c.AssignedAgent, c.ClaimedVia, c.WorkerStatus = t.assignedAgent, t.claimedVia, t.workerStatus
	c.State, c.Phase = t.state, t.phase
	c.ClaimedAt, c.LastHeartbeat = t.claimedAt, t.lastHeartbeat
	c.ClaimEpoch = t.epoch
}

// bareStall is a guess about liveness: the card was stalled and nobody has
// picked it up. A completion on the other side is proof of liveness and wins.
func bareStall(t claimTuple) bool {
	return t.state == board.StateStalled && t.assignedAgent == ""
}

// mergeClaim resolves the claim tuple. The side with the higher epoch
// supplies the whole tuple, except that a bare stall never overrides a
// terminal state. At equal epochs a double claim from an unclaimed base goes
// to the earlier claimed_at; otherwise every field merges on its own, with
// the terminal-absorbs rule for state.
func mergeClaim(base, ours, theirs *board.Card, oursLater bool, audit func(rule, field, losing string)) claimTuple {
	b, o, t := tupleOf(base), tupleOf(ours), tupleOf(theirs)

	switch {
	case o.epoch > t.epoch:
		if bareStall(o) && board.IsTerminalState(t.state) {
			audit(RuleTerminalOverStall, "claim", sideLocal)

			return t
		}

		audit(RuleEpochWins, "claim", sideRemote)

		return o
	case t.epoch > o.epoch:
		if bareStall(t) && board.IsTerminalState(o.state) {
			audit(RuleTerminalOverStall, "claim", sideRemote)

			return o
		}

		audit(RuleEpochWins, "claim", sideLocal)

		return t
	}

	doubleClaim := b.assignedAgent == "" && o.assignedAgent != "" && t.assignedAgent != "" &&
		(o.assignedAgent != t.assignedAgent || o.claimedVia != t.claimedVia)
	if doubleClaim {
		if earlier(o.claimedAt, t.claimedAt, oursLater) {
			audit(RuleDoubleClaim, "claim", sideRemote)

			return o
		}

		audit(RuleDoubleClaim, "claim", sideLocal)

		return t
	}

	scalar := func(field, bv, ov, tv string) string {
		return pickLater(bv, ov, tv, oursLater, audit, field)
	}

	return claimTuple{
		state:         mergeState(base, ours, theirs, oursLater, audit),
		assignedAgent: scalar("assigned_agent", b.assignedAgent, o.assignedAgent, t.assignedAgent),
		claimedVia:    scalar("claimed_via", b.claimedVia, o.claimedVia, t.claimedVia),
		workerStatus:  scalar("worker_status", b.workerStatus, o.workerStatus, t.workerStatus),
		phase:         scalar("phase", b.phase, o.phase, t.phase),
		claimedAt:     pickHeartbeat(b.claimedAt, o.claimedAt, t.claimedAt),
		lastHeartbeat: pickHeartbeat(b.lastHeartbeat, o.lastHeartbeat, t.lastHeartbeat),
		epoch:         o.epoch,
	}
}

// earlier reports whether ours claimed first. A side without claimed_at is a
// legacy claim and loses to a dated one; two legacy claims fall back to the
// side that was updated first.
func earlier(ours, theirs *time.Time, oursLater bool) bool {
	switch {
	case ours != nil && theirs != nil:
		return ours.Before(*theirs)
	case ours != nil:
		return true
	case theirs != nil:
		return false
	default:
		return !oursLater
	}
}

// mergeState is the equal-epoch state rule: a terminal state absorbs a
// non-terminal one regardless of updated; otherwise the later update wins.
func mergeState(base, ours, theirs *board.Card, oursLater bool, audit func(rule, field, losing string)) string {
	switch {
	case ours.State == theirs.State:
		return ours.State
	case board.IsTerminalState(ours.State) != board.IsTerminalState(theirs.State):
		winner, loserState, losing := ours.State, theirs.State, sideRemote
		if board.IsTerminalState(theirs.State) {
			winner, loserState, losing = theirs.State, ours.State, sideLocal
		}

		// The overridden side lost something only if it actually moved: a
		// one-sided move into a terminal state overrides nothing.
		if loserState != base.State {
			audit(RuleTerminalWins, "state", losing)
		}

		return winner
	default:
		return pickLater(base.State, ours.State, theirs.State, oursLater, audit, "state")
	}
}
