package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
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

func TestInFlightRowsAcrossStages(t *testing.T) {
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

	heals, err := st.InFlightRows(ctx)
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
		SentFindings: []int64{3, 4},
		Allowed:      []string{"deploy.example.net"},
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

// analysisRow is the analysis row QueueAnalysis stores for runID.
func analysisRow(cardID, runID string, started time.Time) state.Heal {
	return state.Heal{
		Kind: state.HealKindAnalysis, Project: "p", CardID: cardID, RunStarted: started, RunID: runID,
		Cause: "done", Stage: state.StageAnalyse, CreatedAt: started, UpdatedAt: started,
	}
}

func TestQueueAnalysisIsIdempotentPerRunID(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{Autonomous: true}))
	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", "r1", "running", "", runT0))

	require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-001", "r1", runT0)))

	again := analysisRow("P-001", "r1", runT0)
	again.Cause = "other"
	require.NoError(t, st.QueueAnalysis(ctx, again))

	rows, err := st.OpenAnalyses(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "r1", rows[0].RunID)
	assert.Equal(t, "done", rows[0].Cause, "the first row stands")
	assert.True(t, rows[0].RunStarted.Equal(runT0))

	run, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.True(t, run.AnalysisQueued)

	unqueued, err := st.UnqueuedRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unqueued)
}

func TestUnqueuedRunsNeedAnIDAndNoQueueMark(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	for _, id := range []string{"P-001", "P-002", "P-003"} {
		require.NoError(t, st.OpenCardRun(ctx, "p", id, "endpoint", runT0, state.LaunchFacts{}))
	}

	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", "r1", "running", "", runT0))
	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-002", "r2", "running", "", runT0))
	require.NoError(t, st.MarkAnalysisQueued(ctx, "p", "P-002", runT0))

	runs, err := st.UnqueuedRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1, "P-002 is queued and P-003 has no run ID")
	assert.Equal(t, "P-001", runs[0].CardID)

	// A mark for an earlier run leaves the current one unqueued.
	require.NoError(t, st.MarkAnalysisQueued(ctx, "p", "P-001", runT0.Add(-time.Hour)))

	runs, err = st.UnqueuedRuns(ctx)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
}

func TestCardAnalysesNewestFirst(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	for i, runID := range []string{"r1", "r2", "r3"} {
		require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-001", runID, runT0.Add(time.Duration(i)*time.Hour))))
	}

	require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-002", "r4", runT0)))

	rows, err := st.CardAnalyses(ctx, "p", "P-001")
	require.NoError(t, err)

	ids := make([]string, 0, len(rows))
	for _, h := range rows {
		ids = append(ids, h.RunID)
	}

	assert.Equal(t, []string{"r3", "r2", "r1"}, ids)
}

func TestCardHealsAndLatestHealsIgnoreAnalyses(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	parked := state.Heal{
		Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: state.StageParked,
		CreatedAt: runT0, UpdatedAt: runT0,
	}
	_, err := st.OpenHeal(ctx, parked)
	require.NoError(t, err)
	require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-001", "r2", runT0.Add(time.Hour))))

	heals, err := st.CardHeals(ctx, "p", "P-001")
	require.NoError(t, err)
	require.Len(t, heals, 1)
	assert.Equal(t, state.HealKindHeal, heals[0].Kind)

	latest, err := st.LatestHeals(ctx, "p")
	require.NoError(t, err)
	require.Len(t, latest, 1)
	assert.Equal(t, state.StageParked, latest["P-001"].Stage)
}

func TestHealedSinceCountsHealsOnly(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	h := state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: state.StageRerun}
	id, err := st.OpenHeal(ctx, h)
	require.NoError(t, err)

	h.ID, h.Stage, h.UpdatedAt = id, state.StageHealed, runT0.Add(time.Hour)
	require.NoError(t, st.SaveHeal(ctx, &h))

	// A row of another kind that carries a heal's terminal stage.
	other := analysisRow("P-002", "r2", runT0)
	other.Stage, other.UpdatedAt = state.StageHealed, runT0.Add(time.Hour)
	insertRow(t, st, other)

	n, err := st.HealedSince(ctx, runT0)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestRunHealIsTheRunsHeal(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	none, err := st.RunHeal(ctx, "p", "P-001", runT0)
	require.NoError(t, err)
	assert.Nil(t, none)

	require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-001", "r1", runT0)))

	none, err = st.RunHeal(ctx, "p", "P-001", runT0)
	require.NoError(t, err)
	assert.Nil(t, none, "an analysis of the run is no heal")

	id, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0, Stage: state.StageParked})
	require.NoError(t, err)

	got, err := st.RunHeal(ctx, "p", "P-001", runT0)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got.ID)
}

