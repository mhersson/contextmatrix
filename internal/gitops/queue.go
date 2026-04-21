package gitops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

// CommitKind identifies how a CommitJob should be executed. It selects which
// Manager method the worker calls.
type CommitKind int

const (
	// CommitKindFile uses go-git (Manager.CommitFile) to stage and commit one file.
	CommitKindFile CommitKind = iota
	// CommitKindFiles uses go-git (Manager.CommitFiles) to stage and commit multiple files.
	CommitKindFiles
	// CommitKindFilesShell uses shell git (Manager.CommitFilesShell) and is
	// immune to stale in-memory state after shell push/rebase.
	CommitKindFilesShell
)

// CommitJob describes a single git commit to be performed by the CommitQueue.
// Exactly one of Path / Paths is required depending on Kind.
//
// The embedded context is deliberate: jobs live asynchronously on a worker
// queue, so the context must travel with the job rather than with a single
// caller-side function invocation.
//
//nolint:containedctx // async job carries its own caller context.
type CommitJob struct {
	// Project is the board project name; commits for the same project are
	// serialized through a single worker so order is preserved.
	Project string
	Kind    CommitKind
	Path    string   // for CommitKindFile
	Paths   []string // for CommitKindFiles / CommitKindFilesShell
	Message string
	// ReloadAfter, when true, refreshes go-git's in-memory repo state after a
	// successful commit. Used for shell-git commits so subsequent go-git
	// reads see the new commit (mirrors flushDeferredCommit's current
	// behavior).
	ReloadAfter bool
	// Ctx is the caller's context; if cancelled before the worker picks up
	// the job, the job fails immediately with the context error.
	Ctx context.Context
	// Done receives the result. Buffered(1) so workers never block on send
	// when a caller gives up. Closed after the single result is sent.
	Done chan error
}

// ErrQueueClosed is returned when Enqueue is called after Close.
var ErrQueueClosed = errors.New("commit queue closed")

// Committer is the narrow interface the queue requires from a Manager. It
// is exported so tests in other packages can wire a failing committer into
// a CommitQueue via NewCommitQueueWithCommitter.
type Committer interface {
	CommitFile(ctx context.Context, path, message string) error
	CommitFiles(ctx context.Context, paths []string, message string) error
	CommitFilesShell(ctx context.Context, paths []string, message string) error
	ReloadRepo(ctx context.Context) error
}

// committer is kept as an internal alias so existing references in this
// package (and the queue's mgr field) compile without touching every
// call site.
type committer = Committer

// CommitQueue serializes git commits per project via dedicated worker
// goroutines. It decouples the caller's request path from the on-disk
// go-git latency and allows different projects to commit in parallel.
//
// Ordering guarantee: for a fixed Project, commits execute in enqueue
// order. No ordering is implied across projects.
//
// Lifecycle: create with NewCommitQueue, then Start(ctx). Call Close(ctx)
// on shutdown to drain all pending jobs before the process exits.
type CommitQueue struct {
	mgr     committer
	onAfter func() // optional hook, called after a successful commit

	mu       sync.Mutex
	workers  map[string]chan CommitJob // project -> job channel
	closed   bool
	paused   bool
	pauseCh  chan struct{} // closed when unpaused; non-nil only while paused
	inflight int           // workers currently executing a commit
	// idleCh is closed when inflight transitions from >0 to 0. A fresh
	// channel is installed (still closed) while idle; when inflight first
	// transitions from 0 to >0 the channel is replaced with a new open one
	// that is closed again on the next drop-to-zero. AwaitIdle selects on
	// the channel + ctx.Done(), so a cancelled context never parks a
	// goroutine inside a sync.Cond wait.
	idleCh chan struct{}

	wg sync.WaitGroup

	// queueBuf is the per-project channel buffer. Exposed for tests.
	queueBuf int
}

// NewCommitQueue constructs a queue that routes commits through mgr.
// The queue is not started until Start is called.
//
// bufferSize bounds each per-project job channel; a value <= 0 defaults
// to 1024. The total in-flight backlog is therefore N*bufferSize for N
// active projects, which is plenty in practice (cards-per-second per
// project is tiny compared to this).
func NewCommitQueue(mgr *Manager, bufferSize int) *CommitQueue {
	return newCommitQueueFromCommitter(mgr, bufferSize)
}

