package attention

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel"
	"github.com/mhersson/contextmatrix/internal/service"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func parkedCard(id string, entries ...board.ActivityEntry) *board.Card {
	return &board.Card{
		ID: id, Project: "p", Title: "title " + id, State: "in_progress",
		WorkerStatus: "parked", Updated: t0, ActivityLog: entries,
	}
}

func key(id string) CardKey { return CardKey{Project: "p", ID: id} }

func cardSnap(c *board.Card, holds map[CardKey]sentinel.CardHold) Snapshot {
	return Snapshot{
		Cards: map[CardKey]*board.Card{key(c.ID): c},
		Holds: holds,
	}
}

func waitingRun(id, title, entry, startedBy string, at time.Time, entries ...service.PlaybookEntryDetail) *service.PlaybookDetail {
	return &service.PlaybookDetail{
		ID: id, Title: title, Entries: entries,
		Run: &board.PlaybookRun{
			Status: board.RunStatusWaiting, Entry: entry, Reason: "gate " + id,
			StartedBy: startedBy, UpdatedAt: at,
		},
	}
}

func cardEntry(id, card string) service.PlaybookEntryDetail {
	return service.PlaybookEntryDetail{PlaybookEntry: board.PlaybookEntry{
		ID: id, Type: board.EntryTypeCard, Project: "p", Card: card,
	}}
}

func TestMerge_BlueprintItem(t *testing.T) {
	s := Snapshot{Blueprints: []BlueprintWait{
		{ID: "s1", Title: "Plan", Owner: "human:alice", Kind: "gate", Since: t0},
		{ID: "s2", Title: "Other", Owner: "human:bob", Kind: "weird", Since: t0.Add(-time.Hour)},
	}}

	items := Merge(s, "human:alice")
	require.Len(t, items, 2)

	assert.Equal(t, KindBlueprint, items[0].Kind)
	assert.Equal(t, blueprintReason("gate"), items[0].Reason)
	assert.True(t, items[0].Mine)
	assert.Equal(t, blueprintReason("weird"), items[1].Reason)
	assert.False(t, items[1].Mine)
}

func TestMerge_WorkerParkedCard(t *testing.T) {
	c := parkedCard("A-1",
		board.ActivityEntry{Action: "parked", Message: "old", Timestamp: t0.Add(-2 * time.Hour)},
		board.ActivityEntry{Action: "note", Message: "x", Timestamp: t0},
		board.ActivityEntry{Action: "parked", Message: "needs a decision", Timestamp: t0.Add(-time.Hour)},
	)

	items := Merge(cardSnap(c, nil), "human:alice")
	require.Len(t, items, 1)
	assert.Equal(t, KindCard, items[0].Kind)
	assert.Equal(t, "needs a decision", items[0].Reason)
	assert.Equal(t, t0.Add(-time.Hour), items[0].Since)
	assert.False(t, items[0].Mine)
	assert.Equal(t, "p", items[0].Project)

	bare := parkedCard("A-2")
	items = Merge(cardSnap(bare, nil), "human:alice")
	require.Len(t, items, 1)
	assert.Equal(t, bare.Updated, items[0].Since)
}

func TestMerge_SentinelParkReplacesWorkerPark(t *testing.T) {
	c := parkedCard("A-1", board.ActivityEntry{Action: "parked", Message: "w", Timestamp: t0})
	holds := map[CardKey]sentinel.CardHold{key("A-1"): {Reason: "sentinel gave up", Since: t0.Add(time.Minute)}}

	items := Merge(cardSnap(c, holds), "human:alice")
	require.Len(t, items, 1)
	assert.Equal(t, KindSentinel, items[0].Kind)
	assert.Equal(t, "sentinel gave up", items[0].Reason)
	assert.Equal(t, t0.Add(time.Minute), items[0].Since)
	assert.False(t, items[0].Mine)

	c.WorkerStatus = ""
	assert.Len(t, Merge(cardSnap(c, holds), "x"), 1)
}

func TestMerge_HealingCardIsNoItem(t *testing.T) {
	c := parkedCard("A-1")
	holds := map[CardKey]sentinel.CardHold{key("A-1"): {Healing: true}}
	assert.Empty(t, Merge(cardSnap(c, holds), "x"))
}

func TestMerge_TerminalCardIsNoItem(t *testing.T) {
	for _, state := range []string{board.StateDone, board.StateNotPlanned} {
		c := parkedCard("A-1")
		c.State = state
		holds := map[CardKey]sentinel.CardHold{key("A-1"): {Reason: "r", Since: t0}}
		assert.Empty(t, Merge(cardSnap(c, holds), "x"), state)
	}
}

func TestMerge_PlaybookOnItemCardFolds(t *testing.T) {
	s := cardSnap(parkedCard("A-1"), nil)
	s.Playbooks = []*service.PlaybookDetail{
		waitingRun("pb-b", "B", "e1", "human:alice", t0, cardEntry("e1", "A-1")),
		waitingRun("pb-a", "A", "e1", "human:alice", t0, cardEntry("e1", "A-1")),
	}

	items := Merge(s, "human:alice")
	require.Len(t, items, 1)
	assert.Equal(t, KindCard, items[0].Kind)
	assert.Equal(t, &PlaybookRef{ID: "pb-a", Title: "A"}, items[0].Playbook)
}

func TestMerge_PlaybookOnHealingCardIsNoItem(t *testing.T) {
	c := &board.Card{ID: "A-1", Project: "p", State: "in_progress"}
	s := cardSnap(c, map[CardKey]sentinel.CardHold{key("A-1"): {Healing: true}})
	s.Playbooks = []*service.PlaybookDetail{waitingRun("pb", "P", "e1", "u", t0, cardEntry("e1", "A-1"))}
	assert.Empty(t, Merge(s, "u"))
}

func TestMerge_PlaybookOwnItem(t *testing.T) {
	manual := service.PlaybookEntryDetail{PlaybookEntry: board.PlaybookEntry{ID: "m1", Type: board.EntryTypeManual}}
	s := Snapshot{
		Cards: map[CardKey]*board.Card{key("B-1"): {ID: "B-1", Project: "p", State: "todo"}},
		Playbooks: []*service.PlaybookDetail{
			waitingRun("pb-manual", "M", "m1", "human:alice", t0, manual),
			waitingRun("pb-unvetted", "U", "", "human:bob", t0.Add(-time.Hour)),
			waitingRun("pb-card", "C", "e1", "human:alice", t0.Add(-2*time.Hour), cardEntry("e1", "B-1")),
		},
	}

	items := Merge(s, "human:alice")
	require.Len(t, items, 3)

	for _, it := range items {
		assert.Equal(t, KindPlaybook, it.Kind)
		assert.Nil(t, it.Playbook)
		assert.Equal(t, it.ID != "pb-unvetted", it.Mine, it.ID)
		assert.Equal(t, "gate "+it.ID, it.Reason)
	}
}

func TestMerge_NewestFirst(t *testing.T) {
	s := Snapshot{Blueprints: []BlueprintWait{
		{ID: "b", Since: t0}, {ID: "a", Since: t0}, {ID: "old", Since: t0.Add(-time.Hour)}, {ID: "new", Since: t0.Add(time.Hour)},
	}}

	items := Merge(s, "x")
	ids := make([]string, len(items))

	for i, it := range items {
		ids[i] = it.ID
	}

	assert.Equal(t, []string{"new", "a", "b", "old"}, ids)
}
