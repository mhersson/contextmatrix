package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

var runStarted = epoch.Add(-time.Hour)

// newPassEnv is a dispatch env whose agent has room for one run kind.
func newPassEnv(t *testing.T) *dispatchEnv {
	t.Helper()

	e := newDispatchEnv(t)
	e.backend.health = protocol.HealthResponse{OK: true, MaxConcurrent: 2}
	e.cards.projects["alpha"] = &board.ProjectConfig{Name: "alpha"}

	return e
}

// stopped stores card id in progress, its worker failed, and its autonomous
// run stopped with cause by a terminal callback 20 minutes ago; edits change
// both before the launch snapshot is taken.
func (e *dispatchEnv) stopped(id, cause string, edits ...func(*board.Card, *state.Run)) (*board.Card, state.Run) {
	card := &board.Card{
		ID: id, Project: "alpha", Title: "Fix the build", Type: "task", State: board.StateInProgress,
		WorkerStatus: "failed", Autonomous: true, Updated: epoch.Add(-20 * time.Minute), Body: "Make the build pass.\n",
	}
	at := epoch.Add(-20 * time.Minute)
	run := state.Run{
		Project: "alpha", CardID: id, Mode: "endpoint", StartedAt: runStarted, StoppedAt: &at,
		Launch: state.LaunchFacts{Autonomous: true, Origin: state.OriginREST}, RunID: "run-" + id, StopCause: cause,
	}

	for _, edit := range edits {
		edit(card, &run)
	}

	run.Launch.Snapshot = board.TakeEditSnapshot(card)
	e.cards.cards = append(e.cards.cards, card)
	e.store.putRun(run)

	return card, run
}

// openHeal stores a heal for card id's stopped run at stage, edited by edits,
// and marks the run decided as opening it does.
func (e *dispatchEnv) openHeal(t *testing.T, id, stage string, edits ...func(*state.Heal)) state.Heal {
	t.Helper()

	h := state.Heal{
		ID: int64(len(e.store.allHeals()) + 1), Kind: state.HealKindHeal, Project: "alpha", CardID: id, RunStarted: runStarted,
		RunID: "run-" + id, Cause: protocol.StopCauseVerifyParked, Response: RespInvestigateFix, Stage: stage,
		CreatedAt: epoch.Add(-15 * time.Minute), UpdatedAt: epoch.Add(-15 * time.Minute),
	}

	for _, edit := range edits {
		edit(&h)
	}

	e.store.putHeal(h)
	require.NoError(t, e.store.MarkRunDecided(context.Background(), "alpha", id, runStarted))

	return h
}

func dispatchedAgo(kind string, age time.Duration) func(*state.Heal) {
	return func(h *state.Heal) {
		at := epoch.Add(-age)
		h.Dispatch = state.Dispatch{Kind: kind, Mode: "endpoint", CeilingUSD: 2, At: &at}
	}
}

func investigated(cause string, fix *protocol.CardFix) func(*state.Heal) {
	return func(h *state.Heal) {
		dispatchedAgo(protocol.RunKindInvestigate, 5*time.Minute)(h)
		h.Dispatch.AnsweredRunID = "rk-1"
		h.Investigation = &protocol.InvestigateResult{Cause: cause, Diagnosis: "the verify needs make generate first", Fix: fix}
	}
}

func TestPassOpensInvestigateForVerifyParked(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	_, run := e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)

	require.NoError(t, e.ctl.pass(ctx))

	heals := e.store.allHeals()
	require.Len(t, heals, 1)
	h := heals[0]
	assert.Equal(t, state.StageInvestigate, h.Stage)
	assert.Equal(t, protocol.StopCauseVerifyParked, h.Cause)
	assert.Equal(t, RespInvestigateFix, h.Response)
	assert.Equal(t, run.StartedAt, h.RunStarted)
	assert.Equal(t, run.RunID, h.RunID)
	assert.True(t, e.store.run("alpha", "ALPHA-001").Decided)
	assert.Empty(t, e.backend.sent, "a heal opened in a pass is dispatched by the next")
	assert.True(t, e.ctl.TakeNudgeForTest(), "the next pass follows at once")
	assert.Equal(t, StateWorking, e.ctl.Status().State)
	assert.Equal(t, 1, e.ctl.Status().Healing)

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, protocol.RunKindInvestigate, e.backend.sent[0].Kind)
	assert.InDelta(t, state.DefaultSettings().InvestigateCeilingUSD, e.backend.sent[0].MaxCostUSD, 0)
	assert.Equal(t, protocol.RunKindInvestigate, e.store.heal(h.ID).Dispatch.Kind)
	assert.False(t, e.ctl.TakeNudgeForTest(), "a dispatch waits for its report")
	assert.Empty(t, e.cards.logged, "opening and dispatching write no activity entry")
}

