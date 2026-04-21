package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// newAsyncTestService provisions a CardService with a wired commit queue
// plus an optional extra project for multi-project tests.
func newAsyncTestService(t *testing.T, extraProjects ...string) (*CardService, *gitops.CommitQueue, string) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Primary project.
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	for _, p := range extraProjects {
		pd := filepath.Join(boardsDir, p)
		require.NoError(t, os.MkdirAll(filepath.Join(pd, "tasks"), 0o755))

		cfg := testProject()
		cfg.Name = p
		cfg.Prefix = fmt.Sprintf("%s-", p)

		require.NoError(t, board.SaveProjectConfig(pd, cfg))
	}

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	q := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(q)

	t.Cleanup(func() {
		_ = q.Close(context.Background())
	})

	return svc, q, boardsDir
}

// waitForCommits polls the git log until at least wantCommits total commits
// are present or timeout elapses. Useful when assertions immediately follow
// a mutation that was routed through the async queue.
//
// Uses require.Eventually rather than an explicit time.Sleep loop; the commit
// queue's worker goroutine is scheduled by the Go runtime, not the clock
// abstraction, so a short real-wallclock poll is the correct primitive.
func waitForCommits(t *testing.T, svc *CardService, wantCommits int, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		count, err := svc.git.CommitCount()

		return err == nil && count >= wantCommits
	}, timeout, 10*time.Millisecond, "timed out waiting for %d commits", wantCommits)
}

// TestAsyncCommit_HeartbeatFanoutAcrossCards verifies that heartbeat commits
// from many concurrent goroutines all land in git log and complete within a
// reasonable wall-clock budget.
func TestAsyncCommit_HeartbeatFanoutAcrossCards(t *testing.T) {
	svc, _, _ := newAsyncTestService(t)

	ctx := context.Background()

	const numCards = 20

	cardIDs := make([]string, numCards)

	for i := 0; i < numCards; i++ {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Heartbeat Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		cardIDs[i] = card.ID

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, fmt.Sprintf("agent-%d", i))
		require.NoError(t, err)
	}

	// Record baseline commit count: numCards (create) + numCards (claim).
	baseline, err := svc.git.CommitCount()
	require.NoError(t, err)

	var (
		wg       sync.WaitGroup
		errCount atomic.Int32
	)

	start := time.Now()

	for i := 0; i < numCards; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			if err := svc.HeartbeatCard(ctx, "test-project", cardIDs[i], fmt.Sprintf("agent-%d", i)); err != nil {
				errCount.Add(1)
				t.Errorf("heartbeat %d: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)

	assert.Zero(t, errCount.Load())

	// Confirm every heartbeat produced a commit.
	waitForCommits(t, svc, baseline+numCards, 10*time.Second)

	// Logging as info — not a strict assertion since CI wall clocks vary,
	// but a sequential worst case would be ~numCards * per-commit latency
	// (typically 10–40ms each on this repo). We only assert it does not
	// exceed a generous upper bound.
	assert.Less(t, elapsed, 10*time.Second,
		"heartbeat fanout took longer than expected; elapsed=%s", elapsed)
}

// TestAsyncCommit_ShutdownDrainsPendingHeartbeat enqueues a commit and then
// immediately closes the queue, asserting that the commit still lands and
// the Close call waits for it to finish.
func TestAsyncCommit_ShutdownDrainsPendingHeartbeat(t *testing.T) {
	svc, q, _ := newAsyncTestService(t)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Shutdown",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	baseline, err := svc.git.CommitCount()
	require.NoError(t, err)

	require.NoError(t, svc.HeartbeatCard(ctx, "test-project", card.ID, "agent-1"))

	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, q.Close(closeCtx))

	count, err := svc.git.CommitCount()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, baseline+1,
		"expected commit to land before queue close; count=%d baseline=%d", count, baseline)
}

// TestAsyncCommit_LockWritesPausesQueue verifies that LockWrites pauses
// the queue and drains in-flight commits — matching the rebase-gate
// requirement from the gitsync layer.
func TestAsyncCommit_LockWritesPausesQueue(t *testing.T) {
	svc, q, boardsDir := newAsyncTestService(t)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Lock",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Mutate the card file so the staged commit has something to record.
	cardFile := filepath.Join(boardsDir, "test-project", "tasks", card.ID+".md")

	data, err := os.ReadFile(cardFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cardFile, append(data, []byte("\nmarker\n")...), 0o644))

	// Hold LockWrites; while held, new enqueued jobs should not run.
	svc.LockWrites()

	// Enqueue a job directly; should buffer but not execute.
	done := q.Enqueue(gitops.CommitJob{
		Project: "test-project",
		Kind:    gitops.CommitKindFile,
		Path:    fmt.Sprintf("test-project/tasks/%s.md", card.ID),
		Message: "manual-paused",
		Ctx:     ctx,
	})

	select {
	case <-done:
		t.Fatal("queue executed a job while writes are locked")
	case <-time.After(30 * time.Millisecond):
	}

	svc.UnlockWrites()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("queue did not resume after UnlockWrites")
	}
}
