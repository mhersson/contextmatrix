package gitsync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// maxDiffEvents bounds how many per-card events one shared sync publishes.
// Above it the diff is dropped and subscribers fall back to the refetch the
// sync.completed event triggers, which keeps a big pull from overrunning the
// 64-slot subscriber buffers in internal/events.
const maxDiffEvents = 16

// cardSnapshot is the slice of a card the sync diff compares: enough to tell
// created from deleted from moved from edited, and nothing else - plus the
// claim tuple lostClaims needs to tell a takeover from a release.
type cardSnapshot struct {
	Project       string
	ID            string
	Updated       time.Time
	State         string
	AssignedAgent string
	ClaimedVia    string
	ClaimEpoch    int
}

// snapshotCards captures every card the store currently holds, keyed by
// project and card ID. Called on either side of the index reload a pull
// triggers, so the two maps describe the board before and after.
func (s *Syncer) snapshotCards(ctx context.Context) (map[string]cardSnapshot, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	out := map[string]cardSnapshot{}

	for _, p := range projects {
		cards, err := s.store.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			return nil, fmt.Errorf("list cards %s: %w", p.Name, err)
		}

		for _, c := range cards {
			out[p.Name+"/"+c.ID] = cardSnapshot{
				Project:       p.Name,
				ID:            c.ID,
				Updated:       c.Updated,
				State:         c.State,
				AssignedAgent: c.AssignedAgent,
				ClaimedVia:    c.ClaimedVia,
				ClaimEpoch:    c.ClaimEpoch,
			}
		}
	}

	return out, nil
}

// publishDiff turns the difference between two snapshots into per-card events
// so open boards update in place instead of refetching. Every event carries
// source "sync" so subscribers can tell a peer's change from a local one.
func (s *Syncer) publishDiff(before, after map[string]cardSnapshot) {
	var evts []events.Event

	now := time.Now()

	mk := func(typ events.EventType, c cardSnapshot, data map[string]any) {
		data["source"] = "sync"

		evts = append(evts, events.Event{
			Type:      typ,
			Project:   c.Project,
			CardID:    c.ID,
			Agent:     "system",
			Timestamp: now,
			Data:      data,
		})
	}

	for k, a := range after {
		b, ok := before[k]

		switch {
		case !ok:
			mk(events.CardCreated, a, map[string]any{"new_state": a.State})
		case a.State != b.State:
			mk(events.CardStateChanged, a, map[string]any{"old_state": b.State, "new_state": a.State})
		case !a.Updated.Equal(b.Updated):
			mk(events.CardUpdated, a, map[string]any{"old_state": b.State, "new_state": a.State})
		}
	}

	for k, b := range before {
		if _, ok := after[k]; !ok {
			mk(events.CardDeleted, b, map[string]any{"old_state": b.State})
		}
	}

	// Nothing changed, or too much did: the sync.completed refetch covers
	// the large case and an empty diff needs no event at all.
	if len(evts) == 0 || len(evts) > maxDiffEvents {
		return
	}

	for _, e := range evts {
		s.bus.Publish(e)
	}
}

// claimLoss is a claim this instance held before a pull and no longer holds
// after it, because a peer moved the card on at a higher epoch.
type claimLoss struct {
	Project, ID, PreviousAgent, NewVia string
	Epoch                              int
}

// lostClaims lists the cards a pull took out of this instance's hands. A
// deleted card is left to the reconcile sweep, which kills containers of
// cards that no longer exist. Sorted by key for deterministic events.
func lostClaims(instance string, before, after map[string]cardSnapshot) []claimLoss {
	if instance == "" {
		return nil
	}

	var out []claimLoss

	for _, k := range slices.Sorted(maps.Keys(before)) {
		b := before[k]
		if b.AssignedAgent == "" || b.ClaimedVia != instance {
			continue
		}

		a, ok := after[k]
		if !ok {
			continue
		}

		if a.AssignedAgent != "" && a.ClaimedVia == instance {
			continue // still ours
		}

		// Another instance in claimed_via is a takeover at any epoch: the
		// resolver leaves a double claim at the epoch both sides wrote. An
		// empty tuple is a loss only at a higher epoch and outside a
		// terminal state; a completion with an empty tuple is a release.
		takenOver := a.AssignedAgent != "" && a.ClaimedVia != "" && a.ClaimedVia != instance
		cleared := a.AssignedAgent == "" && a.ClaimEpoch > b.ClaimEpoch && !board.IsTerminalState(a.State)

		if takenOver || cleared {
			out = append(out, claimLoss{Project: b.Project, ID: b.ID, PreviousAgent: b.AssignedAgent, NewVia: a.ClaimedVia, Epoch: a.ClaimEpoch})
		}
	}

	return out
}
