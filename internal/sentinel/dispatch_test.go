package sentinel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

type dispatchEnv struct {
	store   *fakeStore
	backend *fakeBackend
	cards   *fakeCards
	pbs     fakePlaybooks
	runner  *fakeRunner
	clk     *clock.FakeClock
	ctl     *Controller
	models  []bool // the model flag of each RunKindBase call

	relaunched  []string // "<project>/<card>" per relauncher call
	relaunchErr error
	onRelaunch  func() // runs before the relauncher answers
}

func newDispatchEnv(t *testing.T) *dispatchEnv {
	t.Helper()

	e := &dispatchEnv{
		store:   &fakeStore{settings: enabledSettings()},
		backend: &fakeBackend{},
		cards:   &fakeCards{projects: map[string]*board.ProjectConfig{}},
		pbs:     fakePlaybooks{},
		runner:  &fakeRunner{},
		clk:     clock.Fake(epoch),
	}
	e.ctl = New(Config{
		Store: e.store, Channel: fakeChannel{config.BackendNameAgent: llmchannel.ModeEndpoint}, Bus: events.NewBus(),
		Clock: e.clk, Pricer: ratePricer{}, Backend: e.backend, Cards: e.cards, Playbooks: e.pbs, Runner: e.runner,
		Instance: "lap-a",
	})
	e.ctl.SetRelauncher(func(_ context.Context, project, cardID string) error {
		if e.onRelaunch != nil {
			e.onRelaunch()
		}

		e.relaunched = append(e.relaunched, project+"/"+cardID)

		return e.relaunchErr
	})
	e.ctl.SetRunKindBase(func(_ context.Context, project, cardID string, model bool) (protocol.RunKindPayload, llmchannel.Mode, error) {
		e.models = append(e.models, model)

		p := protocol.RunKindPayload{CardID: cardID, Project: project, RepoURL: "https://github.com/acme/alpha.git", GitToken: "read-tok"}
		if !model {
			return p, "", nil
		}

		p.Model = "z-ai/glm-5.2"

		return p, llmchannel.ModeClaudeCode, nil
	})

	return e
}

// heal stores ALPHA-001's heal at stage, stopped by verify_parked, and returns
// the copy a pass would have read.
func (e *dispatchEnv) heal(stage string) *state.Heal {
	h := state.Heal{
		ID: 1, Kind: state.HealKindHeal, Project: "alpha", CardID: "ALPHA-001", RunStarted: runStarted,
		RunID: "run-1", Cause: protocol.StopCauseVerifyParked, Stage: stage,
	}
	e.store.putHeal(h)

	return &h
}

func investigateRun() protocol.RunKindPayload {
	return protocol.RunKindPayload{Kind: protocol.RunKindInvestigate, CardID: "ALPHA-001", Project: "alpha", MaxCostUSD: 2}
}

func TestDispatchRecordsBeforeSending(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)

	var atSend state.Heal

	e.backend.onSend = func(protocol.RunKindPayload) { atSend = e.store.heal(1) }

	require.NoError(t, e.ctl.dispatch(context.Background(), h, investigateRun(), llmchannel.ModeClaudeCode))

	require.NotNil(t, atSend.Dispatch.At, "the dispatch is stored before the agent hears of it")
	assert.Equal(t, epoch, *atSend.Dispatch.At)
	assert.Equal(t, protocol.RunKindInvestigate, atSend.Dispatch.Kind)
	assert.Equal(t, "claude_code", atSend.Dispatch.Mode)
	assert.InDelta(t, 2.0, atSend.Dispatch.CeilingUSD, 0)
	assert.Equal(t, []protocol.RunKindPayload{investigateRun()}, e.backend.sent)

	assert.Equal(t, atSend, e.store.heal(1), "nothing is written after a send the agent accepted")
	assert.Equal(t, e.store.heal(1).Version, h.Version, "h follows the row, so a pass can save it again")
}

func TestDispatchStaleHealSendsNothing(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)
	h.Version = 7 // a callback wrote the row since h was read

	err := e.ctl.dispatch(context.Background(), h, investigateRun(), llmchannel.ModeEndpoint)
	require.ErrorIs(t, err, state.ErrHealChanged)
	assert.Empty(t, e.backend.sent)
	assert.Nil(t, e.store.heal(1).Dispatch.At)
}

func TestDispatchErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		returned    bool // the send error comes back to the caller
		inFlight    bool // the recorded dispatch stays in flight
		stage       string
		state       string
		stateReason string
	}{
		{"unavailable", backend.ErrRunKindsUnavailable, true, false, state.StageInvestigate, StatePaused, "the agent has no run kinds"},
		{"busy", backend.ErrRunKindBusy, true, false, state.StageInvestigate, StateWatching, ""},
		{"transport: the run may have started", errors.New("connection reset"), false, true, state.StageInvestigate, StateWatching, ""},
		{"rejected", fmt.Errorf("%w: invalid_field git_token", backend.ErrRunKindRejected), false, false, state.StageParked, StateWatching, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newDispatchEnv(t)
			h := e.heal(state.StageInvestigate)
			e.backend.err = tc.err

			err := e.ctl.dispatch(context.Background(), h, investigateRun(), llmchannel.ModeEndpoint)
			if tc.returned {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			stored := e.store.heal(1)
			if tc.inFlight {
				require.NotNil(t, stored.Dispatch.At, "a send of unknown outcome stays on record")
				assert.Equal(t, protocol.RunKindInvestigate, stored.Dispatch.Kind)
				assert.InDelta(t, 2.0, stored.Dispatch.CeilingUSD, 0)
			} else {
				assert.Equal(t, state.Dispatch{}, stored.Dispatch, "an answer that started nothing leaves no dispatch in flight")
			}

			assert.Equal(t, tc.stage, stored.Stage)

			if tc.stage == state.StageParked {
				assert.Contains(t, stored.Reason, protocol.RunKindInvestigate)
			}

			require.NoError(t, e.ctl.pass(context.Background()))

			st := e.ctl.Status()
			assert.Equal(t, tc.state, st.State)
			assert.Equal(t, tc.stateReason, st.Reason)
		})
	}
}

func TestDispatchClearKeepsAReportRecordedMeanwhile(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageInvestigate)

	// The status callback records an answer while the send is still out.
	e.backend.onSend = func(protocol.RunKindPayload) {
		answered := e.store.heal(1)
		answered.Dispatch.AnsweredRunID = "run-k1"
		answered.Version++
		e.store.putHeal(answered)
	}
	e.backend.err = backend.ErrRunKindBusy

	err := e.ctl.dispatch(context.Background(), h, investigateRun(), llmchannel.ModeEndpoint)
	require.ErrorIs(t, err, backend.ErrRunKindBusy)

	stored := e.store.heal(1)
	assert.Equal(t, "run-k1", stored.Dispatch.AnsweredRunID, "the stale clear never overwrites the report")
	assert.NotNil(t, stored.Dispatch.At)
}

func TestDispatchHoldsWhileTheAgentHasNoRunKinds(t *testing.T) {
	e := newDispatchEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked) // so a pass leaves the heal waiting
	h := e.heal(state.StageInvestigate)

	e.backend.err = backend.ErrRunKindsUnavailable
	require.Error(t, e.ctl.dispatch(ctx, h, investigateRun(), llmchannel.ModeEndpoint))

	e.backend.err = nil
	e.clk.Advance(15*time.Minute - time.Second)
	require.NoError(t, e.ctl.dispatch(ctx, h, investigateRun(), llmchannel.ModeEndpoint))
	assert.Len(t, e.backend.sent, 1, "no send for 15 minutes")
	assert.Nil(t, e.store.heal(1).Dispatch.At)

	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, StatePaused, e.ctl.Status().State)

	e.clk.Advance(time.Second)
	require.NoError(t, e.ctl.dispatch(ctx, h, investigateRun(), llmchannel.ModeEndpoint))
	assert.Len(t, e.backend.sent, 2)
	assert.NotNil(t, e.store.heal(1).Dispatch.At)

	require.NoError(t, e.ctl.pass(ctx))
	assert.Equal(t, StateWorking, e.ctl.Status().State, "the hold is over and the heal is in flight")
}