func TestPassIgnoresDoneAndHumanStop(t *testing.T) {
	tests := []struct {
		name string
		edit func(*board.Card, *state.Run)
	}{
		{"done", func(c *board.Card, r *state.Run) {
			c.State, c.WorkerStatus, r.StopCause = board.StateReview, "completed", protocol.StopCauseDone
		}},
		{"human stop", func(c *board.Card, r *state.Run) {
			c.WorkerStatus, r.StopCause, r.HumanStop = "killed", protocol.StopCauseKilled, true
		}},
		{"claimed by a human since", func(c *board.Card, _ *state.Run) { c.AssignedAgent = "human:alice" }},
		{"finished", func(c *board.Card, _ *state.Run) { c.State = board.StateDone }},
		{"finished before its run settled", func(c *board.Card, r *state.Run) {
			c.State, c.WorkerStatus, c.Updated, r.StoppedAt = board.StateDone, "", epoch, nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, tc.edit)

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Empty(t, e.store.allHeals())
			assert.True(t, e.store.run("alpha", "ALPHA-001").Decided)
			assert.Empty(t, e.cards.logged)
			assert.Empty(t, e.backend.sent)
		})
	}
}

func TestPassParksBudgetExceededWithOneEntry(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseBudgetExceeded)

	require.NoError(t, e.ctl.pass(ctx))
	require.NoError(t, e.ctl.pass(ctx))

	heals := e.store.allHeals()
	require.Len(t, heals, 1)
	assert.Equal(t, state.StageParked, heals[0].Stage)
	assert.Equal(t, RespPark, heals[0].Response)
	assert.Equal(t, responseFor(protocol.StopCauseBudgetExceeded).Reason, heals[0].Reason)
	assert.True(t, e.store.run("alpha", "ALPHA-001").Decided)

	entries := e.cards.logged["ALPHA-001"]
	require.Len(t, entries, 1, "a heal writes one activity entry")
	assert.Equal(t, "system:sentinel", entries[0].Agent)
	assert.Equal(t, "sentinel", entries[0].Action)
	assert.Equal(t, epoch, entries[0].Timestamp)
	assert.Contains(t, entries[0].Message, heals[0].Reason)

	assert.Empty(t, e.backend.sent)
	assert.False(t, e.ctl.TakeNudgeForTest(), "a heal parked at once needs no next pass")
	assert.Equal(t, StateWatching, e.ctl.Status().State)
}

func TestPassOpensEachResponseAtItsStage(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*board.Card, *state.Run)
		stage   string
		entries int // a heal parked at once writes its one entry
	}{
		{"investigate_park", func(_ *board.Card, r *state.Run) { r.StopCause = protocol.StopCauseError }, state.StageInvestigate, 0},
		{"usage_rerun", func(_ *board.Card, r *state.Run) { r.StopCause = protocol.StopCauseUsageLimit }, state.StageUsageWait, 0},
		{"investigate_fix with its attempts used", func(_ *board.Card, r *state.Run) { r.Attempts = state.MaxHealAttempts }, state.StageParked, 1},
		{"investigate without a run ID", func(_ *board.Card, r *state.Run) { r.RunID = "" }, state.StageParked, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, tc.edit)

			require.NoError(t, e.ctl.pass(context.Background()))

			heals := e.store.allHeals()
			require.Len(t, heals, 1)
			assert.Equal(t, tc.stage, heals[0].Stage)
			assert.Len(t, e.cards.logged["ALPHA-001"], tc.entries)
			assert.Empty(t, e.backend.sent)
		})
	}
}

