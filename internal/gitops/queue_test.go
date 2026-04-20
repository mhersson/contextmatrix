package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCommitter records calls and simulates delays for unit tests that
// exercise ordering, pausing, and shutdown without spinning go-git.
type fakeCommitter struct {
	mu     sync.Mutex
	calls  []fakeCall
	delay  time.Duration
	failOn map[string]error
}

type fakeCall struct {
	kind    CommitKind
	path    string
	paths   []string
	message string
}

func (f *fakeCommitter) CommitFile(_ context.Context, path, message string) error {
	return f.record(CommitKindFile, path, nil, message)
}

func (f *fakeCommitter) CommitFiles(_ context.Context, paths []string, message string) error {
	return f.record(CommitKindFiles, "", paths, message)
}

func (f *fakeCommitter) CommitFilesShell(_ context.Context, paths []string, message string) error {
	return f.record(CommitKindFilesShell, "", paths, message)
}

func (f *fakeCommitter) ReloadRepo(_ context.Context) error {
	return nil
}

func (f *fakeCommitter) record(kind CommitKind, path string, paths []string, message string) error {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, fakeCall{kind: kind, path: path, paths: paths, message: message})

	if err, ok := f.failOn[message]; ok {
		return err
	}

	return nil
}

func (f *fakeCommitter) snapshot() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]fakeCall, len(f.calls))
	copy(out, f.calls)

	return out
}

// newFakeQueue builds a queue backed by a fake committer. Safe for tests
// that do not need real git output.
func newFakeQueue(t *testing.T, delay time.Duration) (*CommitQueue, *fakeCommitter) {
	t.Helper()

	f := &fakeCommitter{delay: delay}
	q := &CommitQueue{
		mgr:      f,
		workers:  make(map[string]chan CommitJob),
		queueBuf: 16,
	}
	q.idleCond = sync.NewCond(&q.mu)

	return q, f
}

func TestCommitQueue_PerProjectOrdering(t *testing.T) {
	q, fake := newFakeQueue(t, 5*time.Millisecond)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	const n = 10

	dones := make([]<-chan error, n)
	for i := range n {
		dones[i] = q.Enqueue(CommitJob{
			Project: "alpha",
			Kind:    CommitKindFile,
			Path:    fmt.Sprintf("alpha/tasks/card-%02d.md", i),
			Message: fmt.Sprintf("msg-%02d", i),
		})
	}

	for i, d := range dones {
		require.NoError(t, <-d, "job %d", i)
	}

	calls := fake.snapshot()
	require.Len(t, calls, n)

	for i, c := range calls {
		assert.Equal(t, fmt.Sprintf("msg-%02d", i), c.message,
			"FIFO order violated at index %d", i)
	}
}

func TestCommitQueue_CrossProjectParallelism(t *testing.T) {
	// Use a 20ms delay so a sequential execution would take 200ms+ for
	// ten jobs. With per-project workers, ten jobs on ten projects
	// should finish in roughly one delay cycle (plus overhead).
	q, fake := newFakeQueue(t, 20*time.Millisecond)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	const n = 10

	start := time.Now()

	dones := make([]<-chan error, n)
	for i := range n {
		dones[i] = q.Enqueue(CommitJob{
			Project: fmt.Sprintf("proj-%d", i),
			Kind:    CommitKindFile,
			Path:    "tasks/x.md",
			Message: fmt.Sprintf("msg-%d", i),
		})
	}

	for _, d := range dones {
		require.NoError(t, <-d)
	}

	elapsed := time.Since(start)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"expected parallel execution across projects; elapsed=%s", elapsed)
	assert.Len(t, fake.snapshot(), n)
}

func TestCommitQueue_PauseAndResume(t *testing.T) {
	q, fake := newFakeQueue(t, 0)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	q.Pause()

	done := q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "alpha/tasks/a.md",
		Message: "paused",
	})

	select {
	case <-done:
		t.Fatal("commit ran while queue was paused")
	case <-time.After(50 * time.Millisecond):
	}

	q.Resume()

	require.NoError(t, <-done)
	assert.Len(t, fake.snapshot(), 1)
}

func TestCommitQueue_AwaitIdleDuringBusyWorker(t *testing.T) {
	q, _ := newFakeQueue(t, 40*time.Millisecond)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "alpha/tasks/a.md",
		Message: "slow",
	})

	// Give the worker a moment to pick up the job.
	time.Sleep(5 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := q.AwaitIdle(ctx)
	assert.NoError(t, err)
}

