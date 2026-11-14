package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

func cardEntry(id, cardID string) service.PlaybookEntryDetail {
	return service.PlaybookEntryDetail{
		PlaybookEntry: board.PlaybookEntry{ID: id, Type: board.EntryTypeCard, Project: "alpha", Card: cardID},
	}
}

// forwardRun is a runnable playbook's run in status on instance, over
// ALPHA-001, ALPHA-020 and ALPHA-021 in that order.
func forwardRun(status, instance string) *service.PlaybookDetail {
	return &service.PlaybookDetail{
		ID: "rollout", Runnable: true,
		Run:     &board.PlaybookRun{Status: status, Instance: instance, Entry: "e1"},
		Entries: []service.PlaybookEntryDetail{cardEntry("e1", "ALPHA-001"), cardEntry("e2", "ALPHA-020"), cardEntry("e3", "ALPHA-021")},
	}
}

func TestForwardTargets(t *testing.T) {
	base := func() (state.Heal, *service.PlaybookDetail, map[string]*board.Card) {
		h := state.Heal{
			Project: "alpha", CardID: "ALPHA-001", SentSiblings: []string{"ALPHA-020", "ALPHA-021"},
			Investigation: &protocol.InvestigateResult{Cause: protocol.DiagnosisCard, ForwardTo: []string{"ALPHA-021", "ALPHA-020"}},
		}
		cards := map[string]*board.Card{
			"ALPHA-020": {ID: "ALPHA-020", Project: "alpha", State: board.StateTodo},
			"ALPHA-021": {ID: "ALPHA-021", Project: "alpha", State: board.StateTodo},
		}

		return h, forwardRun(board.RunStatusRunning, "lap-a"), cards
	}

	tests := []struct {
		name string
		edit func(*state.Heal, *service.PlaybookDetail, map[string]*board.Card)
		want []string
	}{
		{"named, sent and not started, in entry order", nil, []string{"ALPHA-020", "ALPHA-021"}},
		{"not named", func(h *state.Heal, _ *service.PlaybookDetail, _ map[string]*board.Card) {
			h.Investigation.ForwardTo = []string{"ALPHA-020"}
		}, []string{"ALPHA-020"}},
		{"not sent", func(h *state.Heal, _ *service.PlaybookDetail, _ map[string]*board.Card) {
			h.SentSiblings = []string{"ALPHA-021"}
		}, []string{"ALPHA-021"}},
		{"started", func(_ *state.Heal, _ *service.PlaybookDetail, cards map[string]*board.Card) {
			cards["ALPHA-020"].WorkerStatus = "running"
		}, []string{"ALPHA-021"}},
		{"imported and not vetted", func(_ *state.Heal, _ *service.PlaybookDetail, cards map[string]*board.Card) {
			cards["ALPHA-020"].Source = &board.Source{System: "jira", ExternalID: "OPS-1"}
		}, []string{"ALPHA-021"}},
		{"another project's entry", func(_ *state.Heal, pb *service.PlaybookDetail, _ map[string]*board.Card) {
			pb.Entries[2].Project = "beta"
		}, []string{"ALPHA-020"}},
		{"another instance's run", func(_ *state.Heal, pb *service.PlaybookDetail, _ map[string]*board.Card) {
			pb.Run.Instance = "lap-b"
		}, nil},
		{"run stopped", func(_ *state.Heal, pb *service.PlaybookDetail, _ map[string]*board.Card) {
			pb.Run.Status = board.RunStatusStopped
		}, nil},
		{"no investigation", func(h *state.Heal, _ *service.PlaybookDetail, _ map[string]*board.Card) {
			h.Investigation = nil
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, pb, cards := base()
			if tc.edit != nil {
				tc.edit(&h, pb, cards)
			}

			assert.Equal(t, tc.want, forwardTargets(h, pb, cards, "lap-a"))
		})
	}
}

func TestForwardOutcome(t *testing.T) {
	tests := []struct {
		name, key, state, want string
	}{
		{"done", protocol.StopCauseDone, board.StateReview, state.StageForwardPassed},
		{"the card is done", CauseUnknown, board.StateDone, state.StageForwardPassed},
		{"the forwarded fix failed", CauseForwardFixFailed, board.StateInProgress, state.StageForwardFailed},
		{"a usage limit", protocol.StopCauseUsageLimit, board.StateInProgress, ""},
		{"a human stop", CauseHumanStop, board.StateInProgress, ""},
		{"a stall", CauseStalled, board.StateStalled, ""},
		{"the review cap", CauseReviewCap, board.StateInProgress, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, forwardOutcome(tc.key, &board.Card{State: tc.state}))
		})
	}
}

// verifyFix keeps the project's command, which the check did not list as a
// candidate, and retires the healed card's own subtask.
func verifyFix() protocol.CardFix {
	return protocol.CardFix{
		Section:        "Run make generate first.",
		Verify:         &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 900},
		RetireSubtasks: []string{"ALPHA-011"},
	}
}

