package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

// openDispatchedHeal opens an autonomous run of cardID started at runT0 and a
// heal for it at stage, with an investigate dispatch in flight on mode.
func openDispatchedHeal(t *testing.T, st *Store, cardID, stage, mode string) int64 {
	t.Helper()

	ctx := context.Background()
	require.NoError(t, st.OpenCardRun(ctx, "p", cardID, mode, runT0, state.LaunchFacts{Autonomous: true}))

	at := runT0.Add(time.Minute)
	id, err := st.OpenHeal(ctx, state.Heal{
		Kind: state.HealKindHeal, Project: "p", CardID: cardID, RunStarted: runT0, Stage: stage,
		Dispatch:  state.Dispatch{Kind: protocol.RunKindInvestigate, Mode: mode, CeilingUSD: 1.0, At: &at},
		CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)

	return id
}

func onlyHeal(t *testing.T, st *Store, cardID string) state.Heal {
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

func investigateReport(cardID string) state.RunKindReport {
	return state.RunKindReport{
		Project: "p", CardID: cardID, Kind: protocol.RunKindInvestigate, RunID: "k1",
		Result:   []byte(`{"diagnosis":"the verify command needs a longer timeout","cause":"card"}`),
		SpendUSD: 0.4, At: runT0.Add(2 * time.Minute),
	}
}

func TestOpenHealIsIdempotentPerRun(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{Autonomous: true}))

	h := state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: state.StageInvestigate}
	first, err := st.OpenHeal(ctx, h)
	require.NoError(t, err)

	h.Stage = state.StageParked
	second, err := st.OpenHeal(ctx, h)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, state.StageInvestigate, onlyHeal(t, st, "P-001").Stage)

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

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", runT0, state.LaunchFacts{Autonomous: true}))

	fwd := state.Heal{Kind: state.HealKindForward, Project: "p", CardID: "P-002", Stage: state.StageForwarded, SourceCard: "P-001"}
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

	open := []string{state.StageInvestigate, state.StageCheck, state.StageApply, state.StageUsageWait, state.StageRerun}
	done := state.TerminalStages

	for i, stage := range slices.Concat(open, done) {
		_, err := st.OpenHeal(ctx, state.Heal{
			Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Duration(i) * time.Hour), Stage: stage,
		})
		require.NoError(t, err)
	}

	_, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindForward, Project: "p", CardID: "P-002", Stage: state.StageForwarded})
	require.NoError(t, err)

	heals, err := st.OpenHeals(ctx)
	require.NoError(t, err)

	stages := make([]string, 0, len(heals))
	for _, h := range heals {
		assert.Equal(t, state.HealKindHeal, h.Kind)
		stages = append(stages, h.Stage)
	}

	assert.ElementsMatch(t, open, stages)
}

func TestInFlightHealsAcrossStages(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	open := openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")
	closed := openDispatchedHeal(t, st, "P-002", state.StageClosed, "endpoint")
	parked := openDispatchedHeal(t, st, "P-003", state.StageParked, "claude_code")

	for _, edit := range []struct {
		card string
		edit func(*state.Heal)
	}{
		{"P-004", func(h *state.Heal) { h.Dispatch.AnsweredRunID = "k4" }},
		{"P-005", func(h *state.Heal) { h.Dispatch.Failure = "the investigate run did not report" }},
		{"P-006", func(h *state.Heal) { h.Dispatch = state.Dispatch{} }},
	} {
		openDispatchedHeal(t, st, edit.card, state.StageParked, "endpoint")

		h := onlyHeal(t, st, edit.card)
		edit.edit(&h)
		require.NoError(t, st.SaveHeal(ctx, &h))
	}

	heals, err := st.InFlightHeals(ctx)
	require.NoError(t, err)

	ids := make([]int64, 0, len(heals))
	for _, h := range heals {
		ids = append(ids, h.ID)
	}

	assert.Equal(t, []int64{open, closed, parked}, ids, "every stage, oldest first; answered, failed and cleared dispatches are not in flight")
}

