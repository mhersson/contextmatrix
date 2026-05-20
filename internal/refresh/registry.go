// Package refresh tracks in-flight knowledge-base refresh jobs keyed by
// (project, repo). State is in-memory only; a CM restart loses tracking
// but in-flight runner containers complete via MCP regardless.
package refresh

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/clock"
)

// State is the lifecycle phase of a refresh job.
type State string

const (
	// StateIdle is the zero value; a Job with no state set is idle.
	StateIdle      State = ""
	StatePlanning  State = "planning"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
)

// maxRegistrySize is the maximum number of concurrent (project, repo) entries.
// Acquire returns ErrRegistryFull once the map reaches this size.
const maxRegistrySize = 1024

// IsTerminal returns true for states that release the (project, repo) lock.
func (s State) IsTerminal() bool {
	return s == StateSucceeded || s == StateFailed
}

// Job is a single refresh-job lifecycle record. Three callers
// mutate it: the registry methods themselves (Acquire/MarkRunning/
// MarkTerminal), the MCP update_refresh_progress tool (UpdateProgress),
// and the WriteKnowledgeDocs service-layer side effect (MarkCommitted).
type Job struct {
	Project     string
	Repo        string
	State       State
	AgentID     string
	StartedAt   time.Time
	FinishedAt  *time.Time
	DocsTotal   int
	DocsDone    int
	CurrentDoc  string
	Committed   bool
	CommitSHA   string
	Error       string
	LastUpdated time.Time
}

// ErrJobInFlight is returned by Acquire when a job for (project, repo) is
// already in StatePlanning or StateRunning.
var ErrJobInFlight = errors.New("refresh job already in flight for (project, repo)")

// ErrJobNotFound is returned by mutator methods when the keyed job is
// absent from the registry. Callers should treat this as "not tracked"
// rather than as an error worth surfacing to the user.
var ErrJobNotFound = errors.New("refresh job not found")

// ErrRegistryFull is returned by Acquire when the registry has reached
// maxRegistrySize entries. This guards against unbounded map growth in
// pathological scenarios (e.g. a loop issuing Acquire for new keys).
var ErrRegistryFull = errors.New("refresh registry full; too many concurrent (project, repo) entries")

// ErrAlreadyTerminal is returned by MarkTerminal and FinalizeRunner when the
// job has already reached a terminal state. Callers should log+ignore this
// sentinel — a late runner callback racing with the janitor's PromoteStale,
// or two concurrent terminal callbacks, must not clobber the first outcome.
var ErrAlreadyTerminal = errors.New("refresh job is already in a terminal state")

// projectRepoKey is a comparable struct used as the registry map key. A
// struct key eliminates the ambiguous "project/repo" string concatenation
// that collapses when project or repo themselves contain a "/" separator.
type projectRepoKey struct {
	project string
	repo    string
}

// Registry holds the in-memory map of (project, repo) -> *Job.
// Read-only paths (Snapshot) acquire RLock; mutations acquire the exclusive
// Lock. Snapshots return value-copies of jobs so callers cannot mutate
// registry state through the returned values.
//
// active is a denormalised counter of non-terminal entries kept in sync with
// the map so Acquire's capacity check is O(1). It is incremented when a job
// enters a non-terminal state (Acquire) and decremented on every transition
// to a terminal state (MarkTerminal, FinalizeRunner, PromoteStale) and on
// GarbageCollectExpired removals of non-terminal entries (defence in depth —
// GC normally only reaps terminal entries).
type Registry struct {
	mu     sync.RWMutex
	jobs   map[projectRepoKey]*Job
	active int
	clk    clock.Clock
}

// NewRegistry returns a Registry using the real wall-clock. Tests that
// need fakeable time should construct via NewRegistryWithClock.
func NewRegistry() *Registry {
	return NewRegistryWithClock(clock.Real())
}

// NewRegistryWithClock returns a Registry using clk for all timestamps.
func NewRegistryWithClock(clk clock.Clock) *Registry {
	return &Registry{
		jobs: make(map[projectRepoKey]*Job),
		clk:  clk,
	}
}

