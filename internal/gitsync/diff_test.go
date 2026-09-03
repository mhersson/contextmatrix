package gitsync

import (
	"fmt"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ts(sec int) time.Time { return time.Date(2026, 9, 3, 12, 0, sec, 0, time.UTC) }

func TestPublishDiff(t *testing.T) {
	bus := events.NewBus()

	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	s := &Syncer{bus: bus}
	before := map[string]cardSnapshot{
		"p/A-1": {Project: "p", ID: "A-1", Updated: ts(1), State: "todo"},
		"p/A-2": {Project: "p", ID: "A-2", Updated: ts(1), State: "todo"},
	}
	after := map[string]cardSnapshot{
		"p/A-1": {Project: "p", ID: "A-1", Updated: ts(2), State: "done"},
		"p/A-3": {Project: "p", ID: "A-3", Updated: ts(2), State: "todo"},
	}

	s.publishDiff(before, after)

	var got []events.Event

	drainEvents(ch, &got)

	types := map[events.EventType]int{}

	for _, e := range got {
		types[e.Type]++

		assert.Equal(t, "sync", e.Data["source"])
		assert.Equal(t, "system", e.Agent)
		assert.Equal(t, "p", e.Project)
		assert.NotEmpty(t, e.CardID)
	}

	assert.Equal(t, 1, types[events.CardStateChanged])
	assert.Equal(t, 1, types[events.CardCreated])
	assert.Equal(t, 1, types[events.CardDeleted])
	assert.Len(t, got, 3)
}

// TestPublishDiff_UpdatedOnlyEmitsCardUpdated pins the branch split: a changed
// Updated stamp with an unchanged state is an update, never a state change.
func TestPublishDiff_UpdatedOnlyEmitsCardUpdated(t *testing.T) {
	bus := events.NewBus()

	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	s := &Syncer{bus: bus}
	before := map[string]cardSnapshot{
		"p/A-1": {Project: "p", ID: "A-1", Updated: ts(1), State: "todo"},
	}
	after := map[string]cardSnapshot{
		"p/A-1": {Project: "p", ID: "A-1", Updated: ts(2), State: "todo"},
	}

	s.publishDiff(before, after)

	var got []events.Event

	drainEvents(ch, &got)

	require.Len(t, got, 1)
	assert.Equal(t, events.CardUpdated, got[0].Type)
	assert.Equal(t, "todo", got[0].Data["new_state"])
}

// TestPublishDiff_UnchangedEmitsNothing keeps a sync that pulled commits
// touching no card silent, so the board is not redrawn for nothing.
func TestPublishDiff_UnchangedEmitsNothing(t *testing.T) {
	bus := events.NewBus()

	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	s := &Syncer{bus: bus}
	same := map[string]cardSnapshot{
		"p/A-1": {Project: "p", ID: "A-1", Updated: ts(1), State: "todo"},
	}

	s.publishDiff(same, same)

	var got []events.Event

	drainEvents(ch, &got)

	assert.Empty(t, got)
}

func TestPublishDiff_LargeDiffEmitsNothing(t *testing.T) {
	bus := events.NewBus()

	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	s := &Syncer{bus: bus}
	after := map[string]cardSnapshot{}

	for i := range maxDiffEvents + 1 {
		id := fmt.Sprintf("A-%d", i)
		after["p/"+id] = cardSnapshot{Project: "p", ID: id}
	}

	s.publishDiff(map[string]cardSnapshot{}, after)

	var got []events.Event

	drainEvents(ch, &got)

	assert.Empty(t, got)
}

func TestLostClaims(t *testing.T) {
	own := func(epoch int) cardSnapshot {
		return cardSnapshot{Project: "p", ID: "A-1", State: "in_progress", AssignedAgent: "agent-A-1", ClaimedVia: "lap-a", ClaimEpoch: epoch}
	}

	tests := []struct {
		name   string
		before cardSnapshot
		after  cardSnapshot
		gone   bool
		want   []claimLoss
	}{
		{
			"taken over at a higher epoch", own(1),
			cardSnapshot{Project: "p", ID: "A-1", State: "in_progress", AssignedAgent: "agent-A-1", ClaimedVia: "lap-b", ClaimEpoch: 2},
			false,
			[]claimLoss{{Project: "p", ID: "A-1", PreviousAgent: "agent-A-1", NewVia: "lap-b", Epoch: 2}},
		},
		{
			"double claim lost at an equal epoch", own(1),
			cardSnapshot{Project: "p", ID: "A-1", State: "in_progress", AssignedAgent: "agent-A-1", ClaimedVia: "lap-b", ClaimEpoch: 1},
			false,
			[]claimLoss{{Project: "p", ID: "A-1", PreviousAgent: "agent-A-1", NewVia: "lap-b", Epoch: 1}},
		},
		{
			"stalled by a peer at a higher epoch", own(1),
			cardSnapshot{Project: "p", ID: "A-1", State: "stalled", ClaimEpoch: 2},
			false,
			[]claimLoss{{Project: "p", ID: "A-1", PreviousAgent: "agent-A-1", Epoch: 2}},
		},
		{"still ours is not a loss", own(1), own(1), false, nil},
		{
			"a completion with an empty tuple is a release, never a takeover", own(1),
			cardSnapshot{Project: "p", ID: "A-1", State: "done", ClaimEpoch: 2},
			false, nil,
		},
		{
			"foreign card was never ours",
			cardSnapshot{Project: "p", ID: "A-1", AssignedAgent: "x", ClaimedVia: "lap-b", ClaimEpoch: 1},
			cardSnapshot{Project: "p", ID: "A-1", State: "stalled", ClaimEpoch: 2},
			false, nil,
		},
		{"deleted card is the reconcile sweep's job", own(1), cardSnapshot{}, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := map[string]cardSnapshot{"p/A-1": tt.before}
			after := map[string]cardSnapshot{}

			if !tt.gone {
				after["p/A-1"] = tt.after
			}

			assert.Equal(t, tt.want, lostClaims("lap-a", before, after))
		})
	}
}

// TestPublishDiff_AtTheCapStillEmits pins the boundary: exactly maxDiffEvents
// changes are published, one more is not.
func TestPublishDiff_AtTheCapStillEmits(t *testing.T) {
	bus := events.NewBus()

	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	s := &Syncer{bus: bus}
	after := map[string]cardSnapshot{}

	for i := range maxDiffEvents {
		id := fmt.Sprintf("A-%d", i)
		after["p/"+id] = cardSnapshot{Project: "p", ID: id}
	}

	s.publishDiff(map[string]cardSnapshot{}, after)

	var got []events.Event

	drainEvents(ch, &got)

	assert.Len(t, got, maxDiffEvents)
}