// NewCommitQueueWithCommitter constructs a queue backed by any Committer
// implementation. Intended for cross-package tests that need to inject a
// fake committer (e.g. one that always fails) to exercise the service
// layer's commit-failure rollback path.
func NewCommitQueueWithCommitter(c Committer, bufferSize int) *CommitQueue {
	return newCommitQueueFromCommitter(c, bufferSize)
}

func newCommitQueueFromCommitter(c committer, bufferSize int) *CommitQueue {
	if bufferSize <= 0 {
		bufferSize = 1024
	}

	q := &CommitQueue{
		mgr:      c,
		workers:  make(map[string]chan CommitJob),
		queueBuf: bufferSize,
	}
	// Start idle: install a pre-closed channel so AwaitIdle returns
	// immediately while nothing is in flight.
	q.idleCh = make(chan struct{})
	close(q.idleCh)

	return q
}

// SetOnCommit registers a callback invoked after each successful commit.
// Safe to call any time before workers start.
func (q *CommitQueue) SetOnCommit(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.onAfter = fn
}

// Enqueue submits a commit job. Returns a channel that receives the
// result (nil on success) when the worker finishes. The returned channel
// is the same as job.Done — assigned if nil. Safe to call concurrently.
//
// If the queue is closed, Enqueue returns ErrQueueClosed via the channel
// synchronously (a pre-closed done channel with the error already sent).
//
// Non-atomicity contract: Enqueue only runs the git commit. Callers are
// expected to have already written the new state to the store (cache +
// disk) before enqueueing, so a successful store write with a failed
// commit leaves the cache and disk ahead of git. Callers that need the
// two to stay consistent must roll back their store state when the
// channel yields a non-nil error. See service.applyCardMutation for the
// reference rollback pattern.
func (q *CommitQueue) Enqueue(job CommitJob) <-chan error {
	if job.Done == nil {
		job.Done = make(chan error, 1)
	}

	if job.Ctx == nil {
		job.Ctx = context.Background()
	}

	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()

		job.Done <- ErrQueueClosed

		close(job.Done)

		return job.Done
	}

	ch, ok := q.workers[job.Project]
	if !ok {
		ch = make(chan CommitJob, q.queueBuf)
		q.workers[job.Project] = ch

		q.wg.Add(1)

		go q.run(job.Project, ch)
	}

	q.mu.Unlock()

	// Track queue depth as "buffered jobs not yet picked up".
	metrics.CommitQueueDepth.Inc()

	ch <- job

	return job.Done
}

// run consumes jobs for a single project. It exits when the input
// channel is closed and drained.
func (q *CommitQueue) run(project string, jobs <-chan CommitJob) {
	defer q.wg.Done()

	for job := range jobs {
		// Depth drops when the job is taken off the channel.
		metrics.CommitQueueDepth.Dec()

		// If the queue is paused (e.g. rebase in progress), wait until
		// it resumes before executing. We copy pauseCh while holding the
		// lock to observe a consistent state.
		q.waitUnpaused(job.Ctx)

		if err := job.Ctx.Err(); err != nil {
			job.Done <- err

			close(job.Done)

			continue
		}

		q.markBusy()
		err := q.execute(job)
		q.markIdle()

		job.Done <- err

		close(job.Done)

		if err == nil && q.onAfter != nil {
			q.onAfter()
		}
	}

	_ = project // kept as doc marker for per-project worker
}

// execute runs the job with metrics instrumentation.
func (q *CommitQueue) execute(job CommitJob) error {
	start := time.Now()

	var err error

	switch job.Kind {
	case CommitKindFile:
		err = q.mgr.CommitFile(job.Ctx, job.Path, job.Message)
	case CommitKindFiles:
		err = q.mgr.CommitFiles(job.Ctx, job.Paths, job.Message)
	case CommitKindFilesShell:
		err = q.mgr.CommitFilesShell(job.Ctx, job.Paths, job.Message)
	default:
		err = fmt.Errorf("commit queue: unknown kind %d", job.Kind)
	}

	metrics.CommitDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		metrics.CommitErrorsTotal.Inc()

		return err
	}

	if job.ReloadAfter {
		if rerr := q.mgr.ReloadRepo(job.Ctx); rerr != nil {
			// Non-fatal: log and continue. Callers previously treated
			// this as a warning too.
			slog.Warn("commit queue: reload repo after commit",
				"project", job.Project, "error", rerr)
		}
	}

	return nil
}

