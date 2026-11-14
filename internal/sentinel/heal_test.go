package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestNextStep(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	ago := func(d time.Duration) *time.Time {
		at := now.Add(-d)

		return &at
	}

	// A heal at stage investigate for a verify_parked run, nothing dispatched yet.
	base := func() healView {
		return healView{
			Heal: state.Heal{Kind: state.HealKindHeal, RunStarted: started, Cause: "verify_parked", Response: RespInvestigateFix, Stage: state.StageInvestigate},
			Card: &board.Card{ID: "ALPHA-001", State: board.StateInProgress, WorkerStatus: "failed", Updated: now.Add(-time.Minute)},
			Run:  state.Run{StartedAt: started, StoppedAt: ago(30 * time.Minute), StopCause: "verify_parked"},

			Healing: true, Budget: true, Room: true, Now: now,
		}
	}
	with := func(edits ...func(*healView)) healView {
		v := base()
		for _, e := range edits {
			e(&v)
		}

		return v
	}

	atStage := func(s string) func(*healView) { return func(v *healView) { v.Heal.Stage = s } }
	cardIs := func(state, worker string) func(*healView) {
		return func(v *healView) { v.Card.State, v.Card.WorkerStatus = state, worker }
	}
	deleted := func(v *healView) { v.Card = nil }
	claimed := func(v *healView) { v.Card.AssignedAgent = "agent-alpha-001" }
	newerRun := func(v *healView) { v.Run.StartedAt = now.Add(-time.Minute) }
	healingOff := func(v *healView) { v.Healing = false }
	budgetShut := func(v *healView) { v.Budget = false }
	noRoom := func(v *healView) { v.Room = false }
	listed := func(v *healView) { v.Listed = true }
	edited := func(items ...string) func(*healView) { return func(v *healView) { v.Edited = items } }
	rejected := func(items ...string) func(*healView) { return func(v *healView) { v.Rejected = items } }
	response := func(r string) func(*healView) { return func(v *healView) { v.Heal.Response = r } }
	attempts := func(n int) func(*healView) { return func(v *healView) { v.Run.Attempts = n } }
	usageReruns := func(n int) func(*healView) { return func(v *healView) { v.Run.UsageReruns = n } }
	resetsIn := func(d time.Duration) func(*healView) {
		return func(v *healView) { v.Card.ParkResetsAt = ago(-d) }
	}

	inFlight := func(kind string, age time.Duration) func(*healView) {
		return func(v *healView) { v.Heal.Dispatch = state.Dispatch{Kind: kind, CeilingUSD: 2, At: ago(age)} }
	}
	answeredWithout := func(kind, failure string) func(*healView) {
		return func(v *healView) {
			v.Heal.Dispatch = state.Dispatch{Kind: kind, At: ago(20 * time.Minute), AnsweredRunID: "rk-9", Failure: failure}
		}
	}
	investigated := func(cause, diagnosis string, fix *protocol.CardFix) func(*healView) {
		return func(v *healView) {
			v.Heal.Dispatch = state.Dispatch{Kind: protocol.RunKindInvestigate, CeilingUSD: 2, At: ago(20 * time.Minute), AnsweredRunID: "rk-1"}
			v.Heal.Investigation = &protocol.InvestigateResult{Cause: cause, Diagnosis: diagnosis, Fix: fix}
		}
	}
	checked := func(v *healView) {
		v.Heal.Dispatch = state.Dispatch{Kind: protocol.RunKindCheck, At: ago(5 * time.Minute), AnsweredRunID: "rk-2"}
		v.Heal.Check = &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished}
	}

	sectionFix := &protocol.CardFix{Section: "Run `make test` from the repo root."}
	verifyFix := &protocol.CardFix{Verify: &protocol.VerifyConfig{Command: "make test"}}
	retireFix := &protocol.CardFix{RetireSubtasks: []string{"ALPHA-002"}}

	// relaunched puts the heal at stage rerun, relaunched age ago; the run row
	// stays the stopped run's until the rerun starts.
	relaunched := func(age time.Duration) func(*healView) {
		return func(v *healView) { v.Heal.Stage, v.Heal.RelaunchedAt = state.StageRerun, ago(age) }
	}
	rerunGoing := func(v *healView) {
		relaunched(30 * time.Minute)(v)
		v.Run = state.Run{StartedAt: now.Add(-29 * time.Minute), Launch: state.LaunchFacts{Origin: state.OriginSentinel}, Attempts: 1}
	}
	rerunStopped := func(cause string) func(*healView) {
		return func(v *healView) {
			rerunGoing(v)
			v.Run.StoppedAt, v.Run.StopCause = ago(time.Minute), cause
		}
	}
	// launchedFrom gives the run row started after the relaunch origin.
	launchedFrom := func(origin string) func(*healView) {
		return func(v *healView) { v.Run.Launch.Origin = origin }
	}

	wait := step{Kind: stepWait}
	park := step{Kind: stepPark}
	closeHeal := step{Kind: stepClose}

	tests := []struct {
		name      string
		view      healView
		want      step
		reasonHas []string // interpolated data the reason must carry
	}{
		// Rule 1: a rerun waits for its run and settles on its outcome.
		{"rerun not started within the grace", with(relaunched(9 * time.Minute)), wait, nil},
		{"rerun never started", with(relaunched(10 * time.Minute)), park, nil},
		{"rerun running under its own claim", with(rerunGoing, cardIs(board.StateInProgress, "running"), claimed), wait, nil},
		{"rerun queued", with(rerunGoing, cardIs(board.StateTodo, "queued")), wait, nil},
		{"rerun card went done", with(rerunStopped("done"), cardIs(board.StateDone, "completed")), step{Kind: stepSettle, Stage: state.StageHealed}, nil},
		{"rerun card done before its callback", with(rerunGoing, cardIs(board.StateDone, "completed")), step{Kind: stepSettle, Stage: state.StageHealed}, nil},
		{"rerun stopped done, card in review", with(rerunStopped("done"), cardIs(board.StateReview, "completed")), step{Kind: stepSettle, Stage: state.StageHealed}, nil},
		{"rerun failed", with(rerunStopped("verify_parked"), cardIs(board.StateInProgress, "failed")), step{Kind: stepSettle, Stage: state.StageRerunFailed}, nil},
		{"rerun parked before its callback", with(rerunGoing, cardIs(board.StateReview, "parked")), wait, nil},
		{"rerun card deleted", with(rerunGoing, deleted), closeHeal, nil},
		{"a REST Run after the relaunch", with(rerunStopped("done"), launchedFrom(state.OriginREST), cardIs(board.StateReview, "completed")), closeHeal, nil},
		{"an MCP run after the relaunch", with(rerunStopped("done"), launchedFrom(state.OriginMCP), cardIs(board.StateReview, "completed")), closeHeal, nil},
		{"a sentinel rerun settles", with(rerunStopped("done"), launchedFrom(state.OriginSentinel), cardIs(board.StateReview, "completed")), step{Kind: stepSettle, Stage: state.StageHealed}, nil},
		{"a playbook rerun settles", with(rerunStopped("verify_parked"), launchedFrom(state.OriginPlaybook), cardIs(board.StateInProgress, "failed")), step{Kind: stepSettle, Stage: state.StageRerunFailed}, nil},

		// Rule 2: the card is gone or finished.
		{"card deleted", with(deleted), closeHeal, nil},
		{"card done", with(cardIs(board.StateDone, "")), closeHeal, nil},
		{"card not planned", with(cardIs(board.StateNotPlanned, "")), closeHeal, nil},

		// Rule 3: someone else took the card.
		{"worker queued", with(cardIs(board.StateTodo, "queued")), closeHeal, nil},
		{"card claimed", with(claimed), closeHeal, nil},
		{"a newer run", with(newerRun), closeHeal, nil},

		// Rule 4: human edits win, even over a dispatch in flight.
		{
			"edited since launch", with(inFlight(protocol.RunKindInvestigate, 20*time.Minute), edited("title edited", `section "Plan" edited`)),
			park,
			[]string{"title edited", `section "Plan" edited`},
		},

		// Rule 5: a dispatch in flight.
		{"dispatch listed past the grace", with(inFlight(protocol.RunKindInvestigate, 20*time.Minute), listed), wait, nil},
		{"dispatch unlisted inside the grace", with(inFlight(protocol.RunKindInvestigate, 9*time.Minute)), wait, nil},
		{
			"investigate never reported", with(inFlight(protocol.RunKindInvestigate, 10*time.Minute)),
			step{Kind: stepPark, ChargeCeiling: true},
			[]string{protocol.RunKindInvestigate},
		},
		{
			"check never reported, healing off", with(atStage(state.StageCheck), inFlight(protocol.RunKindCheck, 11*time.Minute), healingOff),
			step{Kind: stepPark, ChargeCeiling: true},
			[]string{protocol.RunKindCheck},
		},

		// Rule 6: healing off holds every stage.
		{"healing off", with(healingOff), wait, nil},

		// Rule 7: stage investigate.
		{"investigate dispatched", with(), step{Kind: stepDispatch, RunKind: protocol.RunKindInvestigate}, nil},
		{"investigate waits for the budget", with(budgetShut), wait, nil},
		{"investigate waits for a worker slot", with(noRoom), wait, nil},
		{
			"investigation answered without a result", with(answeredWithout(protocol.RunKindInvestigate, "run log not found")),
			park,
			[]string{"run log not found"},
		},
		{
			"investigate_park parks with the diagnosis", with(response(RespInvestigatePark), investigated(protocol.DiagnosisCard, "the PR gates need a human", sectionFix)),
			park,
			[]string{"the PR gates need a human"},
		},
		{"card fix with a section", with(investigated(protocol.DiagnosisCard, "d", sectionFix)), step{Kind: stepAdvance, Stage: state.StageCheck}, nil},
		{"card fix with a verify", with(investigated(protocol.DiagnosisCard, "d", verifyFix)), step{Kind: stepAdvance, Stage: state.StageCheck}, nil},
		{"card fix that only retires", with(investigated(protocol.DiagnosisCard, "d", retireFix)), step{Kind: stepAdvance, Stage: state.StageApply}, nil},
		{"model diagnosis", with(investigated(protocol.DiagnosisModel, "d", nil)), step{Kind: stepAdvance, Stage: state.StageApply}, nil},
		{"environment diagnosis", with(investigated(protocol.DiagnosisEnvironment, "the worker image lacks protoc", nil)), park, []string{"the worker image lacks protoc"}},
		{"ecosystem diagnosis", with(investigated(protocol.DiagnosisEcosystem, "the agent drops the verify env", nil)), park, []string{"the agent drops the verify env"}},
		{"card diagnosis with no fix", with(investigated(protocol.DiagnosisCard, "d", nil)), park, nil},

		// Rule 8: stage check; it runs no model, so the budget does not hold it.
		{
			"check dispatched past a shut budget", with(atStage(state.StageCheck), investigated(protocol.DiagnosisCard, "d", verifyFix), budgetShut),
			step{Kind: stepDispatch, RunKind: protocol.RunKindCheck},
			nil,
		},
		{"check waits for a worker slot", with(atStage(state.StageCheck), investigated(protocol.DiagnosisCard, "d", verifyFix), noRoom), wait, nil},
		{
			"check answered without a result", with(atStage(state.StageCheck), investigated(protocol.DiagnosisCard, "d", verifyFix), answeredWithout(protocol.RunKindCheck, "clone failed")),
			park,
			[]string{"clone failed"},
		},
		{"check answered", with(atStage(state.StageCheck), investigated(protocol.DiagnosisCard, "d", verifyFix), checked), step{Kind: stepAdvance, Stage: state.StageApply}, nil},

		// Rule 9: stage apply.
		{"fix rejected", with(atStage(state.StageApply), rejected("verify not trial-run", "introduces x.example")), park, []string{"verify not trial-run", "introduces x.example"}},
		{"heal attempts used", with(atStage(state.StageApply), attempts(state.MaxHealAttempts)), park, []string{"2"}},
		{"apply waits for the budget", with(atStage(state.StageApply), attempts(1), budgetShut), wait, nil},
		{"apply", with(atStage(state.StageApply), attempts(1)), step{Kind: stepApply}, nil},

		// Rule 10: stage usage_wait.
		{"usage-limit reruns used", with(atStage(state.StageUsageWait), usageReruns(state.MaxUsageReruns), resetsIn(-time.Minute)), park, []string{"3"}},
		{"usage limit not reset", with(atStage(state.StageUsageWait), resetsIn(time.Minute)), wait, nil},
		{"usage rerun waits for the budget", with(atStage(state.StageUsageWait), resetsIn(-time.Minute), budgetShut), wait, nil},
		{"usage rerun", with(atStage(state.StageUsageWait), usageReruns(2), resetsIn(-time.Minute)), step{Kind: stepUsageRerun}, nil},

		// A parked heal's late answer reaches no stage rule.
		{"parked heal with a late answer", with(atStage(state.StageParked), investigated(protocol.DiagnosisCard, "d", sectionFix)), wait, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextStep(tt.view)
			reason := got.Reason
			got.Reason = ""

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.Kind == stepPark || tt.want.Kind == stepClose, reason != "", "reason %q", reason)

			for _, s := range tt.reasonHas {
				assert.Contains(t, reason, s)
			}
		})
	}
}

func TestHealFix(t *testing.T) {
	fix := &protocol.CardFix{RetireSubtasks: []string{"ALPHA-002"}}

	tests := []struct {
		name string
		inv  *protocol.InvestigateResult
		want *protocol.CardFix
	}{
		{"card diagnosis", &protocol.InvestigateResult{Cause: protocol.DiagnosisCard, Fix: fix}, fix},
		{"card diagnosis without a fix", &protocol.InvestigateResult{Cause: protocol.DiagnosisCard}, nil},
		{"model diagnosis carrying a fix", &protocol.InvestigateResult{Cause: protocol.DiagnosisModel, Fix: fix}, nil},
		{"environment diagnosis carrying a fix", &protocol.InvestigateResult{Cause: protocol.DiagnosisEnvironment, Fix: fix}, nil},
		{"not investigated", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Same(t, tt.want, healFix(state.Heal{Investigation: tt.inv}))
		})
	}
}