func TestCommitQueue_AwaitIdleContextDeadline(t *testing.T) {
	q, _ := newFakeQueue(t, 200*time.Millisecond)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "alpha/tasks/a.md",
		Message: "slow",
	})

	time.Sleep(5 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := q.AwaitIdle(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCommitQueue_CloseDrainsBufferedJobs(t *testing.T) {
	q, fake := newFakeQueue(t, 0)

	const n = 5

	dones := make([]<-chan error, n)
	for i := range n {
		dones[i] = q.Enqueue(CommitJob{
			Project: "alpha",
			Kind:    CommitKindFile,
			Path:    "alpha/tasks/a.md",
			Message: fmt.Sprintf("buffered-%d", i),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, q.Close(ctx))

	for _, d := range dones {
		require.NoError(t, <-d)
	}

	assert.Len(t, fake.snapshot(), n)
}

func TestCommitQueue_EnqueueAfterCloseFails(t *testing.T) {
	q, _ := newFakeQueue(t, 0)
	require.NoError(t, q.Close(context.Background()))

	done := q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "a.md",
		Message: "late",
	})

	err := <-done
	assert.ErrorIs(t, err, ErrQueueClosed)
}

func TestCommitQueue_PropagatesCommitErrors(t *testing.T) {
	q, fake := newFakeQueue(t, 0)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	sentinel := errors.New("boom")
	fake.failOn = map[string]error{"boom-msg": sentinel}

	done := q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "a.md",
		Message: "boom-msg",
	})

	err := <-done
	assert.ErrorIs(t, err, sentinel)
}

func TestCommitQueue_CtxCancelledBeforePickup(t *testing.T) {
	q, fake := newFakeQueue(t, 0)
	t.Cleanup(func() { _ = q.Close(context.Background()) })

	// Pause so the job sits in the buffer.
	q.Pause()

	ctx, cancel := context.WithCancel(context.Background())

	done := q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "a.md",
		Message: "cancelled",
		Ctx:     ctx,
	})

	cancel()
	q.Resume()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	// No commit should have been recorded.
	assert.Empty(t, fake.snapshot())
}

// TestCommitQueue_RealManagerConcurrency exercises the queue against a real
// go-git Manager, asserting that commits from 20 concurrent goroutines all
// land in the repository without deadlock.
func TestCommitQueue_RealManagerConcurrency(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir, "", "ssh", "")
	require.NoError(t, err)

	q := NewCommitQueue(mgr, 0)

	t.Cleanup(func() { _ = q.Close(context.Background()) })

	const (
		numGoroutines = 20
		numProjects   = 4
	)

	// Seed project directories + files so every commit has something to stage.
	for p := 0; p < numProjects; p++ {
		proj := fmt.Sprintf("proj-%d", p)
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, proj, "tasks"), 0o755))
	}

	var (
		wg          sync.WaitGroup
		successCnt  atomic.Int32
		errorsFound atomic.Int32
	)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			proj := fmt.Sprintf("proj-%d", i%numProjects)
			rel := filepath.Join(proj, "tasks", fmt.Sprintf("card-%02d.md", i))
			full := filepath.Join(tmpDir, rel)

			if err := os.WriteFile(full, []byte(fmt.Sprintf("body %d", i)), 0o644); err != nil {
				errorsFound.Add(1)
				t.Errorf("write file: %v", err)

				return
			}

			done := q.Enqueue(CommitJob{
				Project: proj,
				Kind:    CommitKindFile,
				Path:    rel,
				Message: fmt.Sprintf("add card %02d", i),
			})
			if err := <-done; err != nil {
				errorsFound.Add(1)
				t.Errorf("commit %d: %v", i, err)

				return
			}

			successCnt.Add(1)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(numGoroutines), successCnt.Load())
	assert.Zero(t, errorsFound.Load())

	// Verify every commit actually landed in git history.
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	require.NoError(t, err)

	count := 0
	_ = iter.ForEach(func(_ *object.Commit) error {
		count++

		return nil
	})

	assert.Equal(t, numGoroutines, count, "expected %d commits in git log, got %d", numGoroutines, count)
}

// TestCommitQueue_ShutdownCompletesPendingJob covers the shutdown path where
// a job is already enqueued when Close is called; the job must still run to
// completion before Close returns.
func TestCommitQueue_ShutdownCompletesPendingJob(t *testing.T) {
	q, fake := newFakeQueue(t, 10*time.Millisecond)

	done := q.Enqueue(CommitJob{
		Project: "alpha",
		Kind:    CommitKindFile,
		Path:    "a.md",
		Message: "shutdown-pending",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, q.Close(ctx))

	select {
	case err := <-done:
		require.NoError(t, err)
	default:
		t.Fatal("done channel should be signalled after Close returns")
	}

	assert.Len(t, fake.snapshot(), 1)
}