// insertRow stores h as is, whatever its kind, the way no public method does
// for a revision row.
func insertRow(t *testing.T, st *Store, h state.Heal) {
	t.Helper()

	vals, err := healValues(h)
	require.NoError(t, err)

	args := append([]any{h.Kind, h.Project, h.CardID, unixNano(h.RunStarted), unixNano(h.CreatedAt)}, vals...)
	_, err = st.db.ExecContext(context.Background(),
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+healMutable+`)
		 VALUES (?, ?, ?, ?, ?, `+healMutableParams+`)`, args...)
	require.NoError(t, err)
}

func TestEveryRowReaderReturnsOnlyItsKinds(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	// Each kind on its own card, one row open and one in a terminal stage, both
	// with a dispatch in flight.
	at := runT0.Add(time.Minute)
	seeds := []struct {
		kind, card, dispatch, open, done string
	}{
		{state.HealKindHeal, "P-001", protocol.RunKindInvestigate, state.StageInvestigate, state.StageHealed},
		{state.HealKindForward, "P-002", protocol.RunKindCheck, state.StageForwarded, state.StageForwardPassed},
		{state.HealKindAnalysis, "P-003", protocol.RunKindAnalyse, state.StageAnalyse, state.StageAnalysed},
		{state.HealKindRevision, "", protocol.RunKindGuidancePR, state.StageRevise, state.StageRevisionRefused},
	}
	cards := make([]string, 0, len(seeds))

	for _, sd := range seeds {
		cards = append(cards, sd.card)

		for i, stage := range []string{sd.open, sd.done} {
			insertRow(t, st, state.Heal{
				Kind: sd.kind, Project: "p", CardID: sd.card, RunStarted: runT0.Add(time.Duration(i) * time.Hour),
				RunID: sd.kind + stage, Stage: stage, CreatedAt: runT0, UpdatedAt: runT0,
				Dispatch: state.Dispatch{Kind: sd.dispatch, Mode: "endpoint", At: &at},
			})
		}
	}

	// A revision also ends failed or revised.
	for _, stage := range []string{state.StageRevisionFailed, state.StageRevised} {
		insertRow(t, st, state.Heal{
			Kind: state.HealKindRevision, Project: "p", RunID: "revision-" + stage, Stage: stage,
			CreatedAt: runT0, UpdatedAt: runT0, Dispatch: state.Dispatch{Kind: protocol.RunKindGuidancePR, Mode: "endpoint", At: &at},
		})
	}

	oneCard := func(read func(card string) ([]state.Heal, error)) func() ([]state.Heal, error) {
		return func() ([]state.Heal, error) {
			var all []state.Heal

			for _, card := range cards {
				rows, err := read(card)
				if err != nil {
					return nil, err
				}

				all = append(all, rows...)
			}

			return all, nil
		}
	}

	tests := []struct {
		reader string
		kinds  []string
		read   func() ([]state.Heal, error)
	}{
		{"OpenHeals", []string{state.HealKindHeal}, func() ([]state.Heal, error) { return st.OpenHeals(ctx) }},
		{"CardHeals", []string{state.HealKindHeal, state.HealKindForward}, oneCard(func(card string) ([]state.Heal, error) {
			return st.CardHeals(ctx, "p", card)
		})},
		{"LatestHeals", []string{state.HealKindHeal, state.HealKindForward}, func() ([]state.Heal, error) {
			latest, err := st.LatestHeals(ctx, "p")

			return slices.Collect(maps.Values(latest)), err
		}},
		{"RunHeal", []string{state.HealKindHeal}, oneCard(func(card string) ([]state.Heal, error) {
			h, err := st.RunHeal(ctx, "p", card, runT0)
			if h == nil {
				return nil, err
			}

			return []state.Heal{*h}, err
		})},
		{
			"InFlightRows",
			[]string{state.HealKindHeal, state.HealKindForward, state.HealKindAnalysis, state.HealKindRevision},
			func() ([]state.Heal, error) { return st.InFlightRows(ctx) },
		},
		{"CardAnalyses", []string{state.HealKindAnalysis}, oneCard(func(card string) ([]state.Heal, error) {
			return st.CardAnalyses(ctx, "p", card)
		})},
		{"OpenAnalyses", []string{state.HealKindAnalysis}, func() ([]state.Heal, error) { return st.OpenAnalyses(ctx) }},
		{"OpenRevisions", []string{state.HealKindRevision}, func() ([]state.Heal, error) { return st.OpenRevisions(ctx) }},
		{"LastRefusedRevision", []string{state.HealKindRevision}, func() ([]state.Heal, error) {
			h, err := st.LastRefusedRevision(ctx, "p", time.Time{})
			if h == nil {
				return nil, err
			}

			return []state.Heal{*h}, err
		}},
		{"LastFailedRevision", []string{state.HealKindRevision}, func() ([]state.Heal, error) {
			h, err := st.LastFailedRevision(ctx, "p")
			if h == nil {
				return nil, err
			}

			return []state.Heal{*h}, err
		}},
		{"LastRevisedRevision", []string{state.HealKindRevision}, func() ([]state.Heal, error) {
			h, err := st.LastRevisedRevision(ctx, "p")
			if h == nil {
				return nil, err
			}

			return []state.Heal{*h}, err
		}},
		{"LastRevision", []string{state.HealKindRevision}, func() ([]state.Heal, error) {
			h, err := st.LastRevision(ctx, "p")
			if h == nil {
				return nil, err
			}

			return []state.Heal{*h}, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.reader, func(t *testing.T) {
			rows, err := tt.read()
			require.NoError(t, err)

			seen := map[string]bool{}

			for _, h := range rows {
				assert.Contains(t, tt.kinds, h.Kind)

				seen[h.Kind] = true
			}

			for _, kind := range tt.kinds {
				assert.True(t, seen[kind], "no %s row", kind)
			}
		})
	}

	n, err := st.HealedSince(ctx, runT0)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "HealedSince counts the heal only")
}

func TestSentinelHealsGrowAnalysisIndexWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("DROP INDEX idx_sentinel_analysis_run")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	var n, v int

	require.NoError(t, st.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sentinel_analysis_run'`).Scan(&n))
	assert.Equal(t, 1, n)

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
}

func TestFindingRouting(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	healID, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0})
	require.NoError(t, err)

	at := runT0.Add(time.Hour)
	fs := []protocol.FrictionFinding{
		{Kind: protocol.FindingCode, Title: "a", Text: "a", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "b", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "c", Evidence: "e"},
		{Kind: protocol.FindingHardCard, Text: "d", Evidence: "e"},
	}
	require.NoError(t, st.AddFindings(ctx, healID, "p", "P-001", fs, at))
	require.NoError(t, st.AddFindings(ctx, healID, "q", "Q-001", fs[:1], at))

	queued, err := st.QueuedFindings(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 4)
	assert.Equal(t, "a", queued[0].Title, "oldest first")

	require.NoError(t, st.SetFindingOutcome(ctx, queued[0].ID, state.FindingCarded, "", "P-009", 0, at))
	require.NoError(t, st.SetFindingOutcome(ctx, queued[1].ID, state.FindingPending, "", "", 0, at))
	require.NoError(t, st.SetFindingOutcome(ctx, queued[2].ID, state.FindingDuplicate, "like finding 2", "", 0, at))
	require.NoError(t, st.SetFindingOutcome(ctx, queued[3].ID, state.FindingCarded, "", "Q-009", 0, at.Add(-48*time.Hour)))

	left, err := st.QueuedFindings(ctx)
	require.NoError(t, err)
	assert.Empty(t, left)

	guidance, err := st.ProjectGuidance(ctx, "p")
	require.NoError(t, err)
	require.Len(t, guidance, 1, "a duplicate guidance finding is not listed")
	assert.Equal(t, "b", guidance[0].Text)

	n, err := st.CardedSince(ctx, "p", at.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = st.CardedSince(ctx, "q", at.Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n, "an older card does not count today")

	carded, err := st.CardedFindings(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"P-009": true}, carded)

	got, err := st.CardFindings(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.Equal(t, "like finding 2", got[2].Reason)
}

func TestOpenRevisionIsOnePerProject(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	first, err := st.OpenRevision(ctx, "p", runT0)
	require.NoError(t, err)
	assert.Equal(t, state.HealKindRevision, first.Kind)
	assert.Equal(t, state.StageRevise, first.Stage)
	assert.Empty(t, first.CardID)
	assert.True(t, first.CreatedAt.Equal(runT0))

	again, err := st.OpenRevision(ctx, "p", runT0.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "the open revision is reused")

	other, err := st.OpenRevision(ctx, "q", runT0)
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, other.ID)

	open, err := st.OpenRevisions(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{first.ID, other.ID}, ids(open))

	first.Stage = state.StageRevised
	require.NoError(t, st.SaveHeal(ctx, &first))

	next, err := st.OpenRevision(ctx, "p", runT0.Add(time.Hour))
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, next.ID, "an ended revision frees the project")

	open, err = st.OpenRevisions(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{other.ID, next.ID}, ids(open))
}