func TestSaveHealRoundTripsEveryField(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id, err := st.OpenHeal(ctx, state.Heal{
		Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0,
		Stage: state.StageInvestigate, CreatedAt: runT0, UpdatedAt: runT0,
	})
	require.NoError(t, err)

	dispatched, relaunched := runT0.Add(time.Minute), runT0.Add(time.Hour)
	want := state.Heal{
		ID: id, Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0,
		RunID: "r1", Cause: "verify_parked", Response: "investigate_fix", Stage: state.StageRerun, Reason: "r",
		Dispatch: state.Dispatch{
			Kind: protocol.RunKindCheck, Mode: "claude_code", CeilingUSD: 2, At: &dispatched,
			AnsweredRunID: "k2", Failure: "f",
		},
		Investigation: &protocol.InvestigateResult{
			Diagnosis: "d", Cause: protocol.DiagnosisCard, Fix: &protocol.CardFix{Section: "s"}, ForwardTo: []string{"P-009"},
		},
		Check:        &protocol.CheckResult{Candidates: []string{"make test"}, Trial: protocol.TrialFinished},
		SentSubtasks: []string{"P-002"},
		SentSiblings: []string{"P-003"},
		Change: &state.Change{
			SectionBefore: "a", SectionAfter: "b",
			VerifyBefore: &board.VerifyConfig{Command: "make test"},
			VerifyAfter:  &board.VerifyConfig{Command: "make test", TimeoutSeconds: 1800},
			Retired:      []string{"P-002"}, Fresh: true,
		},
		Dropped:      []string{"introduces gen-tool"},
		RelaunchedAt: &relaunched, CostBaseUSD: 1.25, SourceCard: "P-000",
		CreatedAt: runT0, UpdatedAt: relaunched,
	}
	require.NoError(t, st.SaveHeal(ctx, &want))

	assert.Equal(t, int64(1), want.Version)
	assert.Equal(t, want, onlyHeal(t, st, "P-001"))
}

// The column needs no schema bump: a database without it grows it and keeps
// its version, and the heal an older image writes, naming no dropped, reads
// back with nothing dropped.
func TestSentinelHealsGrowDroppedWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("ALTER TABLE sentinel_heals DROP COLUMN dropped")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	assert.True(t, columnExists(t, st.db, "sentinel_heals", "dropped"))

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	// OpenHeal's and SaveHeal's statements as the older image runs them, verbatim.
	const v6Columns = `run_id, cause, response, stage, reason,
	dispatch_kind, dispatch_mode, dispatch_ceiling_usd, dispatch_at, dispatch_answered_run_id, dispatch_failure,
	investigation, check_result, sent_subtasks, sent_siblings, change,
	relaunched_at, cost_base_usd, source_card, updated_at`

	const v6Params = `?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`

	ctx := context.Background()
	vals := []any{
		"r1", "verify_parked", "investigate_fix", state.StageApply, "", "", "", 0.0, nil, "", "",
		"null", "null", "null", "null", "null", nil, 0.0, "", runT0.UnixNano(),
	}

	_, err = st.db.ExecContext(ctx,
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+v6Columns+`)
		 VALUES (?, ?, ?, ?, ?, `+v6Params+`)`,
		append([]any{state.HealKindHeal, "p", "P-001", runT0.UnixNano(), runT0.UnixNano()}, vals...)...)
	require.NoError(t, err)

	vals[3] = state.StageParked
	_, err = st.db.ExecContext(ctx,
		`UPDATE sentinel_heals SET (`+v6Columns+`) = (`+v6Params+`), version = version + 1 WHERE id = ? AND version = ?`,
		append(vals, 1, 0)...)
	require.NoError(t, err)

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, state.StageParked, h.Stage)
	assert.Nil(t, h.Dropped)
}

func TestConsecutiveSavesOfOneHealBothLand(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")
	h := onlyHeal(t, st, "P-001")

	h.Dispatch.At = nil
	require.NoError(t, st.SaveHeal(ctx, &h))

	h.Stage, h.Reason = state.StageParked, "the agent refused the investigate run"
	require.NoError(t, st.SaveHeal(ctx, &h))

	assert.Equal(t, int64(2), h.Version)
	assert.Equal(t, h, onlyHeal(t, st, "P-001"))
}

func TestSaveHealRefusesAStaleVersion(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")
	stale := onlyHeal(t, st, "P-001")

	_, recorded, err := st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	require.True(t, recorded)

	stale.Stage = state.StageParked
	stale.Dispatch.Failure = "the investigate run did not report"
	require.ErrorIs(t, st.SaveHeal(ctx, &stale), state.ErrHealChanged)
	assert.Zero(t, stale.Version)

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, state.StageInvestigate, h.Stage)
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
		id, err := st.OpenHeal(ctx, state.Heal{
			Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: started, Stage: state.StageParked,
		})
		require.NoError(t, err)

		ids = append(ids, id)
	}

	_, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0})
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

	for _, h := range []state.Heal{
		{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: state.StageHealed},
		{Kind: state.HealKindForward, Project: "p", CardID: "P-001", Stage: state.StageForwarded, SourceCard: "P-009"},
		{Kind: state.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0, Stage: state.StageParked},
		{Kind: state.HealKindHeal, Project: "q", CardID: "Q-001", RunStarted: runT0, Stage: state.StageParked},
	} {
		_, err := st.OpenHeal(ctx, h)
		require.NoError(t, err)
	}

	got, err := st.LatestHeals(ctx, "p")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, state.HealKindForward, got["P-001"].Kind)
	assert.Equal(t, "P-009", got["P-001"].SourceCard)
	assert.Equal(t, state.StageParked, got["P-002"].Stage)
}

