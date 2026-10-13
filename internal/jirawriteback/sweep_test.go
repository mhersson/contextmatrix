package jirawriteback

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestSweep_EnqueuesRecentMissedWrites(t *testing.T) {
	recent := testStart.Add(-time.Hour)
	edgeIn := testStart.Add(-(24*time.Hour - time.Minute))
	edgeOut := testStart.Add(-(24*time.Hour + time.Minute))

	cards := newFakeCards(map[string]*board.Card{
		"api/API-001": jiraCard("API-001", "PROJ-1", board.StateInProgress, recent),
		"api/API-002": jiraCard("API-002", "PROJ-2", board.StateDone, recent),
		"api/API-003": jiraCard("API-003", "PROJ-3", board.StateReview, edgeIn),
		"api/API-004": jiraCard("API-004", "PROJ-4", board.StateInProgress, edgeOut),
		"api/API-005": jiraCard("API-005", "PROJ-5", board.StateTodo, recent),
		"api/API-006": jiraCard("API-006", "PROJ-6", board.StateNotPlanned, recent),
		"web/WEB-001": jiraCard("WEB-001", "PROJ-7", board.StateStalled, recent),
		"web/WEB-003": jiraCard("WEB-003", "PROJ-8", "backlog", recent),
		"web/WEB-004": jiraCard("WEB-004", "PROJ-9", "qa", recent),
		"web/WEB-002": {ID: "WEB-002", State: board.StateDone, Updated: recent},
	})

	w, store, _ := newTestWriteback(t, cards, newFakeJira(st("To Do", "new"), simpleWorkflow()))

	w.sweep(t.Context())

	want := map[string]string{
		"api/API-001": KindStarted,
		"api/API-002": KindDone,
		"api/API-003": KindStarted,
		"web/WEB-001": KindStarted,
	}

	for key := range cards.cards {
		project, id, _ := strings.Cut(key, "/")

		for _, kind := range []string{KindStarted, KindDone} {
			_, ok := rowState(t, store, project, id, kind)
			assert.Equal(t, want[key] == kind, ok, "%s %s", key, kind)
		}
	}

	// A second sweep adds nothing and touches nothing.
	before, _ := rowState(t, store, "api", "API-001", KindStarted)
	w.sweep(t.Context())

	after, _ := rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, before, after)
}