func TestLastRefusedRevisionIsTheNewestSince(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	for i, r := range []struct{ project, stage string }{
		{"p", state.StageRevisionRefused},
		{"p", state.StageRevisionRefused},
		{"p", state.StageRevised},
		{"q", state.StageRevisionRefused},
	} {
		at := runT0.Add(time.Duration(i) * time.Hour)
		insertRow(t, st, state.Heal{
			Kind: state.HealKindRevision, Project: r.project, Stage: r.stage, Allowed: []string{fmt.Sprint(i)},
			CreatedAt: at, UpdatedAt: at,
		})
	}

	got, err := st.LastRefusedRevision(ctx, "p", runT0)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"1"}, got.Allowed, "the newest refused row created after since")

	got, err = st.LastRefusedRevision(ctx, "p", runT0.Add(time.Hour))
	require.NoError(t, err)
	assert.Nil(t, got, "a row created at since is not after it")

	got, err = st.LastRefusedRevision(ctx, "r", time.Time{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLastFailedAndLastRevisedRevision(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	for i, r := range []struct{ project, stage string }{
		{"p", state.StageRevisionFailed},
		{"p", state.StageRevisionStale},
		{"p", state.StageRevisionRefused},
		{"p", state.StageRevised},
		{"q", state.StageRevisionFailed},
	} {
		at := runT0.Add(time.Duration(i) * time.Hour)
		insertRow(t, st, state.Heal{
			Kind: state.HealKindRevision, Project: r.project, Stage: r.stage, CreatedAt: at, UpdatedAt: at,
		})
	}

	got, err := st.LastFailedRevision(ctx, "p")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, state.StageRevisionStale, got.Stage, "refused and revised rows do not count")
	assert.True(t, got.UpdatedAt.Equal(runT0.Add(time.Hour)))

	got, err = st.LastFailedRevision(ctx, "r")
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = st.LastRevisedRevision(ctx, "p")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, state.StageRevised, got.Stage)

	got, err = st.LastRevisedRevision(ctx, "q")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLastRevisionIsTheNewestWhateverItsStage(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	got, err := st.LastRevision(ctx, "p")
	require.NoError(t, err)
	assert.Nil(t, got)

	for i, r := range []struct{ project, stage string }{
		{"p", state.StageRevisionFailed},
		{"p", state.StageRevise},
		{"q", state.StageRevised},
	} {
		at := runT0.Add(time.Duration(i) * time.Hour)
		insertRow(t, st, state.Heal{
			Kind: state.HealKindRevision, Project: r.project, Stage: r.stage, CreatedAt: at, UpdatedAt: at,
		})
	}

	got, err = st.LastRevision(ctx, "p")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, state.StageRevise, got.Stage)
	assert.True(t, got.UpdatedAt.Equal(runT0.Add(time.Hour)))
}

func TestFindingsReadsByIDWhateverTheState(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	healID, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0})
	require.NoError(t, err)

	fs := []protocol.FrictionFinding{
		{Kind: protocol.FindingGuidance, Text: "a", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "b", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "c", Evidence: "e"},
	}
	require.NoError(t, st.AddFindings(ctx, healID, "p", "P-001", fs, runT0))

	all, err := st.QueuedFindings(ctx)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.NoError(t, st.SetFindingOutcome(ctx, all[0].ID, state.FindingDropped, "introduces x", "", 5, runT0))

	got, err := st.Findings(ctx, []int64{all[2].ID, all[0].ID})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Text, "in ID order")
	assert.Equal(t, state.FindingDropped, got[0].State)
	assert.Equal(t, "c", got[1].Text)

	got, err = st.Findings(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPendingFindingsAreTheProjectsPendingGuidanceOldestFirst(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	healID, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0})
	require.NoError(t, err)

	fs := []protocol.FrictionFinding{
		{Kind: protocol.FindingGuidance, Text: "a", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "b", Evidence: "e"},
		{Kind: protocol.FindingGuidance, Text: "c", Evidence: "e"},
		{Kind: protocol.FindingCode, Text: "d", Evidence: "e"},
	}
	require.NoError(t, st.AddFindings(ctx, healID, "p", "P-001", fs, runT0))
	require.NoError(t, st.AddFindings(ctx, healID, "q", "Q-001", fs[:1], runT0))

	all, err := st.QueuedFindings(ctx)
	require.NoError(t, err)

	for _, f := range all {
		next := state.FindingPending
		if f.Text == "b" {
			next = state.FindingProposed
		}

		require.NoError(t, st.SetFindingOutcome(ctx, f.ID, next, "", "", 0, runT0))
	}

	got, err := st.PendingFindings(ctx, "p")
	require.NoError(t, err)

	texts := make([]string, 0, len(got))
	for _, f := range got {
		texts = append(texts, f.Text)
	}

	assert.Equal(t, []string{"a", "c"}, texts)
}