// MarkRunning transitions an acquired job from StatePlanning to StateRunning
// and records DocsTotal. Returns ErrJobNotFound if no job exists for
// (project, repo). Errors if the job is in a terminal state (state machine
// violation). DocsTotal is set only if not already established (e.g. by an
// earlier UpdateProgress callback). The skill's count wins.
func (r *Registry) MarkRunning(project, repo string, docsTotal int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[projectRepoKey{project, repo}]
	if !ok {
		return ErrJobNotFound
	}

	if j.State.IsTerminal() {
		return fmt.Errorf("cannot mark running: job is terminal (%s)", j.State)
	}

	j.State = StateRunning
	if j.DocsTotal == 0 {
		j.DocsTotal = docsTotal
	}

	j.LastUpdated = r.clk.Now()

	return nil
}

// UpdateProgress records per-doc progress. Returns (tracked=true, nil) when
// the matching job exists and is non-terminal. Returns (tracked=false, nil)
// when no job is registered (local-mode no-op) or when the job is already
// terminal (a late callback must not revive it).
func (r *Registry) UpdateProgress(project, repo string, docsTotal, docsDone int, currentDoc string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[projectRepoKey{project, repo}]
	if !ok {
		return false, nil
	}

	if j.State.IsTerminal() {
		return false, nil
	}

	if docsTotal > 0 {
		j.DocsTotal = docsTotal
	}

	j.DocsDone = docsDone
	j.CurrentDoc = currentDoc
	j.LastUpdated = r.clk.Now()

	return true, nil
}

// MarkCommitted records that the container's commit_knowledge_docs succeeded.
// Called from the WriteKnowledgeDocs service-layer side effect on Refresh
// writes. Returns ErrJobNotFound when no job exists for (project, repo);
// callers that handle the local-mode (no-registry) case should swallow it
// with errors.Is(err, refresh.ErrJobNotFound).
func (r *Registry) MarkCommitted(project, repo, commitSHA string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[projectRepoKey{project, repo}]
	if !ok {
		return ErrJobNotFound
	}

	j.Committed = true
	j.CommitSHA = commitSHA
	j.LastUpdated = r.clk.Now()

	return nil
}

// MarkTerminal flips the job to a terminal state, sets FinishedAt, and
// records the error message (if any). Subsequent Acquire on the same key
// is allowed (until GC reaps the record). Returns ErrJobNotFound if no
// job exists for (project, repo). Returns ErrAlreadyTerminal if the job
// has already reached a terminal state — the first terminal outcome wins
// and a late callback (or PromoteStale racing a runner callback) must not
// clobber it.
func (r *Registry) MarkTerminal(project, repo string, state State, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !state.IsTerminal() {
		return fmt.Errorf("MarkTerminal requires a terminal state, got %s", state)
	}

	j, ok := r.jobs[projectRepoKey{project, repo}]
	if !ok {
		return ErrJobNotFound
	}

	if j.State.IsTerminal() {
		return ErrAlreadyTerminal
	}

	now := r.clk.Now()
	j.State = state
	j.Error = errMsg
	j.FinishedAt = &now
	j.LastUpdated = now
	r.active--

	return nil
}

// FinalizeRunner atomically reconciles the runner-reported exit state against
// the registry's Committed flag and flips the job to a terminal state. It is
// the runner-callback equivalent of Snapshot+MarkTerminal but holds the
// registry mutex across the read of Committed and the write of State, so a
// concurrent commit_knowledge_docs MarkCommitted cannot slip in between and
// be lost.
//
// Semantics mirror the runnerKnowledgeStatus callback:
//   - runnerState="succeeded" + Committed=true  → StateSucceeded
//   - runnerState="succeeded" + Committed=false → StateFailed("commit not observed")
//   - any other runnerState                     → StateFailed(runnerErr or fallback)
//
// Returns the post-update Job value-copy. Returns ErrJobNotFound when no
// entry exists for (project, repo) and ErrAlreadyTerminal when the job has
// already reached a terminal state; callers should log+ignore the latter so
// a late callback racing the janitor cannot clobber the first outcome.
func (r *Registry) FinalizeRunner(project, repo, runnerState, runnerErr string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[projectRepoKey{project, repo}]
	if !ok {
		return Job{}, ErrJobNotFound
	}

	if j.State.IsTerminal() {
		return *j, ErrAlreadyTerminal
	}

	terminalState := StateFailed
	terminalErr := runnerErr

	switch runnerState {
	case string(StateSucceeded):
		if j.Committed {
			terminalState = StateSucceeded
			terminalErr = ""
		} else if terminalErr == "" {
			terminalErr = "commit not observed"
		}
	default:
		if terminalErr == "" {
			terminalErr = "runner reported state " + runnerState
		}
	}

	now := r.clk.Now()
	j.State = terminalState
	j.Error = terminalErr
	j.FinishedAt = &now
	j.LastUpdated = now
	r.active--

	return *j, nil
}

