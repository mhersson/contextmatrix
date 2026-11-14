package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func TestSiblings(t *testing.T) {
	card := cardRef{"alpha", "ALPHA-002"}
	todo := map[string]*board.Card{
		"ALPHA-001": {ID: "ALPHA-001", State: board.StateTodo},
		"ALPHA-002": {ID: "ALPHA-002", State: board.StateTodo},
		"ALPHA-003": {ID: "ALPHA-003", State: board.StateTodo},
		"ALPHA-004": {ID: "ALPHA-004", State: board.StateTodo, WorkerStatus: "failed"},
		"ALPHA-005": {ID: "ALPHA-005", State: board.StateInProgress},
		"ALPHA-007": {ID: "ALPHA-007", State: board.StateTodo},
		"ALPHA-008": {ID: "ALPHA-008", State: board.StateTodo, Source: &board.Source{System: "jira", ExternalID: "OPS-1"}},
		"ALPHA-009": {ID: "ALPHA-009", State: board.StateTodo, Source: &board.Source{System: "jira", ExternalID: "OPS-2"}, Vetted: true},
	}
	entry := func(id, typ, project, cardID string, complete bool) service.PlaybookEntryDetail {
		return service.PlaybookEntryDetail{
			PlaybookEntry: board.PlaybookEntry{ID: id, Type: typ, Project: project, Card: cardID},
			Complete:      complete,
		}
	}
	run := func(status, instance string) *service.PlaybookDetail {
		return &service.PlaybookDetail{
			Runnable: true,
			Run:      &board.PlaybookRun{Status: status, Instance: instance, StartedAt: time.Now()},
			Entries: []service.PlaybookEntryDetail{
				entry("e1", board.EntryTypeCard, "alpha", "ALPHA-007", false),
				entry("e2", board.EntryTypeCard, "alpha", "ALPHA-002", false), // the card itself
				entry("e3", board.EntryTypeManual, "", "", false),
				entry("e4", board.EntryTypeCard, "beta", "ALPHA-003", false),  // another project
				entry("e5", board.EntryTypeCard, "alpha", "ALPHA-001", true),  // complete
				entry("e6", board.EntryTypeCard, "alpha", "ALPHA-004", false), // a worker ran it
				entry("e7", board.EntryTypeCard, "alpha", "ALPHA-005", false), // not in todo
				entry("e8", board.EntryTypeCard, "alpha", "ALPHA-006", false), // gone
				entry("e9", board.EntryTypeCard, "alpha", "ALPHA-003", false),
				entry("e10", board.EntryTypeCard, "alpha", "ALPHA-008", false), // imported, not vetted
				entry("e11", board.EntryTypeCard, "alpha", "ALPHA-009", false), // imported and vetted
			},
		}
	}

	tests := []struct {
		name string
		pb   *service.PlaybookDetail
		want []string
	}{
		{"no playbook", nil, nil},
		{"running here, in entry order", run(board.RunStatusRunning, "lap-a"), []string{"ALPHA-007", "ALPHA-003", "ALPHA-009"}},
		{"waiting here", run(board.RunStatusWaiting, "lap-a"), []string{"ALPHA-007", "ALPHA-003", "ALPHA-009"}},
		{"stopped", run(board.RunStatusStopped, "lap-a"), nil},
		{"another instance's run", run(board.RunStatusRunning, "lap-b"), nil},
		{"not runnable", func() *service.PlaybookDetail {
			pb := run(board.RunStatusRunning, "lap-a")
			pb.Runnable = false

			return pb
		}(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range siblings(tc.pb, "lap-a", card, todo) {
				got = append(got, c.ID)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}