func TestRecordRunKindStatusChargesOnce(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id := openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")

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
	assert.Equal(t, state.StageInvestigate, h.Stage)
	assert.InDelta(t, 0.4, h.SpendUSD, 1e-9)
}

func TestRecordRunKindStatusStoresCheckResult(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	openDispatchedHeal(t, st, "P-001", state.StageCheck, "endpoint")

	h := onlyHeal(t, st, "P-001")
	h.Dispatch.Kind = protocol.RunKindCheck
	require.NoError(t, st.SaveHeal(ctx, &h))

	_, recorded, err := st.RecordRunKindStatus(ctx, state.RunKindReport{
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
		heal   func(h *state.Heal)
		report func(r *state.RunKindReport)
	}{
		{"dispatch parked as silent", func(h *state.Heal) {
			h.Stage = state.StageParked
			h.Dispatch.Failure = "the investigate run did not report"
		}, nil},
		{"another kind in flight", nil, func(r *state.RunKindReport) { r.Kind = protocol.RunKindCheck }},
		{
			"nothing dispatched", func(h *state.Heal) { h.Dispatch = state.Dispatch{} },
			func(r *state.RunKindReport) { r.Kind = "" },
		},
		{"another card", nil, func(r *state.RunKindReport) { r.CardID = "P-404" }},
		{"no run id", nil, func(r *state.RunKindReport) { r.RunID = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")

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

// openOlderAndNewerDispatch leaves P-001 with a closed heal whose investigate
// dispatch never answered, and a newer heal with one in flight.
func openOlderAndNewerDispatch(t *testing.T, st *Store) (older, newer int64) {
	t.Helper()

	ctx := context.Background()
	older = openDispatchedHeal(t, st, "P-001", state.StageClosed, "endpoint")

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour), state.LaunchFacts{Autonomous: true}))

	at := runT0.Add(time.Hour + time.Minute)
	newer, err := st.OpenHeal(ctx, state.Heal{
		Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Hour), Stage: state.StageInvestigate,
		Dispatch: state.Dispatch{Kind: protocol.RunKindInvestigate, Mode: "endpoint", CeilingUSD: 1, At: &at},
	})
	require.NoError(t, err)

	return older, newer
}

func healByID(t *testing.T, st *Store, id int64) state.Heal {
	t.Helper()

	heals, err := st.CardHeals(context.Background(), "p", "P-001")
	require.NoError(t, err)

	for _, h := range heals {
		if h.ID == id {
			return h
		}
	}

	require.FailNow(t, "no heal", "id %d", id)

	return state.Heal{}
}

func TestRecordRunKindStatusAnswersTheNewestDispatch(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	older, newer := openOlderAndNewerDispatch(t, st)

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

func TestRecordRunKindStatusRepeatSkipsAnOlderOpenDispatch(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	older, newer := openOlderAndNewerDispatch(t, st)
	before := healByID(t, st, older)

	healID, recorded, err := st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, newer, healID)

	healID, recorded, err = st.RecordRunKindStatus(ctx, investigateReport("P-001"))
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Zero(t, healID)
	assert.Equal(t, 1, ledgerRows(t, st))
	assert.Equal(t, before, healByID(t, st, older))
}

func TestRecordRunKindStatusOnParkedHealStillCharges(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	id := openDispatchedHeal(t, st, "P-001", state.StageParked, "endpoint")

	r := investigateReport("P-001")
	r.Result, r.Failure, r.SpendUSD = nil, "infrastructure", 0.3

	healID, recorded, err := st.RecordRunKindStatus(ctx, r)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, id, healID)

	h := onlyHeal(t, st, "P-001")
	assert.Equal(t, state.StageParked, h.Stage)
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

	openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "claude_code")

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

	id := openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")

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

		h := state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: cardID, RunStarted: started, Stage: state.StageRerun}
		id, err := st.OpenHeal(ctx, h)
		require.NoError(t, err)

		h.ID, h.Stage, h.UpdatedAt = id, stage, at
		require.NoError(t, st.SaveHeal(ctx, &h))
	}

	settle("P-001", runT0, state.StageHealed, runT0.Add(time.Hour))
	settle("P-001", runT0.Add(time.Hour), state.StageHealed, runT0.Add(2*time.Hour))
	settle("P-002", runT0, state.StageHealed, runT0)
	settle("P-003", runT0, state.StageHealed, runT0.Add(-time.Nanosecond))
	settle("P-004", runT0, state.StageRerunFailed, runT0.Add(time.Hour))

	n, err := st.HealedSince(ctx, runT0)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestCardFindingsNewestHealFirst(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	var ids []int64

	for _, h := range []state.Heal{
		{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0},
		{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0.Add(time.Hour)},
		{Kind: state.HealKindHeal, Project: "p", CardID: "P-002", RunStarted: runT0},
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
	assert.Equal(t, []state.Finding{
		{
			ID: got[0].ID, HealID: ids[1], Project: "p", CardID: "P-001", Kind: hard.Kind, Text: hard.Text, Evidence: hard.Evidence,
			State: state.FindingNote, CreatedAt: t2, UpdatedAt: t2,
		},
		{
			ID: got[1].ID, HealID: ids[0], Project: "p", CardID: "P-001", Kind: guidance.Kind, Text: guidance.Text, Evidence: guidance.Evidence,
			State: state.FindingQueued, CreatedAt: t1, UpdatedAt: t1,
		},
		{
			ID: got[2].ID, HealID: ids[0], Project: "p", CardID: "P-001", Kind: code.Kind, Title: code.Title, Text: code.Text,
			Evidence: code.Evidence, State: state.FindingQueued, CreatedAt: t1, UpdatedAt: t1,
		},
	}, got)
	assert.NotZero(t, got[0].ID)
}

func TestRecordRunKindStatusStoresAnalyseAndGuidanceResults(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	at := runT0.Add(time.Minute)
	open := func(cardID, kind string) {
		_, err := st.OpenHeal(ctx, state.Heal{
			Kind: state.HealKindHeal, Project: "p", CardID: cardID, RunStarted: runT0, Stage: state.StageInvestigate,
			Dispatch:  state.Dispatch{Kind: kind, Mode: "endpoint", CeilingUSD: 1, At: &at},
			CreatedAt: at, UpdatedAt: at,
		})
		require.NoError(t, err)
	}

	open("P-001", protocol.RunKindAnalyse)
	open("", protocol.RunKindGuidancePR)

	analysis := protocol.AnalyseResult{Findings: []protocol.FrictionFinding{{Kind: protocol.FindingCode, Text: "t", Evidence: "e"}}}
	revision := protocol.GuidancePRResult{Outcome: protocol.GuidancePushed, PRURL: "https://x/pull/1", HeadSHA: "abc", File: "AGENTS.md"}

	for _, r := range []struct {
		report state.RunKindReport
		result any
	}{
		{state.RunKindReport{Project: "p", CardID: "P-001", Kind: protocol.RunKindAnalyse, RunID: "a1"}, analysis},
		{state.RunKindReport{Project: "p", CardID: "", Kind: protocol.RunKindGuidancePR, RunID: "g1"}, revision},
	} {
		var err error

		r.report.Result, err = json.Marshal(r.result)
		require.NoError(t, err)

		r.report.At = at

		_, recorded, err := st.RecordRunKindStatus(ctx, r.report)
		require.NoError(t, err)
		require.True(t, recorded)
	}

	a := onlyHeal(t, st, "P-001")
	assert.Equal(t, &analysis, a.Analysis)
	assert.Nil(t, a.Revision)
	assert.Equal(t, "a1", a.Dispatch.AnsweredRunID)

	g := onlyHeal(t, st, "")
	assert.Equal(t, &revision, g.Revision)
	assert.Nil(t, g.Analysis)
}

// The result column needs no schema bump: a database without it grows it and
// keeps its version, and a row an older image wrote reads back with no result.
func TestSentinelHealsGrowResultWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("ALTER TABLE sentinel_heals DROP COLUMN result")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	assert.True(t, columnExists(t, st.db, "sentinel_heals", "result"))

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")

	h := onlyHeal(t, st, "P-001")
	assert.Nil(t, h.Analysis)
	assert.Nil(t, h.Revision)
}

func TestSentinelFindingsGrowStateColumnsWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)

	for _, col := range []string{"state", "reason", "output", "revision_id", "updated_at"} {
		_, err = db.Exec("ALTER TABLE sentinel_findings DROP COLUMN " + col)
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	for _, col := range []string{"state", "reason", "output", "revision_id", "updated_at"} {
		assert.True(t, columnExists(t, st.db, "sentinel_findings", col), col)
	}

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	// A finding row as the older image inserts it.
	id := openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")
	_, err = st.db.Exec(`INSERT INTO sentinel_findings (heal_id, project, card_id, kind, title, text, evidence, created_at)
		VALUES (?, 'p', 'P-001', 'code', 't', 'x', 'e', ?)`, id, runT0.UnixNano())
	require.NoError(t, err)

	got, err := st.CardFindings(context.Background(), "p", "P-001")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].State)
	assert.Zero(t, got[0].RevisionID)
	assert.True(t, got[0].UpdatedAt.IsZero())
}
