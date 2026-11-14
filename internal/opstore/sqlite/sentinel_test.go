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

func TestSentinelSettingsDefaultsWithoutRow(t *testing.T) {
	st := openLadderStore(t)

	set, at, by, err := st.SentinelSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, state.DefaultSettings(), set)
	assert.True(t, at.IsZero())
	assert.Empty(t, by)
}

func TestPutSentinelSettingsRoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	want := state.Settings{Enabled: true, Healing: false, Friction: true, DailyCapUSD: 7.5, InvestigateCeilingUSD: 1.25, Headroom: 0.6, AnalyseCeilingUSD: 2}

	require.NoError(t, st.PutSentinelSettings(ctx, want, "human:alice"))

	set, at, by, err := st.SentinelSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, set)
	assert.Equal(t, "human:alice", by)
	assert.WithinDuration(t, time.Now(), at, 5*time.Second)

	want.Enabled = false
	require.NoError(t, st.PutSentinelSettings(ctx, want, "human:bob"))

	set, _, by, err = st.SentinelSettings(ctx)
	require.NoError(t, err)
	assert.False(t, set.Enabled)
	assert.Equal(t, "human:bob", by)
}

func TestSwitchingSentinelOnDecidesEveryWaitingRun(t *testing.T) {
	off := state.DefaultSettings()
	on := off
	on.Enabled = true
	healingOff := on
	healingOff.Healing = false
	offNoHealing := off
	offNoHealing.Healing = false
	recapped := on
	recapped.DailyCapUSD = 5

	tests := []struct {
		name    string
		before  *state.Settings // nil: no row stored yet
		after   state.Settings
		decides bool
	}{
		{"enabling with no row stored", nil, on, true},
		{"enabling", &off, on, true},
		{"turning healing on", &healingOff, on, true},
		{"turning healing on while off", &offNoHealing, off, true},
		{"enabling and turning healing on together", &offNoHealing, on, true},
		{"changing a cap", &on, recapped, false},
		{"turning healing off", &on, healingOff, false},
		{"disabling", &on, off, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			if tc.before != nil {
				require.NoError(t, st.PutSentinelSettings(ctx, *tc.before, "human:alice"))
			}

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{Autonomous: true}))

			require.NoError(t, st.PutSentinelSettings(ctx, tc.after, "human:alice"))

			undecided, err := st.UndecidedRuns(ctx)
			require.NoError(t, err)

			if !tc.decides {
				assert.Len(t, undecided, 1, "a run waiting before the save is still considered")

				return
			}

			assert.Empty(t, undecided, "switching on starts fresh")

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", runT0.Add(time.Hour), state.LaunchFacts{Autonomous: true}))

			undecided, err = st.UndecidedRuns(ctx)
			require.NoError(t, err)
			assert.Len(t, undecided, 1, "a run launched after the switch is considered")
		})
	}
}