// Snapshot returns a value-copy map of repo -> Job for the given
// project. Mutating returned entries does not affect registry state.
// Uses RLock so it does not block concurrent readers.
func (r *Registry) Snapshot(project string) map[string]Job {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]Job)

	for k, j := range r.jobs {
		if k.project == project {
			out[k.repo] = *j
		}
	}

	return out
}

// GarbageCollectExpired removes terminal jobs whose FinishedAt is older
// than now-keepWindow. Non-terminal jobs are never reaped — the active
// counter therefore stays unchanged on every removal.
func (r *Registry) GarbageCollectExpired(keepWindow time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := r.clk.Now().Add(-keepWindow)

	for k, j := range r.jobs {
		if !j.State.IsTerminal() {
			continue
		}

		if j.FinishedAt == nil {
			continue
		}

		if j.FinishedAt.Before(cutoff) {
			delete(r.jobs, k)
		}
	}
}

// PromoteStale flips stale jobs to StateFailed. Two kinds are promoted:
//
//   - StateRunning jobs whose LastUpdated is older than threshold (runner
//     has stopped reporting progress).
//   - StatePlanning jobs whose StartedAt is older than planningMaxAge (the
//     runner trigger handler never called MarkRunning, indicating a CM-side
//     bug or a crashed handler goroutine).
//
// Returns the total number of jobs promoted.
func (r *Registry) PromoteStale(threshold, planningMaxAge time.Duration, errMsg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clk.Now()
	runningCutoff := now.Add(-threshold)
	planningCutoff := now.Add(-planningMaxAge)
	count := 0

	for _, j := range r.jobs {
		switch j.State {
		case StateRunning:
			if j.LastUpdated.Before(runningCutoff) {
				j.State = StateFailed
				j.Error = errMsg
				finished := now
				j.FinishedAt = &finished
				j.LastUpdated = now
				count++

				r.active--
			}

		case StatePlanning:
			// Planning is expected to be short-lived: Acquire is called
			// immediately before the runner trigger in the same handler.
			// A Planning job older than planningMaxAge indicates the handler
			// crashed or was stuck; promote it so the lock is released.
			if j.StartedAt.Before(planningCutoff) {
				j.State = StateFailed
				j.Error = "stuck in planning state for >" + planningMaxAge.String() + "; handler may have crashed"
				finished := now
				j.FinishedAt = &finished
				j.LastUpdated = now
				count++

				r.active--
			}
		}
	}

	return count
}

// Acquire reserves the (project, repo) lock and returns a fresh job in
// StatePlanning. Returns ErrJobInFlight if a non-terminal job already
// exists for the pair. An existing terminal job for the pair is silently
// overwritten — a stale terminal record does not block new work. Returns
// ErrRegistryFull when the registry has already reached maxRegistrySize
// entries to prevent unbounded memory growth.
func (r *Registry) Acquire(project, repo, agentID string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := projectRepoKey{project, repo}

	existing, replacing := r.jobs[key]
	if replacing && !existing.State.IsTerminal() {
		return Job{}, fmt.Errorf("%w: %s/%s", ErrJobInFlight, project, repo)
	}

	// Only count non-terminal entries toward the cap. Terminal entries that
	// have not yet been GC'd should not prevent legitimate new work.
	if r.active >= maxRegistrySize {
		return Job{}, fmt.Errorf("%w (active entries: %d)", ErrRegistryFull, r.active)
	}

	now := r.clk.Now()
	j := &Job{
		Project:     project,
		Repo:        repo,
		State:       StatePlanning,
		AgentID:     agentID,
		StartedAt:   now,
		LastUpdated: now,
	}
	// Overwriting a terminal entry (replacing=true) does not increase the
	// active count because the slot it replaces was already terminal.
	r.jobs[key] = j
	r.active++

	return *j, nil
}
