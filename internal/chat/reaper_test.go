package chat_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/chat/sqlite"
	"github.com/mhersson/contextmatrix/internal/clock"
)

func TestIdleReaper_EndsWarmIdlePastTTL(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fakeClock := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   fakeClock,
		IdleTTL: 30 * time.Minute,
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)
	// Set to warm-idle with last_active far in the past
	sess.Status = chat.StatusWarmIdle
	sess.LastActive = fakeClock.Now().Add(-2 * time.Hour)
	require.NoError(t, store.UpdateSession(ctx, sess))

	reaper := chat.NewIdleReaper(mgr, 1*time.Millisecond)
	go reaper.Run(ctx)

	t.Cleanup(reaper.Stop)

	require.Eventually(t, func() bool {
		got, err := store.GetSession(ctx, sess.ID)
		if err != nil {
			return false
		}

		return got.Status == chat.StatusCold
	}, 2*time.Second, 5*time.Millisecond, "reaper did not transition session to cold")

	assert.Equal(t, int64(1), runner.endCalls.Load())
}

// TestIdleReaper_Stop_DoubleCallSafe verifies that calling Stop twice does
// not panic. The reaper is plumbed through main.go's lifecycle and shutdown
// hooks can fire it more than once during graceful shutdown / signal-driven
// teardown.
func TestIdleReaper_Stop_DoubleCallSafe(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  &stubRunner{},
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	reaper := chat.NewIdleReaper(mgr, time.Hour)

	// First Stop closes the channel; second Stop must be a no-op, not a panic.
	assert.NotPanics(t, reaper.Stop)
	assert.NotPanics(t, reaper.Stop)
	assert.NotPanics(t, reaper.Stop)
}

func TestIdleReaper_SweepStaleRehydration_FlipsTimeoutSessions(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fakeClock := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mgr := chat.NewManager(chat.Config{
		Store:              store,
		Runner:             &stubRunner{},
		Clock:              fakeClock,
		IdleTTL:            1 * time.Hour,
		RehydrationTimeout: 10 * time.Minute,
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Activate rehydration and push last_active 15 min into the past.
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess.ID, true))
	sess.LastActive = fakeClock.Now().Add(-15 * time.Minute)
	require.NoError(t, store.UpdateSession(ctx, sess))

	reaper := chat.NewIdleReaper(mgr, 1*time.Millisecond)
	reaper.SweepStaleRehydrationForTest(ctx)

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, got.RehydrationActive, "stale rehydration flag should be flipped off")
}

func TestIdleReaper_SweepStaleRehydration_LeavesRecentAlone(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fakeClock := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mgr := chat.NewManager(chat.Config{
		Store:              store,
		Runner:             &stubRunner{},
		Clock:              fakeClock,
		IdleTTL:            1 * time.Hour,
		RehydrationTimeout: 10 * time.Minute,
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Activate rehydration and push last_active 2 min into the past (within timeout).
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess.ID, true))
	sess.LastActive = fakeClock.Now().Add(-2 * time.Minute)
	require.NoError(t, store.UpdateSession(ctx, sess))

	reaper := chat.NewIdleReaper(mgr, 1*time.Millisecond)
	reaper.SweepStaleRehydrationForTest(ctx)

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, got.RehydrationActive, "recent rehydration must survive the sweep")
}

func TestIdleReaper_SweepStaleRehydration_SkipsIfDisabled(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fakeClock := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	// RehydrationTimeout: 0 disables the sweep.
	mgr := chat.NewManager(chat.Config{
		Store:              store,
		Runner:             &stubRunner{},
		Clock:              fakeClock,
		IdleTTL:            1 * time.Hour,
		RehydrationTimeout: 0,
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Activate rehydration and push far into the past.
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess.ID, true))
	sess.LastActive = fakeClock.Now().Add(-1 * time.Hour)
	require.NoError(t, store.UpdateSession(ctx, sess))

	reaper := chat.NewIdleReaper(mgr, 1*time.Millisecond)
	reaper.SweepStaleRehydrationForTest(ctx)

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, got.RehydrationActive, "sweep must be skipped when RehydrationTimeout is 0")
}

func TestIdleReaper_SweepStaleRehydration_MultipleStale(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fakeClock := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mgr := chat.NewManager(chat.Config{
		Store:              store,
		Runner:             &stubRunner{},
		Clock:              fakeClock,
		IdleTTL:            1 * time.Hour,
		RehydrationTimeout: 5 * time.Minute,
	})

	ctx := context.Background()

	// Create three sessions: two stale, one fresh.
	sess1, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess1.ID, true))
	sess1.LastActive = fakeClock.Now().Add(-10 * time.Minute)
	require.NoError(t, store.UpdateSession(ctx, sess1))

	sess2, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess2.ID, true))
	sess2.LastActive = fakeClock.Now().Add(-8 * time.Minute)
	require.NoError(t, store.UpdateSession(ctx, sess2))

	sess3, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess3.ID, true))
	sess3.LastActive = fakeClock.Now().Add(-1 * time.Minute)
	require.NoError(t, store.UpdateSession(ctx, sess3))

	reaper := chat.NewIdleReaper(mgr, 1*time.Millisecond)
	reaper.SweepStaleRehydrationForTest(ctx)

	// Check results.
	got1, err := mgr.GetSession(ctx, sess1.ID)
	require.NoError(t, err)
	assert.False(t, got1.RehydrationActive, "sess1 (-10m) should have rehydration flipped off")

	got2, err := mgr.GetSession(ctx, sess2.ID)
	require.NoError(t, err)
	assert.False(t, got2.RehydrationActive, "sess2 (-8m) should have rehydration flipped off")

	got3, err := mgr.GetSession(ctx, sess3.ID)
	require.NoError(t, err)
	assert.True(t, got3.RehydrationActive, "sess3 (-1m) should survive")
}
