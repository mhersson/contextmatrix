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
	// CommitKindAll uses go-git (Manager.CommitAll) to stage every change in
	// the worktree and commit. Used for project-level events (create/delete)
	// where the set of files touched is not known in advance.
	CommitKindAll
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
	CommitAll(ctx context.Context, message string) error
	ReloadRepo(ctx context.Context) error
}

// CommitQueue serializes git commits per project via dedicated worker
// goroutines. It decouples the caller's request path from the on-disk
// go-git latency and allows different projects to commit in parallel.
//
// Ordering guarantee: for a fixed Project, commits execute in enqueue
// order. No ordering is implied across projects.
//
// Lifecycle: create with NewCommitQueue; per-project workers spawn lazily on
// Enqueue and run until Close. Call Close(ctx) on shutdown to drain all
// pending jobs before the process exits.
type CommitQueue struct {
	mgr Committer

	mu       sync.Mutex
	workers  map[string]chan CommitJob
	closed   bool
	paused   bool
	pauseCh  chan struct{} // closed when unpaused; non-nil only while paused
	inflight int           // workers currently executing a commit
	// pending counts jobs Enqueue has accepted and processJob has not yet
	// finished. Unlike inflight it includes jobs still sitting in a
	// per-project channel, which is what Drain has to wait out.
	pending int
	// idleCh is closed when inflight transitions from >0 to 0. A fresh
	// channel is installed (still closed) while idle; when inflight first
	// transitions from 0 to >0 the channel is replaced with a new open one
	// that is closed again on the next drop-to-zero. AwaitIdle selects on
	// the channel + ctx.Done(), so a cancelled context never parks a
	// goroutine inside a sync.Cond wait.
	idleCh chan struct{}

	wg sync.WaitGroup
	// sending counts Enqueue calls between the closed check and their
	// channel send; Close waits it out before closing any channel.
	sending sync.WaitGroup

	// queueBuf is the per-project channel buffer. Exposed for tests.
	queueBuf int
}

// NewCommitQueue constructs a queue that routes commits through mgr.
//
// bufferSize bounds each per-project job channel; a value <= 0 defaults
// to 1024. The total in-flight backlog is therefore N*bufferSize for N
// active projects, which is plenty in practice (cards-per-second per
// project is tiny compared to this).
func NewCommitQueue(mgr *Manager, bufferSize int) *CommitQueue {
	return NewCommitQueueWithCommitter(mgr, bufferSize)
}

// NewCommitQueueWithCommitter constructs a queue backed by any Committer
// implementation. Intended for cross-package tests that need to inject a
// fake committer (e.g. one that always fails) to exercise the service
// layer's commit-failure rollback path.
func NewCommitQueueWithCommitter(c Committer, bufferSize int) *CommitQueue {
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

// Enqueue submits a commit job. Returns a channel that receives the
// result (nil on success) when the worker finishes. The returned channel
// is the same as job.Done - assigned if nil. Safe to call concurrently.
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

		go q.run(ch)
	}
	// Count the job under q.mu, before Close can flip closed: pending lets
	// Drain wait it out, sending keeps Close from closing ch mid-send.
	q.pending++
	q.sending.Add(1)
	q.mu.Unlock()

	metrics.CommitQueueDepth.Inc()

	// Sent outside q.mu: a full buffer back-pressures Enqueue while the
	// worker, which takes q.mu per job, keeps draining.
	ch <- job

	q.sending.Done()

	return job.Done
}

// run commits one project's jobs in enqueue order until Close closes ch and
// the buffered jobs are drained.
func (q *CommitQueue) run(ch <-chan CommitJob) {
	defer q.wg.Done()

	for job := range ch {
		q.processJob(job)
	}
}