// investigateCards stores ALPHA-001 with two subtasks, in an active playbook
// run on this instance whose other unstarted card is ALPHA-020.
func (e *dispatchEnv) investigateCards(body string) *board.Card {
	card := &board.Card{
		ID: "ALPHA-001", Project: "alpha", Title: "Fix the build", Type: "task", State: board.StateTodo,
		Verify: &board.VerifyConfig{Command: "make test"}, Body: body,
		PlaybookLock: &board.CardPlaybookLock{ID: "rollout", RunStatus: board.RunStatusRunning},
	}
	e.cards.cards = []*board.Card{
		card,
		{ID: "ALPHA-010", Project: "alpha", Title: "Part one", Parent: "ALPHA-001", State: board.StateDone},
		{ID: "ALPHA-011", Project: "alpha", Title: "Part two", Parent: "ALPHA-001", State: board.StateTodo},
		{ID: "ALPHA-020", Project: "alpha", Title: "Next card", State: board.StateTodo},
	}
	e.cards.projects["alpha"] = &board.ProjectConfig{
		Name: "alpha", Verify: &board.VerifyConfig{TimeoutSeconds: 600, Env: []string{"GOFLAGS"}},
	}
	e.pbs["rollout"] = &service.PlaybookDetail{
		ID: "rollout", Runnable: true,
		Run: &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a"},
		Entries: []service.PlaybookEntryDetail{
			{PlaybookEntry: board.PlaybookEntry{ID: "e1", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-001"}},
			{PlaybookEntry: board.PlaybookEntry{ID: "e2", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-020"}},
		},
	}

	return card
}

func (e *dispatchEnv) file(t *testing.T, id string) string {
	t.Helper()

	c, err := e.cards.GetCard(context.Background(), "alpha", id)
	require.NoError(t, err)

	b, err := board.SerializeCard(c)
	require.NoError(t, err)

	return string(b)
}

func TestDispatchInvestigateSendsTheCardItsSubtasksAndSiblings(t *testing.T) {
	tests := []struct {
		cause, stopCause string
	}{
		{protocol.StopCauseVerifyParked, protocol.StopCauseVerifyParked},
		{CauseStalled, ""},
		{CauseUnknown, ""},
	}
	for _, tc := range tests {
		t.Run(tc.cause, func(t *testing.T) {
			e := newDispatchEnv(t)
			e.investigateCards("Make the build pass.\n")
			h := e.heal(state.StageInvestigate)
			h.Cause = tc.cause
			e.store.putHeal(*h)

			require.NoError(t, e.ctl.dispatchInvestigate(context.Background(), h, 2))

			require.Len(t, e.backend.sent, 1)
			p := e.backend.sent[0]
			assert.Equal(t, []bool{true}, e.models)
			assert.Equal(t, protocol.RunKindInvestigate, p.Kind)
			assert.Equal(t, "read-tok", p.GitToken)
			assert.Equal(t, "z-ai/glm-5.2", p.Model)
			assert.InDelta(t, 2.0, p.MaxCostUSD, 0)
			require.NotNil(t, p.Investigate)
			assert.Equal(t, protocol.InvestigateInput{
				RunID: "run-1", StopCause: tc.stopCause, Card: e.file(t, "ALPHA-001"),
				Verify:   &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 600, Env: []string{"GOFLAGS"}},
				Subtasks: []string{e.file(t, "ALPHA-010"), e.file(t, "ALPHA-011")},
				Siblings: []string{e.file(t, "ALPHA-020")},
			}, *p.Investigate)

			stored := e.store.heal(1)
			assert.Equal(t, []string{"ALPHA-010", "ALPHA-011"}, stored.SentSubtasks)
			assert.Equal(t, []string{"ALPHA-020"}, stored.SentSiblings)
			assert.Equal(t, "claude_code", stored.Dispatch.Mode)
			assert.InDelta(t, 2.0, stored.Dispatch.CeilingUSD, 0)
		})
	}
}

func TestDispatchInvestigateParksACardOverTheCap(t *testing.T) {
	e := newDispatchEnv(t)
	e.investigateCards(strings.Repeat("x", maxRunKindBody))
	h := e.heal(state.StageInvestigate)

	require.NoError(t, e.ctl.dispatchInvestigate(context.Background(), h, 2))

	assert.Empty(t, e.backend.sent)

	stored := e.store.heal(1)
	assert.Equal(t, state.StageParked, stored.Stage)
	assert.NotEmpty(t, stored.Reason)
	assert.Nil(t, stored.Dispatch.At)
}

func TestDispatchCheckSendsTheResolvedVerify(t *testing.T) {
	e := newDispatchEnv(t)
	e.cards.cards = []*board.Card{{
		ID: "ALPHA-001", Project: "alpha", State: board.StateTodo,
		Verify: &board.VerifyConfig{Command: "make test", TimeoutSeconds: 300},
	}}
	e.cards.projects["alpha"] = &board.ProjectConfig{Name: "alpha", Verify: &board.VerifyConfig{Env: []string{"GOFLAGS"}}}

	h := e.heal(state.StageCheck)
	h.Investigation = &protocol.InvestigateResult{Cause: protocol.DiagnosisCard, Fix: &protocol.CardFix{
		Section: "Run make generate first.",
		Verify:  &protocol.VerifyConfig{Command: " make test ", TimeoutSeconds: 900},
	}}
	e.store.putHeal(*h)

	require.NoError(t, e.ctl.dispatchCheck(context.Background(), h))

	require.Len(t, e.backend.sent, 1)
	p := e.backend.sent[0]
	assert.Equal(t, []bool{false}, e.models)
	assert.Equal(t, protocol.RunKindCheck, p.Kind)
	assert.Empty(t, p.Model)
	assert.Zero(t, p.MaxCostUSD)
	assert.Equal(t, &protocol.CheckInput{
		Verify:         &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 900, Env: []string{"GOFLAGS"}},
		CurrentCommand: "make test",
		Text:           "Run make generate first.",
	}, p.Check)

	stored := e.store.heal(1)
	assert.Equal(t, protocol.RunKindCheck, stored.Dispatch.Kind)
	assert.Empty(t, stored.Dispatch.Mode)
}

func TestDispatchCheckWithoutAFixSendsNothing(t *testing.T) {
	e := newDispatchEnv(t)
	h := e.heal(state.StageCheck)

	require.Error(t, e.ctl.dispatchCheck(context.Background(), h))
	assert.Empty(t, e.backend.sent)
}