func TestPassWaitsForCallbackAfterReportParked(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", "", func(c *board.Card, r *state.Run) {
		c.State, c.WorkerStatus, c.Updated = board.StateReview, "parked", epoch.Add(-time.Minute)
		r.StoppedAt = nil
	})

	require.NoError(t, e.ctl.pass(ctx))

	assert.Empty(t, e.store.allHeals())
	assert.False(t, e.store.run("alpha", "ALPHA-001").Decided)
	assert.Empty(t, e.cards.logged)
	assert.False(t, e.ctl.TakeNudgeForTest())

	// No callback came within the settle grace: the run settles as unknown.
	e.clk.Advance(9 * time.Minute)
	require.NoError(t, e.ctl.pass(ctx))

	heals := e.store.allHeals()
	require.Len(t, heals, 1)
	assert.Equal(t, CauseUnknown, heals[0].Cause)
	assert.Equal(t, state.StageInvestigate, heals[0].Stage)
}

func TestPassSkipsCardsWithOpenHeal(t *testing.T) {
	e := newPassEnv(t)
	e.backend.health.MaxConcurrent = 1 // no room, so the heal waits
	_, run := e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	before := e.openHeal(t, "ALPHA-001", state.StageInvestigate)
	e.store.putRun(run) // the card's run reads undecided again

	require.NoError(t, e.ctl.pass(context.Background()))

	heals := e.store.allHeals()
	require.Len(t, heals, 1)
	assert.Equal(t, before, heals[0])
	assert.False(t, e.store.run("alpha", "ALPHA-001").Decided, "a card with an open heal is not considered")
	assert.Empty(t, e.backend.sent)
}

func TestPassDoesNothingWhenOff(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(state.DefaultSettings())
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.stopped("ALPHA-002", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-002", state.StageInvestigate, dispatchedAgo(protocol.RunKindInvestigate, time.Hour))

	before := e.store.otherCalls()

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, 1, e.store.passes())
	assert.Equal(t, before, e.store.otherCalls(), "no store reads beyond settings")
	assert.Zero(t, e.cards.calls)
	assert.Zero(t, e.backend.lists+e.backend.healths)
	assert.Equal(t, StateOff, e.ctl.Status().State)
}

func TestPassHealingOffStillSettlesNothingNew(t *testing.T) {
	e := newPassEnv(t)
	set := enabledSettings()
	set.Healing = false
	e.store.set(set)
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	open := e.openHeal(t, "ALPHA-001", state.StageInvestigate)
	e.stopped("ALPHA-002", protocol.StopCauseVerifyParked)

	require.NoError(t, e.ctl.pass(context.Background()))

	heals := e.store.allHeals()
	require.Len(t, heals, 1, "no new heals")
	assert.Equal(t, open, heals[0], "the open heal waits")
	assert.False(t, e.store.run("alpha", "ALPHA-002").Decided)
	assert.Empty(t, e.backend.sent)
	assert.Zero(t, e.backend.healths, "no dispatch can happen")
	assert.Equal(t, 1, e.ctl.Status().Healing)
}

func TestPassParksInvestigationThatNeverReported(t *testing.T) {
	tests := []struct {
		name       string
		containers []backend.ContainerInfo
		parks      bool
	}{
		{"not listed", nil, true},
		{"another card's run kind listed", []backend.ContainerInfo{{Project: "alpha", CardID: "ALPHA-002", Kind: protocol.RunKindInvestigate}}, true},
		{"the card's own run listed", []backend.ContainerInfo{{Project: "alpha", CardID: "ALPHA-001"}}, true},
		{"listed", []backend.ContainerInfo{{Project: "alpha", CardID: "ALPHA-001", Kind: protocol.RunKindInvestigate}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			ctx := context.Background()

			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
			e.openHeal(t, "ALPHA-001", state.StageInvestigate, dispatchedAgo(protocol.RunKindInvestigate, 11*time.Minute))
			e.backend.containers = tc.containers

			require.NoError(t, e.ctl.pass(ctx))

			h := e.store.heal(1)
			if !tc.parks {
				assert.Equal(t, state.StageInvestigate, h.Stage)
				assert.Empty(t, e.store.charges())
				assert.Empty(t, e.cards.logged)

				return
			}

			assert.Equal(t, state.StageParked, h.Stage)
			assert.Contains(t, h.Reason, protocol.RunKindInvestigate)
			assert.NotEmpty(t, h.Dispatch.Failure, "the dispatch is marked failed with the park")
			assert.Equal(t, []ledgerRow{{HealID: 1, Mode: "endpoint", USD: 2, At: epoch}}, e.store.charges(), "the ceiling is charged once")
			assert.Len(t, e.cards.logged["ALPHA-001"], 1)

			// The run reports after all: nothing is recorded or charged again.
			require.NoError(t, e.ctl.RunKindStatus(ctx, protocol.StatusCallbackPayload{
				Project: "alpha", CardID: "ALPHA-001", Kind: protocol.RunKindInvestigate, RunID: "rk-late", WorkerStatus: "failed",
				Usage: []protocol.RunUsage{{Model: "z-ai/glm-5.2", CostUSD: 0.5}},
			}))
			require.NoError(t, e.ctl.pass(ctx))

			assert.Len(t, e.store.charges(), 1)
			assert.Equal(t, h, e.store.heal(1))
			assert.Len(t, e.cards.logged["ALPHA-001"], 1)
			assert.InDelta(t, 2.0, e.ctl.Status().SpentTodayUSD, 0)
		})
	}
}

