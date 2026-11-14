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

func TestLLMChannel_RoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	mode, at, by, err := st.LLMChannel(ctx)
	require.NoError(t, err)
	assert.Empty(t, mode)
	assert.True(t, at.IsZero())
	assert.Empty(t, by)

	require.NoError(t, st.PutLLMChannel(ctx, "claude_code", "human:alice"))
	require.NoError(t, st.PutLLMChannel(ctx, "endpoint", "human:bob"))

	mode, at, by, err = st.LLMChannel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "endpoint", mode)
	assert.False(t, at.IsZero())
	assert.Equal(t, "human:bob", by)
}

func TestSchema_NewTablesKeepVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)

	var v int

	require.NoError(t, s.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
	assert.True(t, tableExists(t, s.db, "llm_channel"))
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE llm_channel")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s2, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	assert.True(t, tableExists(t, s2.db, "llm_channel"))
	require.NoError(t, s2.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
}

func TestCardRun_OpenCloseRoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 0).UTC()

	runs, err := st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, runs)

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "claude_code", t0, state.LaunchFacts{}))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", t0, state.LaunchFacts{}))

	runs, err = st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []CardRun{
		{Project: "p", CardID: "P-001", Mode: "claude_code", StartedAt: t0},
		{Project: "p", CardID: "P-002", Mode: "endpoint", StartedAt: t0},
	}, runs)

	require.NoError(t, st.CloseCardRun(ctx, "p", "P-001", t0, t0.Add(time.Minute)))
	// Closing a closed or unknown run is a no-op.
	require.NoError(t, st.CloseCardRun(ctx, "p", "P-001", t0, t0.Add(2*time.Minute)))
	require.NoError(t, st.CloseCardRun(ctx, "p", "P-404", t0, t0))

	runs, err = st.OpenCardRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "P-002", runs[0].CardID)
}

func TestCardRun_RerunReplacesRow(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 0).UTC()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", t0, state.LaunchFacts{}))
	require.NoError(t, st.CloseCardRun(ctx, "p", "P-001", t0, t0.Add(time.Minute)))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "claude_code", t0.Add(time.Hour), state.LaunchFacts{}))

	runs, err := st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []CardRun{{Project: "p", CardID: "P-001", Mode: "claude_code", StartedAt: t0.Add(time.Hour)}}, runs)
}

func TestSubscriptionUsage_UpsertPerWindow(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	rows, err := st.SubscriptionUsage(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows)

	reset1 := time.Unix(2_000_000_000, 0).UTC()
	reset2 := time.Unix(2_000_100_000, 0).UTC()
	seen1 := time.Unix(1_900_000_000, 0).UTC()
	seen2 := time.Unix(1_900_000_500, 0).UTC()

	require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{
		{Window: "seven_day", Utilization: 0.4, Status: "allowed", ResetsAt: reset1},
		{Window: "five_hour", Utilization: 0.1, ResetsAt: reset1},
	}, seen1))
	require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{
		{Window: "seven_day", Utilization: 0.64, Status: "allowed_warning", ResetsAt: reset2},
	}, seen2))

	rows, err = st.SubscriptionUsage(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byWindow := map[string]SubscriptionUsageRow{}
	for _, r := range rows {
		byWindow[r.Window] = r
	}

	sd := byWindow["seven_day"]
	assert.InDelta(t, 0.64, sd.Utilization, 1e-9)
	assert.Equal(t, "allowed_warning", sd.Status)
	assert.True(t, reset2.Equal(sd.ResetsAt))
	assert.True(t, seen2.Equal(sd.SeenAt))

	fh := byWindow["five_hour"]
	assert.InDelta(t, 0.1, fh.Utilization, 1e-9)
	assert.Empty(t, fh.Status)
	assert.True(t, seen1.Equal(fh.SeenAt))
}