// processJob runs one CommitJob through the pause/markBusy/execute pipeline
// and signals the caller via job.Done.
func (q *CommitQueue) processJob(job CommitJob) {
	// Every path out of this function - commit, failure, or a context
	// cancelled before pickup - retires the job Enqueue counted.
	defer func() {
		q.mu.Lock()
		q.pending--
		q.mu.Unlock()
	}()

	// Depth drops when the job is taken off the channel.
	metrics.CommitQueueDepth.Dec()

	// If the queue is paused (e.g. rebase in progress), wait until it
	// resumes before executing.
	q.waitUnpaused(job.Ctx)

	if err := job.Ctx.Err(); err != nil {
		// Distinguish cancellations from commit failures: the commit
		// never ran, so this is not a commit error. Operators alerting
		// on CommitErrorsTotal would get spurious pages from shutdown
		// drains otherwise.
		metrics.CommitCancellationsTotal.Inc()

		job.Done <- err

		close(job.Done)

		return
	}

	q.markBusy()
	err := q.execute(job)
	q.markIdle()

	job.Done <- err

	close(job.Done)
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
	case CommitKindAll:
		err = q.mgr.CommitAll(job.Ctx, job.Message)
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
			// If the caller's context cancelled mid-flight (shutdown,
			// caller hang-up), the commit itself succeeded and HEAD is
			// already on disk - only the in-memory go-git state is
			// stale. Retry the reload with a fresh background context
			// so the next read sees the new HEAD; otherwise the next
			// shell-git pull/push would observe the same stale state.
			if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
				slog.Warn("commit queue: reload after commit ctx cancelled, retrying with background ctx",
					"project", job.Project, "error", rerr)

				if rerr2 := q.mgr.ReloadRepo(context.Background()); rerr2 != nil {
					slog.Warn("commit queue: reload after commit failed on retry",
						"project", job.Project, "error", rerr2)
				}
			} else {
				// Non-fatal: log and continue.
				slog.Warn("commit queue: reload repo after commit",
					"project", job.Project, "error", rerr)
			}
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
// already captured it (they will observe idle, which is correct - the
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
// or ctx is cancelled. Buffered jobs still in channels do not count -
// only actively running commits.
//
// Typical use: Pause, AwaitIdle(ctx) - by the time this returns, no
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

// Drain runs whatever is buffered plus whatever arrives while it runs, then
// pauses. Unlike AwaitIdle, which only waits out the commits already
// executing, Drain also waits for jobs still sitting in a per-project
// channel - required before a shell merge, which needs every card change
// already committed.
//
// Drain does not assume the queue is quiescent. Holding the card write lock
// stops card commits from arriving, but the queue is shared: a playbook
// commit can still enqueue during the window Drain leaves the queue resumed.
// Such a job is waited out because Enqueue counts it under q.mu before the
// channel send, so Drain cannot observe zero while a job is in flight. Do
// not move that increment after the send. A job that races in after Drain
// has paused simply waits, buffered, until the caller resumes the queue.
//
// Quiescence across both writers is the caller's job, not this method's: see
// the lock-ordering contract on CardService.LockWrites. On a cancelled
// context the queue is still left paused and the context error is returned.
func (q *CommitQueue) Drain(ctx context.Context) error {
	q.Resume()

	for {
		q.mu.Lock()
		pending := q.pending
		q.mu.Unlock()

		if pending == 0 {
			q.Pause()

			return nil
		}

		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			q.Pause()

			return ctx.Err()
		}
	}
}

// Close stops the queue and drains all buffered jobs. After Close
// returns, Enqueue rejects new jobs with ErrQueueClosed.
//
// Close blocks until either all workers finish or ctx is cancelled. On
// cancellation it returns ctx.Err() and the drain carries on in the
// background.
func (q *CommitQueue) Close(ctx context.Context) error {
	q.mu.Lock()

	if q.closed {
		q.mu.Unlock()

		return nil
	}

	q.closed = true

	// Snapshot the per-project workers so we can close their channels
	// without holding the queue lock.
	workers := make([]chan CommitJob, 0, len(q.workers))
	for _, ch := range q.workers {
		workers = append(workers, ch)
	}
	// If paused, resume so workers can drain. Shutdown overrides pause.
	if q.paused {
		q.paused = false
		close(q.pauseCh)

		q.pauseCh = nil
	}
	q.mu.Unlock()

	done := make(chan struct{})

	go func() {
		// Every Enqueue that passed the closed check registered on sending
		// first, so once it drains no send can hit a closed channel.
		q.sending.Wait()

		for _, ch := range workers {
			close(ch)
		}

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