// waitUnpaused blocks until the queue is not paused, or until ctx is
// cancelled. Safe to call without holding the queue lock.
func (q *CommitQueue) waitUnpaused(ctx context.Context) {
	for {
		q.mu.Lock()

		if !q.paused {
			q.mu.Unlock()

			return
		}

		ch := q.pauseCh
		q.mu.Unlock()

		select {
		case <-ch:
			// loop around and re-check (resumed).
		case <-ctx.Done():
			return
		}
	}
}

// markBusy increments the in-flight counter. On the 0→1 transition it
// installs a fresh open idle channel so AwaitIdle callers park on the new
// channel; the previously-closed channel is left for any callers that
// already captured it (they will observe idle, which is correct — the
// queue was idle at that instant).
func (q *CommitQueue) markBusy() {
	q.mu.Lock()
	if q.inflight == 0 {
		q.idleCh = make(chan struct{})
	}

	q.inflight++
	q.mu.Unlock()
}

// markIdle decrements the in-flight counter and, on the 1→0 transition,
// closes the current idle channel so AwaitIdle callers unblock.
func (q *CommitQueue) markIdle() {
	q.mu.Lock()
	q.inflight--

	if q.inflight == 0 {
		close(q.idleCh)
	}

	q.mu.Unlock()
}

// Pause prevents workers from starting new commits. Jobs already in
// flight finish normally; buffered jobs remain in the queue. Idempotent.
// Use AwaitIdle to wait until every worker is quiescent.
func (q *CommitQueue) Pause() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.paused {
		return
	}

	q.paused = true
	q.pauseCh = make(chan struct{})
}

// Resume lifts a prior Pause. Idempotent.
func (q *CommitQueue) Resume() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.paused {
		return
	}

	q.paused = false
	close(q.pauseCh)

	q.pauseCh = nil
}

// AwaitIdle blocks until no worker is executing a commit (inflight == 0)
// or ctx is cancelled. Buffered jobs still in channels do not count —
// only actively running commits.
//
// Typical use: Pause, AwaitIdle(ctx) — by the time this returns, no
// commit subprocess is racing against an external shell git operation.
//
// Implementation note: this waits on the queue's idleCh channel rather
// than a sync.Cond so a cancelled context returns immediately without
// leaving a helper goroutine parked in Cond.Wait until the queue
// naturally drains.
func (q *CommitQueue) AwaitIdle(ctx context.Context) error {
	q.mu.Lock()
	// Fast path: already idle. idleCh is closed at rest, so the receive
	// would complete immediately anyway, but taking the fast path keeps
	// the call allocation-free under the common case.
	if q.inflight == 0 {
		q.mu.Unlock()

		return nil
	}

	ch := q.idleCh
	q.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the queue and drains all buffered jobs. After Close
// returns, Enqueue rejects new jobs with ErrQueueClosed.
//
// Close blocks until either all workers finish or ctx is cancelled.
// Pending jobs that cannot run before ctx deadline return context errors
// on their Done channels.
func (q *CommitQueue) Close(ctx context.Context) error {
	q.mu.Lock()

	if q.closed {
		q.mu.Unlock()

		return nil
	}

	q.closed = true

	// Copy channels so we can close them without holding the lock.
	channels := make([]chan CommitJob, 0, len(q.workers))
	for _, ch := range q.workers {
		channels = append(channels, ch)
	}
	// If paused, resume so workers can drain. Shutdown overrides pause.
	if q.paused {
		q.paused = false
		close(q.pauseCh)

		q.pauseCh = nil
	}
	q.mu.Unlock()

	for _, ch := range channels {
		close(ch)
	}

	done := make(chan struct{})

	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
