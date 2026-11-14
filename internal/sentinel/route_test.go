package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

const longText = "the config loader reads every file twice because the cache key ignores the path"

func TestRouteGuidance(t *testing.T) {
	finding := state.Finding{ID: 9, Kind: "guidance", Text: longText}
	other := func(id int64, st string) state.Finding {
		return state.Finding{ID: id, Kind: "guidance", Text: longText, State: st}
	}

	tests := []struct {
		name       string
		f          state.Finding
		earlier    []state.Finding
		secrets    []string
		wantState  string
		wantReason string
	}{
		{"distinct", finding, []state.Finding{{ID: 1, Text: "always run make fmt before committing a change", State: state.FindingPending}}, nil, state.FindingPending, ""},
		{"no earlier", finding, nil, nil, state.FindingPending, ""},
		{"secret", state.Finding{Kind: "guidance", Text: "use token hunter2hunter2 here"}, nil, []string{"hunter2hunter2"}, state.FindingDropped, "holds a secret"},
		{"secret in evidence", state.Finding{Kind: "guidance", Text: "x", Evidence: "hunter2hunter2"}, nil, []string{"hunter2hunter2"}, state.FindingDropped, "holds a secret"},
		{"like a pending one", finding, []state.Finding{other(3, state.FindingPending)}, nil, state.FindingDuplicate, "like finding 3"},
		{"like a proposed one", finding, []state.Finding{other(4, state.FindingProposed)}, nil, state.FindingDuplicate, "like finding 4"},
		{"like a merged one", finding, []state.Finding{other(5, state.FindingMerged)}, nil, state.FindingDuplicate, "like finding 5"},
		{"like a rejected one", finding, []state.Finding{other(6, state.FindingRejected)}, nil, state.FindingDuplicate, "like finding 6"},
		{"like only a duplicate", finding, []state.Finding{other(7, state.FindingDuplicate)}, nil, state.FindingPending, ""},
		{"like only a dropped one", finding, []state.Finding{other(8, state.FindingDropped)}, nil, state.FindingPending, ""},
		{
			"a guidance block marker",
			state.Finding{Kind: "guidance", Text: "Keep <!-- sentinel:alpha:end --> last."},
			nil, nil, state.FindingDropped, "holds a guidance block marker",
		},
		{
			"a marker beats a duplicate",
			state.Finding{Kind: "guidance", Text: longText + " <!-- sentinel:"},
			[]state.Finding{other(3, state.FindingPending)},
			nil, state.FindingDropped, "holds a guidance block marker",
		},
		{"an ordinary comment", state.Finding{Kind: "guidance", Text: "Keep <!-- generated --> blocks."}, nil, nil, state.FindingPending, ""},
		{"the marker matches exactly", state.Finding{Kind: "guidance", Text: "<!-- Sentinel: x -->"}, nil, nil, state.FindingPending, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, gotReason := routeGuidance(tt.f, tt.earlier, tt.secrets)
			assert.Equal(t, tt.wantState, gotState)
			assert.Equal(t, tt.wantReason, gotReason)
		})
	}
}

// frictionCard is a labelled card; body defaults to longText.
func frictionCard(id, title, st string, body ...string) *board.Card {
	text := longText
	if len(body) > 0 {
		text = body[0]
	}

	return &board.Card{ID: id, Title: title, State: st, Labels: []string{frictionLabel}, Body: text + "\n"}
}

func TestRouteCode(t *testing.T) {
	finding := state.Finding{Kind: "code", Title: "Cache the config loader", Text: longText, Evidence: "ev"}

	tests := []struct {
		name       string
		f          state.Finding
		cards      []*board.Card
		secrets    []string
		today      int
		wantState  string
		wantReason string
	}{
		{"free", finding, nil, nil, 0, state.FindingCarded, ""},
		{"like an open card", finding, []*board.Card{frictionCard("P-001", "Something else entirely", board.StateTodo)}, nil, 0, state.FindingDuplicate, "like card P-001"},
		{
			"equal title",
			state.Finding{Kind: "code", Title: "cache  the CONFIG loader!", Text: "different words altogether here"},
			[]*board.Card{frictionCard("P-002", "Cache the config loader", board.StateTodo)},
			nil, 0, state.FindingDuplicate, "like card P-002",
		},
		{"like a not_planned card", finding, []*board.Card{frictionCard("P-003", "Cache the config loader", board.StateNotPlanned)}, nil, 0, state.FindingDuplicate, "like card P-003"},
		{"like a done card", finding, []*board.Card{frictionCard("P-004", "Cache the config loader", board.StateDone)}, nil, 0, state.FindingCarded, ""},
		{"distinct card", finding, []*board.Card{frictionCard("P-005", "Unrelated", board.StateTodo, "the board column order resets whenever a user drags a card across lanes")}, nil, 0, state.FindingCarded, ""},
		{"limit reached", finding, nil, nil, 5, state.FindingQueued, "today's friction-card limit reached"},
		{"one under the limit", finding, nil, nil, 4, state.FindingCarded, ""},
		{"secret beats the limit", state.Finding{Kind: "code", Text: "hunter2hunter2"}, nil, []string{"hunter2hunter2"}, 5, state.FindingDropped, "holds a secret"},
		{"duplicate beats the limit", finding, []*board.Card{frictionCard("P-006", "Cache the config loader", board.StateTodo)}, nil, 5, state.FindingDuplicate, "like card P-006"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, gotReason := routeCode(tt.f, tt.cards, tt.secrets, tt.today)
			assert.Equal(t, tt.wantState, gotState)
			assert.Equal(t, tt.wantReason, gotReason)
		})
	}
}

func TestFrictionCardInput(t *testing.T) {
	long := string(make([]rune, 0))
	for range 250 {
		long += "x"
	}

	f := state.Finding{Kind: "code", CardID: "ALPHA-007", Title: "Split the loader", Text: "body text", Evidence: "it failed"}

	in := frictionCardInput(f, &board.ProjectConfig{Types: []string{"bug", "chore"}, Priorities: []string{"low", "high"}})
	assert.Equal(t, "Split the loader", in.Title)
	assert.Equal(t, "bug", in.Type)
	assert.Equal(t, "low", in.Priority)
	assert.Equal(t, []string{frictionLabel}, in.Labels)

	if assert.NotNil(t, in.Autonomous) {
		assert.False(t, *in.Autonomous)
	}

	assert.Equal(t, "body text\n\n## Evidence\n\nit failed", in.Body)

	if assert.NotNil(t, in.Log) {
		assert.Equal(t, Agent, in.Log.Agent)
		assert.Equal(t, activityAction, in.Log.Action)
		assert.Equal(t, "Sentinel created this card from the friction a run of ALPHA-007 showed", in.Log.Message)
	}

	listed := frictionCardInput(f, &board.ProjectConfig{Types: []string{"bug", "task"}, Priorities: []string{"low", "medium"}})
	assert.Equal(t, "task", listed.Type)
	assert.Equal(t, "medium", listed.Priority)

	untitled := frictionCardInput(state.Finding{Text: long + "\nsecond line"}, &board.ProjectConfig{})
	assert.Len(t, []rune(untitled.Title), 200)
	assert.Equal(t, "task", untitled.Type, "a project listing no types falls back to task")
}