// The revision columns and index need no schema bump: a database without them
// grows them and keeps its version, and a row an older image wrote reads back
// with nothing sent or allowed.
func TestSentinelHealsGrowRevisionColumnsWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)

	for _, stmt := range []string{
		"DROP INDEX idx_sentinel_revision_open",
		"ALTER TABLE sentinel_heals DROP COLUMN sent_findings",
		"ALTER TABLE sentinel_heals DROP COLUMN allowed",
	} {
		_, err = db.Exec(stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	assert.True(t, columnExists(t, st.db, "sentinel_heals", "sent_findings"))
	assert.True(t, columnExists(t, st.db, "sentinel_heals", "allowed"))

	var n, v int

	require.NoError(t, st.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sentinel_revision_open'`).Scan(&n))
	assert.Equal(t, 1, n)

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	// OpenHeal's statement as the older image runs it, verbatim.
	const olderColumns = `run_id, cause, response, stage, reason,
	dispatch_kind, dispatch_mode, dispatch_ceiling_usd, dispatch_at, dispatch_answered_run_id, dispatch_failure,
	investigation, check_result, sent_subtasks, sent_siblings, change, dropped,
	relaunched_at, cost_base_usd, source_card, updated_at`

	_, err = st.db.Exec(
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+olderColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		state.HealKindHeal, "p", "P-001", runT0.UnixNano(), runT0.UnixNano(),
		"r1", "verify_parked", "investigate_fix", state.StageParked, "", "", "", 0.0, nil, "", "",
		"null", "null", "null", "null", "null", "null", nil, 0.0, "", runT0.UnixNano())
	require.NoError(t, err)

	h := onlyHeal(t, st, "P-001")
	assert.Nil(t, h.SentFindings)
	assert.Nil(t, h.Allowed)
}

func ids(heals []state.Heal) []int64 {
	out := make([]int64, 0, len(heals))
	for _, h := range heals {
		out = append(out, h.ID)
	}

	return out
}

func TestRerouteFindingMovesKindStateAndReasonTogether(t *testing.T) {
	ctx := context.Background()
	st := openLadderStore(t)

	healID, err := st.OpenHeal(ctx, state.Heal{Kind: state.HealKindHeal, Project: "p", CardID: "P-001", RunStarted: runT0})
	require.NoError(t, err)

	at := runT0.Add(time.Hour)
	require.NoError(t, st.AddFindings(ctx, healID, "p", "P-001",
		[]protocol.FrictionFinding{{Kind: protocol.FindingGuidance, Title: "t", Text: "the worker has no docker", Evidence: "e"}}, runT0))

	queued, err := st.QueuedFindings(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)

	require.NoError(t, st.RerouteFinding(ctx, queued[0].ID, protocol.FindingProjectVerify, state.FindingNote, "re-routed: mentions CM", at))

	got, err := st.CardFindings(ctx, "p", "P-001")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.FindingProjectVerify, got[0].Kind)
	assert.Equal(t, state.FindingNote, got[0].State)
	assert.Equal(t, "re-routed: mentions CM", got[0].Reason)
	assert.True(t, got[0].UpdatedAt.Equal(at))
	assert.Equal(t, "the worker has no docker", got[0].Text)

	left, err := st.QueuedFindings(ctx)
	require.NoError(t, err)
	assert.Empty(t, left)

	guidance, err := st.ProjectGuidance(ctx, "p")
	require.NoError(t, err)
	assert.Empty(t, guidance, "a rerouted finding is no guidance")

	pending, err := st.PendingFindings(ctx, "p")
	require.NoError(t, err)
	assert.Empty(t, pending)
}