func TestPassHoldsARunKindAnotherHealOfTheCardHasInFlight(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)

	// A human's Run closed the first heal mid-investigation, and that run
	// stopped too: the second heal must not send a second investigate.
	first := state.Heal{
		ID: 1, Kind: state.HealKindHeal, Project: "alpha", CardID: "ALPHA-001", RunStarted: runStarted.Add(-time.Hour),
		Stage: state.StageClosed, Reason: reasonTakenOver, UpdatedAt: epoch.Add(-4 * time.Minute),
	}
	dispatchedAgo(protocol.RunKindInvestigate, 5*time.Minute)(&first)
	e.store.putHeal(first)
	second := e.openHeal(t, "ALPHA-001", state.StageInvestigate)

	e.backend.containers = []backend.ContainerInfo{{Project: "alpha", CardID: "ALPHA-001", Kind: protocol.RunKindInvestigate}}

	require.NoError(t, e.ctl.pass(ctx))
	e.clk.Advance(20 * time.Minute)
	require.NoError(t, e.ctl.pass(ctx))

	assert.Empty(t, e.backend.sent, "the first heal's run is still listed")
	assert.Equal(t, first, e.store.heal(first.ID))
	assert.Empty(t, e.store.charges())

	// The first heal's run is gone and never reported.
	e.backend.containers = nil

	require.NoError(t, e.ctl.pass(ctx))

	silent := e.store.heal(first.ID)
	assert.Equal(t, state.StageClosed, silent.Stage, "a finished heal keeps its stage")
	assert.Equal(t, first.UpdatedAt, silent.UpdatedAt)
	assert.False(t, inFlight(silent.Dispatch), "its dispatch is marked failed")
	assert.Equal(t, []ledgerRow{{HealID: first.ID, Mode: "endpoint", USD: 2, At: e.clk.Now()}}, e.store.charges(), "its ceiling is charged once")
	assert.Empty(t, e.cards.logged, "a finished heal writes no entry")

	require.Len(t, e.backend.sent, 1, "the second heal now dispatches")
	assert.Equal(t, protocol.RunKindInvestigate, e.store.heal(second.ID).Dispatch.Kind)

	require.NoError(t, e.ctl.pass(ctx))
	assert.Len(t, e.store.charges(), 1)
}

func TestPassNeverActsOnTerminalHeal(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	card, _ := e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)

	// A human edited the card mid-investigation, so the heal parked with its
	// dispatch still in flight.
	card.Title = "Fix the build properly"

	e.openHeal(t, "ALPHA-001", state.StageParked, dispatchedAgo(protocol.RunKindInvestigate, 5*time.Minute),
		func(h *state.Heal) { h.Reason = "edited since launch: title edited" })

	// The late answer proposes a fix: it is recorded and charged, never applied.
	require.NoError(t, e.ctl.RunKindStatus(ctx, protocol.StatusCallbackPayload{
		Project: "alpha", CardID: "ALPHA-001", Kind: protocol.RunKindInvestigate, RunID: "rk-late",
		WorkerStatus: "completed", StopCause: protocol.StopCauseDone,
		Result: &protocol.RunKindResult{Investigate: &protocol.InvestigateResult{
			Cause: protocol.DiagnosisCard, Diagnosis: "needs make generate", Fix: &protocol.CardFix{Section: "Run make generate first."},
		}},
		Usage: []protocol.RunUsage{{Model: "z-ai/glm-5.2", CostUSD: 0.4}},
	}))

	answered := e.store.heal(1)
	require.NotNil(t, answered.Investigation)

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, answered, e.store.heal(1), "a terminal heal is never read, so never saved again")
	assert.Empty(t, e.cards.logged, "nor parked a second time")
	assert.Empty(t, e.backend.sent)
	assert.Zero(t, e.backend.lists+e.backend.healths)
}

