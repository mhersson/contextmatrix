package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

func TestStandingPRIsOneRowPerProject(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	none, err := st.StandingPR(ctx, "p")
	require.NoError(t, err)
	assert.Nil(t, none)

	pushed := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	first := state.StandingPR{Project: "p", URL: "https://github.com/a/b/pull/1", HeadSHA: "h1", File: "AGENTS.md", PushedAt: pushed}
	require.NoError(t, st.PutStandingPR(ctx, first))
	require.NoError(t, st.PutStandingPR(ctx, state.StandingPR{Project: "q", URL: "u", HeadSHA: "x", File: "f", PushedAt: pushed}))

	read := pushed.Add(time.Hour)
	first.State, first.ReadAt = state.PROpen, read
	require.NoError(t, st.PutStandingPR(ctx, first))

	got, err := st.StandingPR(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, &first, got)

	// A later revision replaces the record.
	second := state.StandingPR{Project: "p", URL: "https://github.com/a/b/pull/2", HeadSHA: "h2", File: "AGENTS.md", PushedAt: read}
	require.NoError(t, st.PutStandingPR(ctx, second))

	all, err := st.StandingPRs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []state.StandingPR{second, {Project: "q", URL: "u", HeadSHA: "x", File: "f", PushedAt: pushed}}, all)
}

func TestProposedFindingsAreTheProjectsGuidanceInStateProposed(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

	id := openDispatchedHeal(t, st, "P-001", state.StageInvestigate, "endpoint")
	require.NoError(t, st.AddFindings(ctx, id, "p", "P-001", []protocol.FrictionFinding{
		{Kind: protocol.FindingGuidance, Text: "one"},
		{Kind: protocol.FindingGuidance, Text: "two"},
		{Kind: protocol.FindingCode, Text: "three"},
	}, at))

	all, err := st.CardFindings(ctx, "p", "P-001")
	require.NoError(t, err)
	require.Len(t, all, 3)

	require.NoError(t, st.SetFindingOutcome(ctx, all[0].ID, state.FindingProposed, "", "url", 7, at))
	require.NoError(t, st.SetFindingOutcome(ctx, all[2].ID, state.FindingProposed, "", "", 0, at))

	got, err := st.ProposedFindings(ctx, "p")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "one", got[0].Text)

	other, err := st.ProposedFindings(ctx, "q")
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestSentinelStandingPRsTableWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE sentinel_standing_prs")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	require.NoError(t, st.PutStandingPR(context.Background(), state.StandingPR{Project: "p", URL: "u", HeadSHA: "h", File: "f"}))

	got, err := st.StandingPR(context.Background(), "p")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "h", got.HeadSHA)
}
