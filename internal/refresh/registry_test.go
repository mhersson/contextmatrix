package refresh

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
)

func TestState_IsTerminal(t *testing.T) {
	assert.False(t, StatePlanning.IsTerminal())
	assert.False(t, StateRunning.IsTerminal())
	assert.True(t, StateSucceeded.IsTerminal())
	assert.True(t, StateFailed.IsTerminal())
}

func TestJob_ZeroValueState(t *testing.T) {
	var j Job
	assert.Equal(t, StateIdle, j.State, "zero value must be StateIdle")
}

func TestJob_StartedAt(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	j := Job{StartedAt: now}
	assert.Equal(t, now, j.StartedAt)
}

func TestRegistry_Acquire_FirstTimeSucceeds(t *testing.T) {
	r := NewRegistry()
	job, err := r.Acquire("contextmatrix", "contextmatrix", "human:web-aaa")
	require.NoError(t, err)
	assert.Equal(t, StatePlanning, job.State)
	assert.Equal(t, "human:web-aaa", job.AgentID)
}

func TestRegistry_Acquire_DuplicateReturnsErrInFlight(t *testing.T) {
	r := NewRegistry()
	_, err := r.Acquire("p", "r", "human:a")
	require.NoError(t, err)
	_, err = r.Acquire("p", "r", "human:b")
	assert.ErrorIs(t, err, ErrJobInFlight,
		"expected ErrJobInFlight, got %v", err)
}

func TestRegistry_Acquire_DifferentReposIndependent(t *testing.T) {
	r := NewRegistry()
	_, err := r.Acquire("p", "r1", "human:a")
	require.NoError(t, err)
	_, err = r.Acquire("p", "r2", "human:b")
	assert.NoError(t, err, "different (p, repo) pairs must not collide")
}

func TestRegistry_MarkRunning(t *testing.T) {
	r := NewRegistry()
	_, err := r.Acquire("p", "r", "human:a")
	require.NoError(t, err)

	require.NoError(t, r.MarkRunning("p", "r", 4))

	r.mu.Lock()
	job := *r.jobs["p/r"]
	r.mu.Unlock()

	assert.Equal(t, StateRunning, job.State)
	assert.Equal(t, 4, job.DocsTotal)
}

func TestRegistry_UpdateProgress_TrackedJob(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	require.NoError(t, r.MarkRunning("p", "r", 4))

	tracked, err := r.UpdateProgress("p", "r", 4, 2, "code-structure.md")
	require.NoError(t, err)
	assert.True(t, tracked)

	r.mu.Lock()
	job := *r.jobs["p/r"]
	r.mu.Unlock()

	assert.Equal(t, 2, job.DocsDone)
	assert.Equal(t, "code-structure.md", job.CurrentDoc)
}

func TestRegistry_UpdateProgress_NoSuchJobReturnsTrackedFalse(t *testing.T) {
	r := NewRegistry()
	tracked, err := r.UpdateProgress("p", "r", 0, 1, "x.md")
	require.NoError(t, err)
	assert.False(t, tracked, "missing job is not an error; just untracked")
}

func TestRegistry_UpdateProgress_TerminalJobReturnsTrackedFalse(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)
	r.mu.Lock()
	r.jobs["p/r"].State = StateSucceeded
	r.mu.Unlock()

	tracked, err := r.UpdateProgress("p", "r", 4, 5, "x.md")
	require.NoError(t, err)
	assert.False(t, tracked, "late progress on terminal job must not revive it")
}

func TestRegistry_MarkCommitted(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	require.NoError(t, r.MarkCommitted("p", "r", "abc1234"))

	r.mu.Lock()
	job := *r.jobs["p/r"]
	r.mu.Unlock()

	assert.True(t, job.Committed)
	assert.Equal(t, "abc1234", job.CommitSHA)
}

func TestRegistry_MarkCommitted_NoSuchJobIsNoOp(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.MarkCommitted("p", "r", "abc"),
		"side-effect on a missing job is not an error (local-mode commit case)")
}

func TestRegistry_MarkTerminal_Succeeded(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)
	_ = r.MarkCommitted("p", "r", "abc")

	require.NoError(t, r.MarkTerminal("p", "r", StateSucceeded, ""))

	snap := r.Snapshot("p")
	job := snap["r"]
	assert.Equal(t, StateSucceeded, job.State)
	require.NotNil(t, job.FinishedAt)
}

func TestRegistry_MarkTerminal_Failed(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")

	require.NoError(t, r.MarkTerminal("p", "r", StateFailed, "boom"))

	snap := r.Snapshot("p")
	assert.Equal(t, StateFailed, snap["r"].State)
	assert.Equal(t, "boom", snap["r"].Error)
}

func TestRegistry_MarkTerminal_ReleasesLockForReacquire(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkTerminal("p", "r", StateSucceeded, "")

	_, err := r.Acquire("p", "r", "human:b")
	assert.NoError(t, err, "terminal jobs must not block re-acquire")
}