func TestPassAdvancesAnAnsweredInvestigation(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate,
		investigated(protocol.DiagnosisCard, &protocol.CardFix{Section: "Run make generate first."}))

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageCheck, e.store.heal(1).Stage)
	assert.True(t, e.ctl.TakeNudgeForTest(), "the next stage is decided at once")

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, protocol.RunKindCheck, e.backend.sent[0].Kind)
}

func TestPassVetsOnlyACardFixAtApply(t *testing.T) {
	retireUnsent := &protocol.CardFix{RetireSubtasks: []string{"ALPHA-404"}}

	tests := []struct {
		name  string
		cause string
		stage string
	}{
		{"a card fix that skipped the check is still vetted", protocol.DiagnosisCard, state.StageParked},
		{"a model diagnosis holds no fix to vet", protocol.DiagnosisModel, state.StageRerun},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
			e.openHeal(t, "ALPHA-001", state.StageApply, investigated(tc.cause, retireUnsent))

			require.NoError(t, e.ctl.pass(context.Background()))

			h := e.store.heal(1)
			assert.Equal(t, tc.stage, h.Stage)

			if tc.stage == state.StageParked {
				assert.Contains(t, h.Reason, "ALPHA-404")
			}
		})
	}
}

func TestPassAppliesAFixAndReruns(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageApply,
		investigated(protocol.DiagnosisCard, &protocol.CardFix{Section: "Run make generate first."}),
		func(h *state.Heal) { h.Check = &protocol.CheckResult{Trial: protocol.TrialFinished} })

	require.NoError(t, e.ctl.pass(ctx))

	h := e.store.heal(1)
	assert.Equal(t, state.StageRerun, h.Stage)
	require.NotNil(t, h.Change)
	assert.True(t, h.Change.Fresh)
	assert.Contains(t, storedCard(t, e, "ALPHA-001").Body, "Run make generate first.")
	assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
	assert.Equal(t, 1, e.store.run("alpha", "ALPHA-001").Attempts)
	assert.Len(t, e.cards.logged["ALPHA-001"], 1)

	// The rerun has not started yet: the next pass waits on it.
	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, state.StageRerun, e.store.heal(1).Stage)
	assert.Len(t, e.relaunched, 1)
}

func TestPassResumesASectionFixWhenASubtaskHasStarted(t *testing.T) {
	e := newPassEnv(t)

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(c *board.Card, _ *state.Run) { c.Phase = "execute" })
	e.cards.cards = append(e.cards.cards, &board.Card{
		ID: "ALPHA-011", Project: "alpha", Title: "Part one", Parent: "ALPHA-001", State: board.StateInProgress,
		Updated: epoch.Add(-30 * time.Minute),
	})
	e.openHeal(t, "ALPHA-001", state.StageApply,
		investigated(protocol.DiagnosisCard, &protocol.CardFix{Section: "Run make generate first."}),
		func(h *state.Heal) { h.Check = &protocol.CheckResult{Trial: protocol.TrialFinished} })

	require.NoError(t, e.ctl.pass(context.Background()))

	h := e.store.heal(1)
	assert.Equal(t, state.StageRerun, h.Stage)
	require.NotNil(t, h.Change)
	assert.False(t, h.Change.Fresh)

	card := storedCard(t, e, "ALPHA-001")
	assert.Equal(t, "execute", card.Phase, "the rerun resumes on the started work")
	assert.Contains(t, card.Body, "Run make generate first.", "the section still reaches the agent")

	logged := e.cards.logged["ALPHA-001"]
	require.Len(t, logged, 1)
	assert.Equal(t, relaunchText(h.Change, nil, 1), logged[0].Message, "the entry says the rerun resumes")
}