// forwardEnv stores ALPHA-001's heal at rerun, the card in rollout's run
// active on this instance, with ALPHA-020 and ALPHA-021 its unstarted
// siblings, both sent and named for fix, which the check trial-ran; edits
// change the heal. The project's verify runs make test; ALPHA-021 runs
// go test ./... of its own.
func forwardEnv(t *testing.T, fix protocol.CardFix, edits ...func(*state.Heal)) *dispatchEnv {
	t.Helper()

	e := newPassEnv(t)
	e.cards.projects["alpha"].Verify = &board.VerifyConfig{Command: "make test"}
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(c *board.Card, r *state.Run) {
		c.PlaybookLock, r.Launch.Origin = rolloutLock(board.RunStatusRunning), state.OriginPlaybook
	})
	e.cards.cards = append(e.cards.cards,
		&board.Card{
			ID: "ALPHA-011", Project: "alpha", Title: "Part two", Parent: "ALPHA-001", State: board.StateTodo,
			Updated: epoch.Add(-time.Hour),
		},
		&board.Card{
			ID: "ALPHA-020", Project: "alpha", Title: "Next card", Type: "task", State: board.StateTodo,
			Autonomous: true, Updated: epoch.Add(-time.Hour), Body: "Make the next part pass.\n",
			PlaybookLock: rolloutLock(board.RunStatusRunning),
		},
		&board.Card{
			ID: "ALPHA-021", Project: "alpha", Title: "Last card", Type: "task", State: board.StateTodo,
			Autonomous: true, Updated: epoch.Add(-time.Hour), Body: "Make the last part pass.\n",
			Verify: &board.VerifyConfig{Command: "go test ./..."}, PlaybookLock: rolloutLock(board.RunStatusRunning),
		},
	)
	e.pbs["rollout"] = forwardRun(board.RunStatusRunning, "lap-a")

	named := func(h *state.Heal) {
		h.SentSubtasks, h.SentSiblings = []string{"ALPHA-011"}, []string{"ALPHA-020", "ALPHA-021"}
		h.Investigation.ForwardTo = []string{"ALPHA-020", "ALPHA-021"}
		h.Check = &protocol.CheckResult{Candidates: []string{}, Trial: protocol.TrialFinished}
	}
	e.openHeal(t, "ALPHA-001", state.StageRerun,
		append([]func(*state.Heal){investigated(protocol.DiagnosisCard, &fix), relaunchedAt(epoch.Add(-30*time.Minute), 0), named}, edits...)...)

	return e
}

// forwardRows returns the forward rows on card id.
func forwardRows(e *dispatchEnv, id string) []state.Heal {
	var rows []state.Heal

	for _, h := range e.store.allHeals() {
		if h.Kind == state.HealKindForward && h.CardID == id {
			rows = append(rows, h)
		}
	}

	return rows
}

func patchedIDs(e *dispatchEnv) []string {
	var ids []string
	for _, p := range e.cards.patches {
		ids = append(ids, p.ID)
	}

	return ids
}

func TestForwardRespectsTargetVerify(t *testing.T) {
	e := forwardEnv(t, verifyFix())
	ctx := context.Background()
	target := storedCard(t, e, "ALPHA-020")

	e.rerunEnded(t, "ALPHA-001", protocol.StopCauseDone, "completed")

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageHealed, e.store.heal(1).Stage)
	require.Equal(t, []string{"ALPHA-020"}, patchedIDs(e),
		"ALPHA-021's own command is not a candidate, and no subtask is retired")
	in := e.cards.patches[0].In
	require.NotNil(t, in.ExpectUpdated)
	assert.Equal(t, target.Updated, *in.ExpectUpdated)
	assert.Equal(t, Agent, in.AgentID)

	written := storedCard(t, e, "ALPHA-020")
	assert.Contains(t, sentinelSection(written), "Run make generate first.")
	assert.Equal(t, &board.VerifyConfig{Command: "make test", TimeoutSeconds: 900}, written.Verify)
	assert.Zero(t, e.store.refreshes, "a target has no run row to refresh")

	rows := forwardRows(e, "ALPHA-020")
	require.Len(t, rows, 1)
	row := rows[0]
	assert.Equal(t, state.StageForwarded, row.Stage)
	assert.Equal(t, "ALPHA-001", row.SourceCard)
	assert.Equal(t, e.clk.Now(), row.CreatedAt)
	require.NotNil(t, row.Change)
	assert.Empty(t, row.Change.SectionBefore)
	assert.Contains(t, row.Change.SectionAfter, "Run make generate first.")
	assert.Nil(t, row.Change.VerifyBefore)
	assert.Equal(t, written.Verify, row.Change.VerifyAfter)
	assert.Empty(t, row.Change.Retired)
	assert.Empty(t, forwardRows(e, "ALPHA-021"))

	entries := e.cards.logged["ALPHA-020"]
	require.Len(t, entries, 1, "a forward writes one activity entry on its target")
	assert.Equal(t, Agent, entries[0].Agent)
	assert.Equal(t, activityAction, entries[0].Action)
	assert.Contains(t, entries[0].Message, "ALPHA-001")
	assert.Empty(t, e.cards.logged["ALPHA-021"])
	assert.Empty(t, e.cards.logged["ALPHA-001"], "settling writes no activity entry")
}

