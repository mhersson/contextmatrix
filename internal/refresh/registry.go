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

// Registry holds the in-memory map of (project, repo) -> *Job.
// All mutations take the registry's mutex. Snapshots return value-copies
// of jobs so callers cannot mutate registry state through the returned
// pointer.
type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
	clk  clock.Clock
}

// NewRegistry returns a Registry using the real wall-clock. Tests that
// need fakeable time should construct via NewRegistryWithClock.
func NewRegistry() *Registry {
	return NewRegistryWithClock(clock.Real())
}

// NewRegistryWithClock returns a Registry using clk for all timestamps.
func NewRegistryWithClock(clk clock.Clock) *Registry {
	return &Registry{
		jobs: make(map[string]*Job),
		clk:  clk,
	}
}

// jobKey is the deterministic registry-map key for (project, repo).
func jobKey(project, repo string) string {
	return project + "/" + repo
}

// MarkRunning transitions an acquired job from StatePlanning to StateRunning
// and records DocsTotal. Returns ErrJobNotFound if no job exists for
// (project, repo). Errors if the job is in a terminal state (state machine
// violation). DocsTotal is set only if not already established (e.g. by an
// earlier UpdateProgress callback). The skill's count wins.
func (r *Registry) MarkRunning(project, repo string, docsTotal int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobKey(project, repo)]
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

	j, ok := r.jobs[jobKey(project, repo)]
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
// writes. Missing-job is a no-op (local-mode case) — never an error, since
// local-mode refresh writes never acquired a job.
func (r *Registry) MarkCommitted(project, repo, commitSHA string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobKey(project, repo)]
	if !ok {
		return nil
	}

	j.Committed = true
	j.CommitSHA = commitSHA
	j.LastUpdated = r.clk.Now()

	return nil
}

// MarkTerminal flips the job to a terminal state, sets FinishedAt, and
// records the error message (if any). Subsequent Acquire on the same key
// is allowed (until GC reaps the record). Returns ErrJobNotFound if no
// job exists for (project, repo).
func (r *Registry) MarkTerminal(project, repo string, state State, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !state.IsTerminal() {
		return fmt.Errorf("MarkTerminal requires a terminal state, got %s", state)
	}

	j, ok := r.jobs[jobKey(project, repo)]
	if !ok {
		return ErrJobNotFound
	}

	now := r.clk.Now()
	j.State = state
	j.Error = errMsg
	j.FinishedAt = &now
	j.LastUpdated = now

	return nil
}

// Snapshot returns a value-copy map of repo -> Job for the given
// project. Mutating returned entries does not affect registry state.
func (r *Registry) Snapshot(project string) map[string]Job {
	r.mu.Lock()
	defer r.mu.Unlock()

	prefix := project + "/"
	out := make(map[string]Job)

	for k, j := range r.jobs {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			repo := k[len(prefix):]
			out[repo] = *j
		}
	}

	return out
}

// GarbageCollectExpired removes terminal jobs whose FinishedAt is older
// than now-keepWindow. Non-terminal jobs are never reaped.
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

// PromoteStale flips any job in StateRunning whose LastUpdated is older
// than now-threshold to StateFailed with the given error message. Returns
// the number of jobs promoted. Planning jobs are deliberately not
// promoted: they are short-lived (acquired immediately before the runner
// trigger in the same handler), so a Planning job that hangs is a CM-side
// bug, not a runner-side one.
func (r *Registry) PromoteStale(threshold time.Duration, errMsg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clk.Now()
	cutoff := now.Add(-threshold)
	count := 0

	for _, j := range r.jobs {
		if j.State != StateRunning {
			continue
		}

		if j.LastUpdated.Before(cutoff) {
			j.State = StateFailed
			j.Error = errMsg
			finished := now
			j.FinishedAt = &finished
			j.LastUpdated = now
			count++
		}
	}

	return count
}

// Acquire reserves the (project, repo) lock and returns a fresh job in
// StatePlanning. Returns ErrJobInFlight if a non-terminal job already
// exists for the pair. An existing terminal job for the pair is silently
// overwritten — a stale terminal record does not block new work.
func (r *Registry) Acquire(project, repo, agentID string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := jobKey(project, repo)
	if existing, ok := r.jobs[key]; ok && !existing.State.IsTerminal() {
		return Job{}, fmt.Errorf("%w: %s", ErrJobInFlight, key)
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
	r.jobs[key] = j

	return *j, nil
}