func TestPassDropsAFailingSectionAndAppliesTheVerifyFix(t *testing.T) {
	verify := &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 1800}

	tests := []struct {
		name    string
		fix     protocol.CardFix
		stage   string
		dropped []string
	}{
		{"a verify fix applies without the section", protocol.CardFix{Section: "Run gen-tool first.", Verify: verify}, state.StageRerun, []string{"introduces gen-tool"}},
		{"a section-only fix has nothing left to apply", protocol.CardFix{Section: "Run gen-tool first."}, state.StageParked, nil},
		{"a verify that changes nothing leaves nothing to apply", protocol.CardFix{Section: "Run gen-tool first.", Verify: &protocol.VerifyConfig{Command: "make test"}}, state.StageParked, nil},
		{"a fix that retires subtasks parks", protocol.CardFix{Section: "Run gen-tool first.", Verify: verify, RetireSubtasks: []string{"ALPHA-011"}}, state.StageParked, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)

			e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(c *board.Card, _ *state.Run) {
				c.Phase, c.Verify = "execute", &board.VerifyConfig{Command: "make test"}
			})
			e.cards.cards = append(e.cards.cards, &board.Card{
				ID: "ALPHA-011", Project: "alpha", Title: "Part one", Parent: "ALPHA-001", State: board.StateTodo,
				Updated: epoch.Add(-30 * time.Minute),
			})
			e.openHeal(t, "ALPHA-001", state.StageApply, investigated(protocol.DiagnosisCard, &tc.fix), func(h *state.Heal) {
				h.SentSubtasks = []string{"ALPHA-011"}
				h.Check = &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished, AbsentFromRepo: []string{"gen-tool"}}
			})

			require.NoError(t, e.ctl.pass(context.Background()))

			h := e.store.heal(1)
			assert.Equal(t, tc.stage, h.Stage)
			assert.Equal(t, tc.dropped, h.Dropped)

			card := storedCard(t, e, "ALPHA-001")
			assert.Empty(t, sentinelSection(card), "the failing section is never written")

			logged := e.cards.logged["ALPHA-001"]
			require.Len(t, logged, 1)

			if tc.stage == state.StageParked {
				assert.Contains(t, h.Reason, "introduces gen-tool")
				assert.Equal(t, tc.fix, *healFix(h), "a refused fix is kept as the investigation proposed it")
				assert.Equal(t, activityText(h), logged[0].Message)

				return
			}

			assert.Equal(t, protocol.CardFix{Verify: verify}, *healFix(h), "the heal holds the fix it applied")
			assert.Equal(t, 1800, card.Verify.TimeoutSeconds)
			assert.Equal(t, "execute", card.Phase, "the section did not change, so the rerun resumes")
			require.NotNil(t, h.Change)
			assert.False(t, h.Change.Fresh)
			assert.Equal(t, relaunchText(h.Change, h.Dropped, 1), logged[0].Message, "the one entry names the dropped section")
			assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
		})
	}
}

func TestPassDropsTheSectionOnACardThatEndsInAFence(t *testing.T) {
	e := newPassEnv(t)
	body := "Make the build pass.\n\n```\nunclosed human fence\n"
	fix := protocol.CardFix{Section: "Run make generate first.", Verify: &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 1800}}

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(c *board.Card, _ *state.Run) {
		c.Body, c.Phase, c.Verify = body, "execute", &board.VerifyConfig{Command: "make test"}
	})
	e.openHeal(t, "ALPHA-001", state.StageApply, investigated(protocol.DiagnosisCard, &fix), func(h *state.Heal) {
		h.Check = &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished}
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	h := e.store.heal(1)
	assert.Equal(t, state.StageRerun, h.Stage)
	assert.Equal(t, checkSection(fixContext{Card: &board.Card{Body: body}, Fix: fix}), h.Dropped, "the section check's reasons")
	assert.Equal(t, protocol.CardFix{Verify: fix.Verify}, *healFix(h))

	card := storedCard(t, e, "ALPHA-001")
	assert.Equal(t, body, card.Body, "no section is written onto a card that ends inside a fence")
	assert.Equal(t, 1800, card.Verify.TimeoutSeconds)
	assert.Equal(t, "execute", card.Phase, "the rerun resumes")
	require.NotNil(t, h.Change)
	assert.False(t, h.Change.Fresh)
	assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
}

func TestPassLeavesAFixAnEditRefusedAtApply(t *testing.T) {
	e := newPassEnv(t)

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageApply,
		investigated(protocol.DiagnosisCard, &protocol.CardFix{Section: "Run make generate first."}),
		func(h *state.Heal) { h.Check = &protocol.CheckResult{Trial: protocol.TrialFinished} })
	e.cards.refuse = map[string]error{"ALPHA-001": service.ErrCardChanged}

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Equal(t, state.StageApply, e.store.heal(1).Stage)
	assert.Empty(t, e.relaunched)
	assert.Empty(t, e.cards.logged)
	assert.Zero(t, e.store.run("alpha", "ALPHA-001").Attempts)
	assert.True(t, e.ctl.TakeNudgeForTest(), "the next pass decides again")
}