func TestForwardOnlyAfterRerunPasses(t *testing.T) {
	tests := []struct {
		name, cause, worker string
		edit                func(*state.Heal)
		stage               string
	}{
		{"the rerun failed", protocol.StopCauseVerifyParked, "failed", func(*state.Heal) {}, state.StageRerunFailed},
		{"healed without a card fix", protocol.StopCauseDone, "completed", func(h *state.Heal) {
			h.Investigation.Cause = protocol.DiagnosisModel
		}, state.StageHealed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := forwardEnv(t, verifyFix(), tc.edit)

			e.rerunEnded(t, "ALPHA-001", tc.cause, tc.worker)

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, tc.stage, e.store.heal(1).Stage)
			assert.Empty(t, e.cards.patches)
			assert.Empty(t, forwardRows(e, "ALPHA-020"))
			assert.Empty(t, forwardRows(e, "ALPHA-021"))
			assert.Empty(t, e.cards.logged["ALPHA-020"])
		})
	}
}

func TestForwardSkipsATargetChangedMeanwhile(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"written since it was read", service.ErrCardChanged},
		{"claimed since it was read", lock.ErrAgentMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := forwardEnv(t, verifyFix())
			e.cards.refuse = map[string]error{"ALPHA-020": tc.err}

			e.rerunEnded(t, "ALPHA-001", protocol.StopCauseDone, "completed")

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, state.StageHealed, e.store.heal(1).Stage)
			assert.Empty(t, forwardRows(e, "ALPHA-020"))
			assert.Empty(t, e.cards.logged["ALPHA-020"])
			assert.True(t, e.store.run("alpha", "ALPHA-001").Decided, "the healed rerun is still decided")
		})
	}
}

// forwarded runs forwardEnv's rerun to done, which forwards the fix to
// ALPHA-020, then ends ALPHA-020's own first run with cause.
func forwarded(t *testing.T, cause, worker string) *dispatchEnv {
	t.Helper()

	e := forwardEnv(t, verifyFix())
	e.rerunEnded(t, "ALPHA-001", protocol.StopCauseDone, "completed")
	require.NoError(t, e.ctl.pass(context.Background()))
	require.Len(t, forwardRows(e, "ALPHA-020"), 1)

	e.rerunEnded(t, "ALPHA-020", cause, worker)

	return e
}

// cardHeals returns the heal rows on card id.
func cardHeals(e *dispatchEnv, id string) []state.Heal {
	var heals []state.Heal

	for _, h := range e.store.allHeals() {
		if h.Kind == state.HealKindHeal && h.CardID == id {
			heals = append(heals, h)
		}
	}

	return heals
}

func TestForwardCircuitBreakerParks(t *testing.T) {
	e := forwarded(t, protocol.StopCauseVerifyParked, "failed")

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageForwardFailed, forwardRows(e, "ALPHA-020")[0].Stage)

	heals := cardHeals(e, "ALPHA-020")
	require.Len(t, heals, 1)
	assert.Equal(t, CauseForwardFixFailed, heals[0].Cause)
	assert.Equal(t, RespPark, heals[0].Response)
	assert.Equal(t, state.StageParked, heals[0].Stage)
	assert.NotEmpty(t, heals[0].Reason)
	assert.True(t, e.store.run("alpha", "ALPHA-020").Decided)
	assert.Empty(t, e.backend.sent, "nothing is investigated")
	assert.Len(t, e.cards.logged["ALPHA-020"], 2, "the forward's entry and the park's")
}

func TestForwardedTargetPasses(t *testing.T) {
	e := forwarded(t, protocol.StopCauseDone, "completed")

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageForwardPassed, forwardRows(e, "ALPHA-020")[0].Stage)
	assert.Empty(t, cardHeals(e, "ALPHA-020"))
	assert.True(t, e.store.run("alpha", "ALPHA-020").Decided)
	assert.Len(t, e.cards.logged["ALPHA-020"], 1, "only the forward's entry")
}

func TestForwardStaysOnTrialThroughAUsageLimit(t *testing.T) {
	e := forwarded(t, protocol.StopCauseUsageLimit, "parked")

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageForwarded, forwardRows(e, "ALPHA-020")[0].Stage)

	heals := cardHeals(e, "ALPHA-020")
	require.Len(t, heals, 1)
	assert.Equal(t, state.StageUsageWait, heals[0].Stage)
}