func TestPutSentinelSettingsRejectsInvalid(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	bad := state.DefaultSettings()
	bad.Headroom = 0

	require.ErrorIs(t, st.PutSentinelSettings(ctx, bad, "human:alice"), state.ErrInvalidSettings)

	var n int

	require.NoError(t, st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sentinel_settings`).Scan(&n))
	assert.Zero(t, n)
}

func TestSentinelSettingsTableHasEcosystemColumn(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	var dflt string

	rows, err := st.db.QueryContext(ctx, `PRAGMA table_info(sentinel_settings)`)
	require.NoError(t, err)

	defer rows.Close()

	found := false

	for rows.Next() {
		var (
			cid, notnull, pk int
			name, typ        string
			d                *string
		)

		require.NoError(t, rows.Scan(&cid, &name, &typ, &notnull, &d, &pk))

		if name == "ecosystem" {
			found = true

			require.NotNil(t, d)

			dflt = *d
		}
	}

	require.NoError(t, rows.Err())
	require.True(t, found)
	assert.Equal(t, "0", dflt)

	require.NoError(t, st.PutSentinelSettings(ctx, state.DefaultSettings(), "human:alice"))

	var eco int

	require.NoError(t, st.db.QueryRowContext(ctx, `SELECT ecosystem FROM sentinel_settings WHERE id = 1`).Scan(&eco))
	assert.Zero(t, eco)
}

func TestSubscriptionWindowsDropsSeenAt(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, st.RecordSubscriptionUsage(ctx,
		[]protocol.SubscriptionUsage{{Window: "five_hour", Utilization: 0.4, Status: "allowed", ResetsAt: reset}}, time.Now()))

	got, err := st.SubscriptionWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: 0.4, Status: "allowed", ResetsAt: reset}}, got)
}

func TestSentinelSettingsTableKeepsSchemaVersion(t *testing.T) {
	st := openLadderStore(t)

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
	assert.True(t, tableExists(t, st.db, "sentinel_settings"))

	// An older binary's statements still run against the grown schema.
	_, err := st.db.Exec(`INSERT INTO llm_channel (id, mode, updated_at, updated_by) VALUES (1, 'endpoint', 1, 'x')`)
	require.NoError(t, err)
}

func TestPutSentinelSettingsRoundTripsTheAnalyseFields(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	want := state.DefaultSettings()
	want.AnalyseEveryRun, want.AnalyseCeilingUSD = true, 3.5

	require.NoError(t, st.PutSentinelSettings(ctx, want, "human:alice"))

	set, _, _, err := st.SentinelSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, set)
}

func TestSwitchingAnalysesOnQueuesEveryWaitingRun(t *testing.T) {
	ready := state.DefaultSettings()
	ready.Enabled, ready.AnalyseEveryRun = true, true
	frictionOff := ready
	frictionOff.Friction = false
	recapped := ready
	recapped.AnalyseCeilingUSD = 5
	analysesOff := ready
	analysesOff.AnalyseEveryRun = false

	tests := []struct {
		name   string
		before *state.Settings // nil: no row stored yet
		after  state.Settings
		queues bool
	}{
		{"friction turned on", &frictionOff, ready, true},
		{"analyses turned on", &analysesOff, ready, true},
		{"no row stored yet", nil, ready, true},
		{"analyses stay on", &ready, recapped, false},
		{"analyses turned off", &ready, analysesOff, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			if tc.before != nil {
				require.NoError(t, st.PutSentinelSettings(ctx, *tc.before, "human:alice"))
			}

			for _, id := range []string{"P-001", "P-002"} {
				require.NoError(t, st.OpenCardRun(ctx, "p", id, "endpoint", runT0, state.LaunchFacts{Autonomous: true}))
			}

			require.NoError(t, st.PutSentinelSettings(ctx, tc.after, "human:alice"))

			for _, id := range []string{"P-001", "P-002"} {
				r, err := st.CardRunRow(ctx, "p", id)
				require.NoError(t, err)
				assert.Equal(t, tc.queues, r.AnalysisQueued, id)
			}
		})
	}
}

func TestOpenCardRunResetsAnalysisQueued(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	on := state.DefaultSettings()
	on.Enabled, on.AnalyseEveryRun = true, true

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{}))
	require.NoError(t, st.PutSentinelSettings(ctx, on, "human:alice"))

	r, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	require.True(t, r.AnalysisQueued)

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour), state.LaunchFacts{}))

	r, err = st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.False(t, r.AnalysisQueued)
}

// The columns need no schema bump: a database without them grows them with
// their defaults and keeps its version.
func TestSentinelSettingsGrowAnalyseColumnsWithoutABump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)

	for _, stmt := range []string{
		"ALTER TABLE sentinel_settings DROP COLUMN analyse_every_run",
		"ALTER TABLE sentinel_settings DROP COLUMN analyse_ceiling_usd",
		"ALTER TABLE card_run_modes DROP COLUMN analysis_queued",
	} {
		_, err = db.Exec(stmt)
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())

	st, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	assert.True(t, columnExists(t, st.db, "sentinel_settings", "analyse_every_run"))
	assert.True(t, columnExists(t, st.db, "sentinel_settings", "analyse_ceiling_usd"))
	assert.True(t, columnExists(t, st.db, "card_run_modes", "analysis_queued"))

	var v int

	require.NoError(t, st.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)

	// An older image's settings row names neither column.
	_, err = st.db.Exec(`INSERT INTO sentinel_settings
		(id, enabled, healing, friction, daily_cap_usd, investigate_ceiling_usd, headroom, updated_at, updated_by)
		VALUES (1, 1, 1, 1, 20, 2, 0.85, 0, 'x')`)
	require.NoError(t, err)

	set, _, _, err := st.SentinelSettings(context.Background())
	require.NoError(t, err)
	assert.False(t, set.AnalyseEveryRun)
	assert.InDelta(t, 1, set.AnalyseCeilingUSD, 1e-9)
}