func TestPassLeavesAHealChangedUnderItToTheNextPass(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate, investigated(protocol.DiagnosisEnvironment, nil))

	// A callback writes the heal after the pass read it.
	e.cards.onGet = func() {
		h := e.store.heal(1)
		h.Version++
		e.store.putHeal(h)
		e.cards.onGet = nil
	}

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageInvestigate, e.store.heal(1).Stage)
	assert.Empty(t, e.cards.logged)
	assert.True(t, e.ctl.TakeNudgeForTest(), "the next pass decides again")

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageParked, e.store.heal(1).Stage)
	assert.Len(t, e.cards.logged["ALPHA-001"], 1)
}

func TestPassReadsTheAgentOnlyWhenNeeded(t *testing.T) {
	young := dispatchedAgo(protocol.RunKindInvestigate, time.Minute)

	tests := []struct {
		name          string
		heals         []func(*state.Heal) // one heal per card, at stage investigate
		lists         int
		healths       int
		dispatched    int
		maxConcurrent int
	}{
		{"nothing open", nil, 0, 0, 0, 3},
		{"two dispatches in flight", []func(*state.Heal){young, young}, 1, 0, 0, 3},
		{"two heals to dispatch", []func(*state.Heal){nil, nil}, 0, 1, 2, 3},
		{"one slot left for card runs", []func(*state.Heal){nil, nil}, 0, 1, 1, 2},
		{"answered: nothing to send", []func(*state.Heal){investigated(protocol.DiagnosisModel, nil)}, 0, 0, 0, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			e.backend.health.MaxConcurrent = tc.maxConcurrent

			for i, edit := range tc.heals {
				id := []string{"ALPHA-001", "ALPHA-002"}[i]
				e.stopped(id, protocol.StopCauseVerifyParked)

				var edits []func(*state.Heal)
				if edit != nil {
					edits = append(edits, edit)
				}

				e.openHeal(t, id, state.StageInvestigate, edits...)
			}

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Equal(t, tc.lists, e.backend.lists)
			assert.Equal(t, tc.healths, e.backend.healths)
			assert.Len(t, e.backend.sent, tc.dispatched)
		})
	}
}

func TestPassReservesTheDailyCapForWorkInFlight(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	e.backend.health.MaxConcurrent = 10

	set := enabledSettings()
	set.DailyCapUSD = 3.5
	e.store.set(set)
	e.ctl.SetRunKindBase(func(_ context.Context, project, cardID string, _ bool) (protocol.RunKindPayload, llmchannel.Mode, error) {
		return protocol.RunKindPayload{Project: project, CardID: cardID}, llmchannel.ModeEndpoint, nil
	})

	// One investigation is in flight; two heals wait to send theirs.
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate, dispatchedAgo(protocol.RunKindInvestigate, time.Minute))
	e.stopped("ALPHA-002", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-002", state.StageInvestigate)
	e.stopped("ALPHA-003", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-003", state.StageInvestigate)

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1, "2.00 in flight leaves room for one more 2.00 ceiling under 3.50, not two")
	assert.Equal(t, "ALPHA-002", e.backend.sent[0].CardID)

	st := e.ctl.Status()
	assert.Equal(t, StatePaused, st.State)
	assert.Equal(t, "daily cap reached", st.Reason)
	assert.Zero(t, st.SpentTodayUSD, "nothing is charged yet")
}

func TestPassReservesTheDailyCapForARerunsCostSoFar(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	set := enabledSettings()
	set.DailyCapUSD = 2
	e.store.set(set)

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate)

	// A later heal's rerun is still going and has cost 2.00 since its relaunch.
	relaunched := epoch.Add(-30 * time.Minute)

	e.stopped("ALPHA-002", protocol.StopCauseVerifyParked, func(c *board.Card, r *state.Run) {
		c.WorkerStatus, c.TokenUsage = "running", &board.TokenUsage{EstimatedCostUSD: 2.5}
		r.StartedAt, r.StoppedAt, r.Launch.Origin = relaunched.Add(time.Minute), nil, state.OriginSentinel
	})
	e.openHeal(t, "ALPHA-002", state.StageRerun, relaunchedAt(relaunched, 0.5))

	require.NoError(t, e.ctl.pass(ctx))

	assert.Empty(t, e.backend.sent, "the rerun's cost so far fills the cap")
	assert.Equal(t, StatePaused, e.ctl.Status().State)
}

