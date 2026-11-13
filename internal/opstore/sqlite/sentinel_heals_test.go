package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// openDispatchedHeal opens an autonomous run of cardID started at runT0 and a
// heal for it at stage, with an investigate dispatch in flight on mode.
func openDispatchedHeal(t *testing.T, st *Store, cardID, stage, mode string) int64 {
	t.Helper()

	ctx := context.Background()
	require.NoError(t, st.OpenCardRun(ctx, "p", cardID, mode, runT0, sentinel.LaunchFacts{Autonomous: true}))

	at := runT0.Add(time.Minute)
	id, err := st.OpenHeal(ctx, sentinel.Heal{
		Kind: sentinel.HealKindHeal, Project: "p", CardID: cardID, RunStarted: runT0, Stage: stage,
		Dispatch:  sentinel.Dispatch{Kind: protocol.RunKindInvestigate, Mode: mode, CeilingUSD: 1.0, At: &at},
		CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)

	return id
}

func onlyHeal(t *testing.T, st *Store, cardID string) sentinel.Heal {
	t.Helper()

	heals, err := st.CardHeals(context.Background(), "p", cardID)
	require.NoError(t, err)
	require.Len(t, heals, 1)

	return heals[0]
}

func ledgerRows(t *testing.T, st *Store) int {
	t.Helper()

	var n int
	require.NoError(t, st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sentinel_spend`).Scan(&n))

	return n
}

func investigateReport(cardID string) sentinel.RunKindReport {
	return sentinel.RunKindReport{
		Project: "p", CardID: cardID, Kind: protocol.RunKindInvestigate, RunID: "k1",
		Result:   []byte(`{"diagnosis":"the verify command needs a longer timeout","cause":"card"}`),
		SpendUSD: 0.4, At: runT0.Add(2 * time.Minute),
	}
}

func TestOpenHealIsIdempotentPerRun(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, sentinel.LaunchFacts{Autonomous: true}))

	h := sentinel.Heal{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: sentinel.StageInvestigate}
	first, err := st.OpenHeal(ctx, h)
	require.NoError(t, err)

	h.Stage = sentinel.StageParked
	second, err := st.OpenHeal(ctx, h)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, sentinel.StageInvestigate, onlyHeal(t, st, "P-001").Stage)

	run, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.True(t, run.Decided)

	undecided, err := st.UndecidedRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, undecided)
}

func TestOpenHealKeepsEveryForwardRow(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", runT0, sentinel.LaunchFacts{Autonomous: true}))

	fwd := sentinel.Heal{Kind: sentinel.HealKindForward, Project: "p", CardID: "P-002", Stage: sentinel.StageForwarded, SourceCard: "P-001"}
	first, err := st.OpenHeal(ctx, fwd)
	require.NoError(t, err)
	second, err := st.OpenHeal(ctx, fwd)
	require.NoError(t, err)
	assert.NotEqual(t, first, second)

	run, err := st.CardRunRow(ctx, "p", "P-002")
	require.NoError(t, err)
	assert.False(t, run.Decided)
}

func TestOpenHealsReturnsOnlyOpenHeals(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	open := []string{sentinel.StageInvestigate, sentinel.StageCheck, sentinel.StageApply, sentinel.StageUsageWait, sentinel.StageRerun}
	done := []string{sentinel.StageHealed, sentinel.StageRerunFailed, sentinel.StageParked, sentinel.StageClosed}

	for i, stage := range slices.Concat(open, done) {
		_, err := st.OpenHeal(ctx, sentinel.Heal{
			Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Duration(i) * time.Hour), Stage: stage,
		})
		require.NoError(t, err)
	}

	_, err := st.OpenHeal(ctx, sentinel.Heal{Kind: sentinel.HealKindForward, Project: "p", CardID: "P-002", Stage: sentinel.StageForwarded})
	require.NoError(t, err)

	heals, err := st.OpenHeals(ctx)
	require.NoError(t, err)

	stages := make([]string, 0, len(heals))
	for _, h := range heals {
		assert.Equal(t, sentinel.HealKindHeal, h.Kind)
		stages = append(stages, h.Stage)
	}

	assert.ElementsMatch(t, open, stages)
}

func TestSaveHealRoundTripsEveryField(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id, err := st.OpenHeal(ctx, sentinel.Heal{
		Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0,
		Stage: sentinel.StageInvestigate, CreatedAt: runT0, UpdatedAt: runT0,
	})
	require.NoError(t, err)

	dispatched, relaunched := runT0.Add(time.Minute), runT0.Add(time.Hour)
	want := sentinel.Heal{
		ID: id, Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0,
		RunID: "r1", Cause: "verify_parked", Response: "investigate_fix", Stage: sentinel.StageRerun, Reason: "r",
		Dispatch: sentinel.Dispatch{
			Kind: protocol.RunKindCheck, Mode: "claude_code", CeilingUSD: 2, At: &dispatched,
			AnsweredRunID: "k2", Failure: "f",
		},
		Investigation: &protocol.InvestigateResult{
			Diagnosis: "d", Cause: protocol.DiagnosisCard, Fix: &protocol.CardFix{Section: "s"}, ForwardTo: []string{"P-009"},
		},
		Check:        &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished},
		SentSubtasks: []string{"P-002"},
		SentSiblings: []string{"P-003"},
		Change: &sentinel.Change{
			SectionBefore: "a", SectionAfter: "b",
			VerifyBefore: &board.VerifyConfig{Command: "make test"},
			VerifyAfter:  &board.VerifyConfig{Command: "make test", TimeoutSeconds: 1800},
			Retired:      []string{"P-002"}, Fresh: true,
		},
		RelaunchedAt: &relaunched, CostBaseUSD: 1.25, SourceCard: "P-000",
		CreatedAt: runT0, UpdatedAt: relaunched,
	}
	require.NoError(t, st.SaveHeal(ctx, &want))

	assert.Equal(t, int64(1), want.Version)
	assert.Equal(t, want, onlyHeal(t, st, "P-001"))
}

func TestConsecutiveSavesOfOneHealBothLand(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "endpoint")
	h := onlyHeal(t, st, "P-001")

	h.Dispatch.At = nil
	require.NoError(t, st.SaveHeal(ctx, &h))

	h.Stage, h.Reason = sentinel.StageParked, "the agent refused the investigate run"
	require.NoError(t, st.SaveHeal(ctx, &h))

	assert.Equal(t, int64(2), h.Version)
	assert.Equal(t, h, onlyHeal(t, st, "P-001"))
}

func TestSaveHealRefusesAStaleVersion(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "endpoint")
	stale := onlyHeal(t, st, "P-001")

	_, recorded, err := st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	require.True(t, recorded)

	stale.Stage = sentinel.StageParked
	stale.Dispatch.Failure = "the investigate run did not report"
	require.ErrorIs(t, st.SaveHeal(ctx, &stale), sentinel.ErrHealChanged)
	assert.Zero(t, stale.Version)

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, sentinel.StageInvestigate, h.Stage)
	assert.Equal(t, "k1", h.Dispatch.AnsweredRunID)
	assert.Empty(t, h.Dispatch.Failure)
	assert.NotNil(t, h.Investigation)

	// The agent's retry of the same status is still a repeat.
	_, recorded, err = st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, 1, ledgerRows(t, st))
	assert.InDelta(t, 0.4, h.SpendUSD, 1e-9)
}

func TestCardHealsNewestFirstWithSpend(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	var ids []int64

	for _, started := range []time.Time{runT0, runT0.Add(time.Hour)} {
		id, err := st.OpenHeal(ctx, sentinel.Heal{
			Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: started, Stage: sentinel.StageParked,
		})
		require.NoError(t, err)

		ids = append(ids, id)
	}

	_, err := st.OpenHeal(ctx, sentinel.Heal{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0})
	require.NoError(t, err)

	require.NoError(t, st.Charge(ctx, ids[0], "endpoint", 0.5, runT0))
	require.NoError(t, st.Charge(ctx, ids[0], "claude_code", 0.25, runT0))
	require.NoError(t, st.Charge(ctx, ids[1], "endpoint", 1, runT0))

	heals, err := st.CardHeals(ctx, "p", "P-001")
	require.NoError(t, err)
	require.Len(t, heals, 2)
	assert.Equal(t, ids[1], heals[0].ID)
	assert.InDelta(t, 1.0, heals[0].SpendUSD, 1e-9)
	assert.Equal(t, ids[0], heals[1].ID)
	assert.InDelta(t, 0.75, heals[1].SpendUSD, 1e-9)
}

func TestLatestHealsPerCard(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	for _, h := range []sentinel.Heal{
		{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: sentinel.StageHealed},
		{Kind: sentinel.HealKindForward, Project: "p", CardID: "P-001", Stage: sentinel.StageForwarded, SourceCard: "P-009"},
		{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0, Stage: sentinel.StageParked},
		{Kind: sentinel.HealKindHeal, Project: "q", CardID: "Q-001", RunStarted: runT0, Stage: sentinel.StageParked},
	} {
		_, err := st.OpenHeal(ctx, h)
		require.NoError(t, err)
	}

	got, err := st.LatestHeals(ctx, "p")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, sentinel.HealKindForward, got["P-001"].Kind)
	assert.Equal(t, "P-009", got["P-001"].SourceCard)
	assert.Equal(t, sentinel.StageParked, got["P-002"].Stage)
}

func TestRecordRunKindStatusChargesOnce(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id := openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "endpoint")

	healID, recorded, err := st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, id, healID)

	healID, recorded, err = st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Zero(t, healID)

	assert.Equal(t, 1, ledgerRows(t, st))

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, "k1", h.Dispatch.AnsweredRunID)
	assert.Empty(t, h.Dispatch.Failure)
	assert.Equal(t, &protocol.InvestigateResult{
		Diagnosis: "the verify command needs a longer timeout", Cause: protocol.DiagnosisCard,
	}, h.Investigation)
	assert.Nil(t, h.Check)
	assert.Equal(t, sentinel.StageInvestigate, h.Stage)
	assert.InDelta(t, 0.4, h.SpendUSD, 1e-9)
}

func TestRecordRunKindStatusStoresCheckResult(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", sentinel.StageCheck, "endpoint")

	h := onlyHeal(t, st, "P-001")
	h.Dispatch.Kind = protocol.RunKindCheck
	require.NoError(t, st.SaveHeal(ctx, &h))

	_, recorded, err := st.RecordRunKindStatus(ctx, sentinel.RunKindReport{
		Project: "p", CardID: "P-001", Kind: protocol.RunKindCheck, RunID: "k2",
		Result: []byte(`{"candidates":["make test"],"trial":"finished"}`), At: runT0.Add(2 * time.Minute),
	})
	require.NoError(t, err)
	assert.True(t, recorded)

	h = onlyHeal(t, st, "P-001")
	assert.Equal(t, &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished}, h.Check)
	assert.Nil(t, h.Investigation)
	assert.Equal(t, 1, ledgerRows(t, st))
}

func TestRecordRunKindStatusMatchesOnlyAnUnansweredDispatch(t *testing.T) {
	tests := []struct {
		name   string
		heal   func(h *sentinel.Heal)
		report func(r *sentinel.RunKindReport)
	}{
		{"dispatch parked as silent", func(h *sentinel.Heal) {
			h.Stage = sentinel.StageParked
			h.Dispatch.Failure = "the investigate run did not report"
		}, nil},
		{"another kind in flight", nil, func(r *sentinel.RunKindReport) { r.Kind = protocol.RunKindCheck }},
		{
			"nothing dispatched", func(h *sentinel.Heal) { h.Dispatch = sentinel.Dispatch{} },
			func(r *sentinel.RunKindReport) { r.Kind = "" },
		},
		{"another card", nil, func(r *sentinel.RunKindReport) { r.CardID = "P-404" }},
		{"no run id", nil, func(r *sentinel.RunKindReport) { r.RunID = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "endpoint")

			if tt.heal != nil {
				h := onlyHeal(t, st, "P-001")
				tt.heal(&h)
				require.NoError(t, st.SaveHeal(ctx, &h))
			}

			r := investigateReport("P-001")
			if tt.report != nil {
				tt.report(&r)
			}

			healID, recorded, err := st.RecordRunKindStatus(ctx, r)
			require.NoError(t, err)
			assert.False(t, recorded)
			assert.Zero(t, healID)
			assert.Zero(t, ledgerRows(t, st))
			assert.Empty(t, onlyHeal(t, st, "P-001").Dispatch.AnsweredRunID)
		})
	}
}

func TestRecordRunKindStatusAnswersTheNewestDispatch(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	older := openDispatchedHeal(t, st, "P-001", sentinel.StageClosed, "endpoint")

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour), sentinel.LaunchFacts{Autonomous: true}))

	at := runT0.Add(time.Hour + time.Minute)
	newer, err := st.OpenHeal(ctx, sentinel.Heal{
		Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Hour), Stage: sentinel.StageInvestigate,
		Dispatch: sentinel.Dispatch{Kind: protocol.RunKindInvestigate, Mode: "endpoint", CeilingUSD: 1, At: &at},
	})
	require.NoError(t, err)

	healID, recorded, err := st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, newer, healID)

	r := investigateReport("P-001")
	r.RunID = "k0"
	healID, recorded, err = st.RecordRunKindStatus(ctx, r)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, older, healID)
}

func TestRecordRunKindStatusOnParkedHealStillCharges(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id := openDispatchedHeal(t, st, "P-001", sentinel.StageParked, "endpoint")

	r := investigateReport("P-001")
	r.Result, r.Failure, r.SpendUSD = nil, "infrastructure", 0.3

	healID, recorded, err := st.RecordRunKindStatus(ctx, r)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, id, healID)

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, sentinel.StageParked, h.Stage)
	assert.Equal(t, "k1", h.Dispatch.AnsweredRunID)
	assert.Equal(t, "infrastructure", h.Dispatch.Failure)
	assert.Nil(t, h.Investigation)

	spent, err := st.SpentSince(ctx, "endpoint", runT0)
	require.NoError(t, err)
	assert.InDelta(t, 0.3, spent, 1e-9)
}

func TestRecordRunKindStatusCeilingCharge(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "claude_code")

	r := investigateReport("P-001")
	r.Result, r.Failure, r.SpendUSD, r.ChargeCeiling = nil, "killed", 0, true

	_, recorded, err := st.RecordRunKindStatus(ctx, r)
	require.NoError(t, err)
	assert.True(t, recorded)

	spent, err := st.SpentSince(ctx, "claude_code", runT0)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, spent, 1e-9)
	assert.InDelta(t, 1.0, onlyHeal(t, st, "P-001").SpendUSD, 1e-9)

	endpoint, err := st.SpentSince(ctx, "endpoint", runT0)
	require.NoError(t, err)
	assert.Zero(t, endpoint)
}

func TestSpentSinceSumsOneMode(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id := openDispatchedHeal(t, st, "P-001", sentinel.StageInvestigate, "endpoint")

	require.NoError(t, st.Charge(ctx, id, "endpoint", 1.5, runT0))
	require.NoError(t, st.Charge(ctx, id, "claude_code", 4, runT0))
	require.NoError(t, st.Charge(ctx, id, "endpoint", 2, runT0.Add(-time.Nanosecond)))

	spent, err := st.SpentSince(ctx, "endpoint", runT0)
	require.NoError(t, err)
	assert.InDelta(t, 1.5, spent, 1e-9)

	none, err := st.SpentSince(ctx, "endpoint", runT0.Add(time.Nanosecond))
	require.NoError(t, err)
	assert.Zero(t, none)
}

func TestHealedSinceCountsCards(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	settle := func(cardID string, started time.Time, stage string, at time.Time) {
		t.Helper()

		h := sentinel.Heal{Kind: sentinel.HealKindHeal, Project: "p", CardID: cardID, RunStarted: started, Stage: sentinel.StageRerun}
		id, err := st.OpenHeal(ctx, h)
		require.NoError(t, err)

		h.ID, h.Stage, h.UpdatedAt = id, stage, at
		require.NoError(t, st.SaveHeal(ctx, &h))
	}

	settle("P-001", runT0, sentinel.StageHealed, runT0.Add(time.Hour))
	settle("P-001", runT0.Add(time.Hour), sentinel.StageHealed, runT0.Add(2*time.Hour))
	settle("P-002", runT0, sentinel.StageHealed, runT0)
	settle("P-003", runT0, sentinel.StageHealed, runT0.Add(-time.Nanosecond))
	settle("P-004", runT0, sentinel.StageRerunFailed, runT0.Add(time.Hour))

	n, err := st.HealedSince(ctx, runT0)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestCardFindingsNewestHealFirst(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	var ids []int64

	for _, h := range []sentinel.Heal{
		{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0},
		{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Hour)},
		{Kind: sentinel.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0},
	} {
		id, err := st.OpenHeal(ctx, h)
		require.NoError(t, err)

		ids = append(ids, id)
	}

	guidance := protocol.FrictionFinding{Kind: protocol.FindingGuidance, Text: "run make fmt first", Evidence: "lint failed twice"}
	code := protocol.FrictionFinding{Kind: protocol.FindingCode, Title: "Split the config loader", Text: "body", Evidence: "e"}
	hard := protocol.FrictionFinding{Kind: protocol.FindingHardCard, Text: "too broad", Evidence: "e"}
	t1, t2 := runT0.Add(time.Minute), runT0.Add(time.Hour+time.Minute)

	require.NoError(t, st.AddFindings(ctx, ids[0], "p", "P-001", []protocol.FrictionFinding{guidance, code}, t1))
	require.NoError(t, st.AddFindings(ctx, ids[1], "p", "P-001", []protocol.FrictionFinding{hard}, t2))
	require.NoError(t, st.AddFindings(ctx, ids[2], "p", "P-002", []protocol.FrictionFinding{hard}, t2))
	require.NoError(t, st.AddFindings(ctx, ids[0], "p", "P-001", nil, t2))

	got, err := st.CardFindings(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.Equal(t, []sentinel.Finding{
		{HealID: ids[1], Kind: hard.Kind, Text: hard.Text, Evidence: hard.Evidence, CreatedAt: t2},
		{HealID: ids[0], Kind: guidance.Kind, Text: guidance.Text, Evidence: guidance.Evidence, CreatedAt: t1},
		{HealID: ids[0], Kind: code.Kind, Title: code.Title, Text: code.Text, Evidence: code.Evidence, CreatedAt: t1},
	}, got)
}
