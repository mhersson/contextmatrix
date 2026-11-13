package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel"
)

func TestSentinelSettingsDefaultsWithoutRow(t *testing.T) {
	st := openLadderStore(t)

	set, at, by, err := st.SentinelSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, sentinel.DefaultSettings(), set)
	assert.True(t, at.IsZero())
	assert.Empty(t, by)
}

func TestPutSentinelSettingsRoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	want := sentinel.Settings{Enabled: true, Healing: false, Friction: true, DailyCapUSD: 7.5, InvestigateCeilingUSD: 1.25, Headroom: 0.6}

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

func TestPutSentinelSettingsRejectsInvalid(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	bad := sentinel.DefaultSettings()
	bad.Headroom = 0

	require.ErrorIs(t, st.PutSentinelSettings(ctx, bad, "human:alice"), sentinel.ErrInvalidSettings)

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

	require.NoError(t, st.PutSentinelSettings(ctx, sentinel.DefaultSettings(), "human:alice"))

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