// A reading replaces the stored one only when it is not older: a later reset
// always wins, within one period utilization only rises, and at equal
// utilization a stored rejected status yields only to another rejected one.
func TestSubscriptionUsage_KeepsNewerReading(t *testing.T) {
	reset := time.Unix(2_000_000_000, 0).UTC()
	seen1 := time.Unix(1_900_000_000, 0).UTC()
	seen2 := time.Unix(1_900_000_500, 0).UTC()

	reading := func(util float64, status string, resets time.Time) protocol.SubscriptionUsage {
		return protocol.SubscriptionUsage{Window: "five_hour", Utilization: util, Status: status, ResetsAt: resets}
	}

	tests := []struct {
		name             string
		stored, incoming protocol.SubscriptionUsage
		replaces         bool
	}{
		{"older period", reading(0.5, "rejected", reset), reading(0.9, "allowed", reset.Add(-5*time.Hour)), false},
		{"older period rejected", reading(0.5, "allowed", reset), reading(0.9, "rejected", reset.Add(-5*time.Hour)), false},
		{"same period lower", reading(0.5, "rejected", reset), reading(0.4, "allowed", reset), false},
		{"same period equal keeps rejected", reading(0.5, "rejected", reset), reading(0.5, "allowed", reset), false},
		{"same period equal rejected over rejected", reading(0.5, "rejected", reset), reading(0.5, "rejected", reset), true},
		{"same period equal over allowed_warning", reading(0.5, "allowed_warning", reset), reading(0.5, "allowed", reset), true},
		{"same period higher", reading(0.5, "rejected", reset), reading(0.7, "allowed_warning", reset), true},
		{"later period", reading(0.5, "rejected", reset), reading(0.1, "allowed", reset.Add(5*time.Hour)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{tt.stored}, seen1))
			require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{tt.incoming}, seen2))

			rows, err := st.SubscriptionUsage(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1)

			want, wantSeen := tt.stored, seen1
			if tt.replaces {
				want, wantSeen = tt.incoming, seen2
			}

			got := rows[0]
			assert.InDelta(t, want.Utilization, got.Utilization, 1e-9)
			assert.Equal(t, want.Status, got.Status)
			assert.True(t, want.ResetsAt.Equal(got.ResetsAt), "resets_at %v, want %v", got.ResetsAt, want.ResetsAt)
			assert.True(t, wantSeen.Equal(got.SeenAt), "seen_at %v, want %v", got.SeenAt, wantSeen)
		})
	}
}

func TestCardRun_CloseIsBoundedByStart(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 123_000_000).UTC()
	rerun := t0.Add(300 * time.Millisecond) // same second as t0

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", t0, state.LaunchFacts{}))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "claude_code", rerun, state.LaunchFacts{}))

	runs, err := st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []CardRun{{Project: "p", CardID: "P-001", Mode: "claude_code", StartedAt: rerun}}, runs,
		"started_at round-trips exactly")

	// A close aimed at the earlier run leaves the rerun open.
	require.NoError(t, st.CloseCardRun(ctx, "p", "P-001", t0, t0.Add(time.Second)))

	runs, err = st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.Len(t, runs, 1)

	// A close that covers the rerun's start ends it.
	require.NoError(t, st.CloseCardRun(ctx, "p", "P-001", rerun, rerun.Add(time.Second)))

	runs, err = st.OpenCardRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestSubscriptionUsage_SkipsZeroResetsAt(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	reset := time.Unix(2_000_000_000, 0).UTC()

	require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{
		{Window: "seven_day", Utilization: 0.5, ResetsAt: reset},
	}, time.Unix(1_900_000_000, 0)))
	require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{
		{Window: "seven_day", Utilization: 0.9},
		{Window: "five_hour", Utilization: 0.3},
	}, time.Unix(1_900_000_100, 0)))

	rows, err := st.SubscriptionUsage(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.InDelta(t, 0.5, rows[0].Utilization, 1e-9)
}

// Within one period a rejected reading always wins its status, and the stored
// utilization only rises, so a rejected reading of unknown utilization never
// lowers it.
func TestSubscriptionUsage_RejectedWinsStatusKeepsMaxUtilization(t *testing.T) {
	reset := time.Unix(2_000_000_000, 0).UTC()
	seen1 := time.Unix(1_900_000_000, 0).UTC()
	seen2 := time.Unix(1_900_000_500, 0).UTC()

	reading := func(util float64, status string, resets time.Time) protocol.SubscriptionUsage {
		return protocol.SubscriptionUsage{Window: "five_hour", Utilization: util, Status: status, ResetsAt: resets}
	}

	tests := []struct {
		name             string
		stored, incoming protocol.SubscriptionUsage
		wantUtil         float64
		wantStatus       string
		wantSeen         time.Time
	}{
		{"rejected lower utilization", reading(0.8, "allowed_warning", reset), reading(0.3, "rejected", reset), 0.8, "rejected", seen2},
		{"rejected unknown utilization", reading(0.8, "allowed", reset), reading(0, "rejected", reset), 0.8, "rejected", seen2},
		{"rejected higher utilization", reading(0.5, "allowed", reset), reading(1, "rejected", reset), 1, "rejected", seen2},
		{"allowed equal keeps rejected", reading(1, "rejected", reset), reading(1, "allowed", reset), 1, "rejected", seen1},
		{"allowed lower keeps rejected", reading(1, "rejected", reset), reading(0.2, "allowed", reset), 1, "rejected", seen1},
		{"later period replaces rejected", reading(1, "rejected", reset), reading(0.1, "allowed", reset.Add(5*time.Hour)), 0.1, "allowed", seen2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{tt.stored}, seen1))
			require.NoError(t, st.RecordSubscriptionUsage(ctx, []protocol.SubscriptionUsage{tt.incoming}, seen2))

			rows, err := st.SubscriptionUsage(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1)

			assert.InDelta(t, tt.wantUtil, rows[0].Utilization, 1e-9)
			assert.Equal(t, tt.wantStatus, rows[0].Status)
			assert.True(t, tt.wantSeen.Equal(rows[0].SeenAt), "seen_at %v, want %v", rows[0].SeenAt, tt.wantSeen)
		})
	}
}