func TestPassReportsSpendAndHealedToday(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	today, yesterday := epoch.Add(-time.Hour), epoch.Add(-13*time.Hour)

	require.NoError(t, e.store.Charge(ctx, 9, "endpoint", 1.5, today))
	require.NoError(t, e.store.Charge(ctx, 8, "endpoint", 4, yesterday))
	require.NoError(t, e.store.Charge(ctx, 9, "claude_code", 7, today))
	e.store.putHeal(state.Heal{ID: 9, Kind: state.HealKindHeal, Project: "alpha", CardID: "ALPHA-009", Stage: state.StageHealed, UpdatedAt: today})
	e.store.putHeal(state.Heal{ID: 8, Kind: state.HealKindHeal, Project: "alpha", CardID: "ALPHA-008", Stage: state.StageHealed, UpdatedAt: yesterday})

	require.NoError(t, e.ctl.pass(ctx))

	st := e.ctl.Status()
	assert.InDelta(t, 1.5, st.SpentTodayUSD, 1e-9, "the endpoint ledger since 00:00 UTC")
	assert.Equal(t, 1, st.HealedToday)
	assert.Zero(t, st.Healing)
	assert.Equal(t, StateWatching, st.State)

	set := enabledSettings()
	set.DailyCapUSD = 1.5
	e.store.set(set)

	require.NoError(t, e.ctl.pass(ctx))

	st = e.ctl.Status()
	assert.Equal(t, StatePaused, st.State)
	assert.Equal(t, "daily cap reached", st.Reason)
}

func TestPassDispatchesHealsBeforeAnalyses(t *testing.T) {
	e := newPassEnv(t) // one worker slot for run kinds
	e.store.set(analysing())
	e.stopped("ALPHA-002", protocol.StopCauseDone, reviewed)
	require.NoError(t, e.store.MarkRunDecided(context.Background(), "alpha", "ALPHA-002", runStarted))
	row := e.queued(t, "ALPHA-002")
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate)

	require.NoError(t, e.ctl.pass(context.Background()))

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, protocol.RunKindInvestigate, e.backend.sent[0].Kind)
	assert.Nil(t, e.store.heal(row.ID).Dispatch.At, "the analysis waits for a slot")
	assert.Equal(t, 1, e.backend.healths, "the agent's health is read once")
}

func TestPassCountsEachDispatchForTheNext(t *testing.T) {
	tests := []struct {
		name          string
		cap, ledger   float64
		heal          bool // ALPHA-003 has a heal at investigate, stepped before the analyses
		maxConcurrent int
		sent          []string
	}{
		{"room and budget for both", 20, 0, false, 10, []string{protocol.RunKindAnalyse, protocol.RunKindAnalyse}},
		{"a heal's ceiling shuts the analysis gate", 20, 9, true, 10, []string{protocol.RunKindInvestigate}},
		{"an analysis's ceiling shuts the gate for the next", 2, 0, false, 10, []string{protocol.RunKindAnalyse}},
		{"an analysis takes the slot the next needs", 20, 0, false, 2, []string{protocol.RunKindAnalyse}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newPassEnv(t)
			e.backend.health.MaxConcurrent = tc.maxConcurrent
			e.baseIn(llmchannel.ModeEndpoint)

			s := analysing()
			s.DailyCapUSD = tc.cap
			e.store.set(s)
			require.NoError(t, e.store.Charge(ctx, 99, "endpoint", tc.ledger, epoch.Add(-time.Hour)))

			for _, id := range []string{"ALPHA-001", "ALPHA-002"} {
				e.stopped(id, protocol.StopCauseDone, reviewed)
				require.NoError(t, e.store.MarkRunDecided(ctx, "alpha", id, runStarted))
				e.queued(t, id)
			}

			if tc.heal {
				e.stopped("ALPHA-003", protocol.StopCauseVerifyParked)
				e.openHeal(t, "ALPHA-003", state.StageInvestigate)
			}

			require.NoError(t, e.ctl.pass(ctx))

			kinds := make([]string, 0, len(e.backend.sent))
			for _, p := range e.backend.sent {
				kinds = append(kinds, p.Kind)
			}

			assert.Equal(t, tc.sent, kinds)
		})
	}
}