func TestRegistry_Snapshot_OnlyReturnsRequestedProject(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p1", "r1", "human:a")
	_, _ = r.Acquire("p2", "r1", "human:b")

	snap := r.Snapshot("p1")
	assert.Len(t, snap, 1)
	_, ok := snap["r1"]
	assert.True(t, ok)
}

func TestRegistry_Snapshot_ReturnsCopiesNotPointers(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")

	snap := r.Snapshot("p")
	mut := snap["r"]
	mut.DocsDone = 99 // mutate local copy
	snap["r"] = mut

	snap2 := r.Snapshot("p")
	assert.Equal(t, 0, snap2["r"].DocsDone, "snapshot mutation must not leak back")
}

func TestRegistry_GarbageCollectExpired_RemovesOldTerminal(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkTerminal("p", "r", StateSucceeded, "")

	clk.Advance(6 * time.Minute) // > 5 min keep window
	r.GarbageCollectExpired(5 * time.Minute)

	snap := r.Snapshot("p")
	assert.Empty(t, snap, "terminal jobs older than keep window must be GC'd")
}

func TestRegistry_GarbageCollectExpired_KeepsRecentTerminal(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkTerminal("p", "r", StateSucceeded, "")

	clk.Advance(2 * time.Minute) // < 5 min keep window
	r.GarbageCollectExpired(5 * time.Minute)

	snap := r.Snapshot("p")
	assert.Len(t, snap, 1, "recent terminal jobs must survive GC")
}

func TestRegistry_GarbageCollectExpired_LeavesNonTerminalAlone(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")

	clk.Advance(10 * time.Minute)
	r.GarbageCollectExpired(5 * time.Minute)

	snap := r.Snapshot("p")
	assert.Len(t, snap, 1, "GC must never reap non-terminal jobs")
}

func TestRegistry_PromoteStale_FlipsRunningToFailed(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	clk.Advance(31 * time.Minute) // > 30 min staleness threshold

	count := r.PromoteStale(30*time.Minute, "no progress callback")
	assert.Equal(t, 1, count)

	snap := r.Snapshot("p")
	assert.Equal(t, StateFailed, snap["r"].State)
	assert.Equal(t, "no progress callback", snap["r"].Error)
}

func TestRegistry_PromoteStale_LeavesFreshRunning(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	clk.Advance(5 * time.Minute) // < threshold

	count := r.PromoteStale(30*time.Minute, "x")
	assert.Equal(t, 0, count)

	snap := r.Snapshot("p")
	assert.Equal(t, StateRunning, snap["r"].State)
}

func TestRegistry_PromoteStale_LeavesPlanningAlone(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a") // stays in Planning

	clk.Advance(60 * time.Minute)

	count := r.PromoteStale(30*time.Minute, "x")
	assert.Equal(t, 0, count, "Planning is short-lived; only Running can stale")
}

func TestRegistry_UpdateProgress_OverridesDocsTotalWhenSmaller(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	// Skill reports the rebuild set is actually 3 (user un-approved 1 overwrite).
	tracked, err := r.UpdateProgress("p", "r", 3, 1, "x.md")
	require.NoError(t, err)
	assert.True(t, tracked)

	snap := r.Snapshot("p")
	assert.Equal(t, 3, snap["r"].DocsTotal)
}

func TestRegistry_UpdateProgress_ZeroDocsTotalDoesNotOverride(t *testing.T) {
	r := NewRegistry()
	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	// A zero docsTotal must not overwrite MarkRunning's value.
	tracked, err := r.UpdateProgress("p", "r", 0, 1, "x.md")
	require.NoError(t, err)
	assert.True(t, tracked)

	snap := r.Snapshot("p")
	assert.Equal(t, 4, snap["r"].DocsTotal, "zero docsTotal must leave the registry value unchanged")
}

func TestRegistry_MarkRunning_PreservesSkillReportedDocsTotal(t *testing.T) {
	r := NewRegistry()
	_, err := r.Acquire("alpha", "service-x", "human:test")
	require.NoError(t, err)

	// Skill reports first via UpdateProgress(docsTotal=2). The runner can
	// reach update_refresh_progress before the trigger handler reaches its
	// own MarkRunning(... len(plan.Items)) call.
	tracked, err := r.UpdateProgress("alpha", "service-x", 2, 0, "a.md")
	require.NoError(t, err)
	assert.True(t, tracked)

	// Trigger handler MarkRunning is reached LATER with a different total.
	require.NoError(t, r.MarkRunning("alpha", "service-x", 4))

	snap := r.Snapshot("alpha")
	job, ok := snap["service-x"]
	require.True(t, ok)
	assert.Equal(t, 2, job.DocsTotal,
		"MarkRunning must not clobber DocsTotal already set by UpdateProgress")
}
