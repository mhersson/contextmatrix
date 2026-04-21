package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/storage"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// failingCommitter is a gitops.Committer that always returns errFailing
// so tests can exercise the rollback path without needing to corrupt a
// real git repo.
type failingCommitter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *failingCommitter) CommitFile(_ context.Context, _, _ string) error {
	return f.record()
}

func (f *failingCommitter) CommitFiles(_ context.Context, _ []string, _ string) error {
	return f.record()
}

func (f *failingCommitter) CommitFilesShell(_ context.Context, _ []string, _ string) error {
	return f.record()
}

func (f *failingCommitter) ReloadRepo(_ context.Context) error {
	return nil
}

func (f *failingCommitter) record() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.err
}

// TestApplyCardMutation_RollbackOnCommitFailure verifies that when the
// async commit fails, the card's state in the cache + disk is restored
// to the pre-mutation snapshot and the returned error references the
// commit failure.
func TestApplyCardMutation_RollbackOnCommitFailure(t *testing.T) {
	// Bootstrap the card with a working Manager first, then swap the
	// queue to the failing committer so only the mutation's commit fails.
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Original Title",
		Type:     "task",
		Priority: "medium",
		Body:     "Original body",
	})
	require.NoError(t, err)

	preState := card.State
	preTitle := card.Title
	preBody := card.Body

	// Now swap in the failing queue so the next mutation cannot commit.
	sentinel := errors.New("commit boom")

	failing := &failingCommitter{err: sentinel}
	failQueue := gitops.NewCommitQueueWithCommitter(failing, 0)

	t.Cleanup(func() { _ = failQueue.Close(context.Background()) })

	svc.SetCommitQueue(failQueue)

	// Mutate the card via PatchCard (routes through applyCardMutation).
	newTitle := "Mutated Title"
	newBody := "Mutated body"
	immediate := true

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title:           &newTitle,
		Body:            &newBody,
		ImmediateCommit: immediate,
	})
	require.Error(t, err, "commit failure must propagate to caller")
	require.ErrorIs(t, err, sentinel, "returned error must wrap the commit error")
	assert.Contains(t, err.Error(), "git commit")

	// Card must now read as pre-mutation.
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, preTitle, reloaded.Title, "title should be rolled back")
	assert.Equal(t, preBody, reloaded.Body, "body should be rolled back")
	assert.Equal(t, preState, reloaded.State, "state should be unchanged")

	// And the failing committer must have been exercised at least once.
	failing.mu.Lock()
	calls := failing.calls
	failing.mu.Unlock()
	assert.Positive(t, calls, "failing committer should have been called")
}

// TestApplyCardMutation_RollbackOnCommitFailure_DiskConsistent asserts
// the rollback writes back to disk, not just to the in-memory cache.
func TestApplyCardMutation_RollbackOnCommitFailure_DiskConsistent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Original Title",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	preTitle := card.Title

	sentinel := errors.New("commit boom")

	failing := &failingCommitter{err: sentinel}
	failQueue := gitops.NewCommitQueueWithCommitter(failing, 0)

	t.Cleanup(func() { _ = failQueue.Close(context.Background()) })

	svc.SetCommitQueue(failQueue)

	newTitle := "Mutated Title"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title:           &newTitle,
		ImmediateCommit: true,
	})
	require.Error(t, err)

	// Re-open the filesystem store from scratch so we verify disk
	// content rather than any cached state.
	fresh, err := storage.NewFilesystemStore(svc.boardsDir)
	require.NoError(t, err)

	onDisk, err := fresh.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, preTitle, onDisk.Title, "on-disk title must match pre-mutation snapshot")
}

// TestAddLogEntry_RollbackOnCommitFailure verifies AddLogEntry rolls back
// the appended activity entry when the commit fails.
func TestAddLogEntry_RollbackOnCommitFailure(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Log Target",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	preLog := append([]board.ActivityEntry(nil), card.ActivityLog...)

	sentinel := errors.New("commit boom")
	failing := &failingCommitter{err: sentinel}
	failQueue := gitops.NewCommitQueueWithCommitter(failing, 0)

	t.Cleanup(func() { _ = failQueue.Close(context.Background()) })
	svc.SetCommitQueue(failQueue)

	err = svc.AddLogEntry(ctx, "test-project", card.ID, board.ActivityEntry{
		Agent:   "human:alice",
		Action:  "commented",
		Message: "this should not stick",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)

	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Len(t, reloaded.ActivityLog, len(preLog),
		"activity log should be rolled back to pre-mutation length")
}

// TestParentAutoTransition_FailedCommitIncrementsCounter verifies that a
// parent auto-transition that cannot commit increments the
// ParentAutoTransitionErrors counter. The child's own mutation must
// still succeed — auto-transitions are best-effort and their failure
// is surfaced via the metric, not the caller's error.
func TestParentAutoTransition_FailedCommitIncrementsCounter(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Parent in "todo".
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Child subtask that will transition to in_progress and trigger the
	// parent auto-transition.
	child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Child",
		Type:     "task",
		Priority: "medium",
		Parent:   parent.ID,
	})
	require.NoError(t, err)

	// Claim the child so it can go in_progress.
	_, err = svc.ClaimCard(ctx, "test-project", child.ID, "agent-1")
	require.NoError(t, err)

	// Swap in a fresh metric so other tests' increments do not pollute
	// this count. Reset by grabbing the baseline value.
	base := testutil.ToFloat64(metrics.ParentAutoTransitionErrors)

	// Prime a failing queue. The child's own PatchCard will fail to
	// commit; rollback restores the card, and the parent auto-transition
	// then runs. To isolate "parent commit failure counted by metric",
	// install the failing queue AFTER the child transitions, via a
	// different pathway: transition the child via TransitionTo so its
	// commit uses the sync path (no rollback), then trigger the parent
	// transition via maybeTransitionParent with a failing queue.
	//
	// Simplest: use transitionParentDirect's call site through
	// PatchCard-on-child-to-in_progress. But with a failing queue, the
	// child commit also fails + rolls back, and the parent never
	// transitions. To get the metric bump we need the child commit to
	// succeed and only the parent commit to fail.
	//
	// Workaround: wrap a committer that fails ONLY on messages that
	// contain "auto-transitioned". The child's commit message is
	// "updated", the parent's is "auto-transitioned to in_progress".
	selective := &selectiveFailingCommitter{
		pattern: "auto-transitioned",
		err:     errors.New("parent commit boom"),
		inner:   newRealCommitter(svc.git),
	}
	selQueue := gitops.NewCommitQueueWithCommitter(selective, 0)

	t.Cleanup(func() { _ = selQueue.Close(context.Background()) })
	svc.SetCommitQueue(selQueue)

	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", child.ID, PatchCardInput{
		State:           &inProgress,
		ImmediateCommit: true,
		AgentID:         "agent-1",
	})
	require.NoError(t, err, "child transition must succeed; only parent commit fails")

	// Parent auto-transition runs under writeMu before PatchCard
	// returns, so by the time we reach here the metric has been
	// incremented.
	got := testutil.ToFloat64(metrics.ParentAutoTransitionErrors)
	assert.InDelta(t, 1.0, got-base, 0.0001,
		"ParentAutoTransitionErrors should have incremented by 1 (base=%f, got=%f)", base, got)
}

// selectiveFailingCommitter forwards to inner but returns err for any
// commit whose message contains pattern. Used to simulate "only parent
// auto-transition commits fail" in TestParentAutoTransition_FailedCommitIncrementsCounter.
type selectiveFailingCommitter struct {
	pattern string
	err     error
	inner   gitops.Committer
}

func (s *selectiveFailingCommitter) CommitFile(ctx context.Context, path, message string) error {
	if contains(message, s.pattern) {
		return s.err
	}

	return s.inner.CommitFile(ctx, path, message)
}

func (s *selectiveFailingCommitter) CommitFiles(ctx context.Context, paths []string, message string) error {
	if contains(message, s.pattern) {
		return s.err
	}

	return s.inner.CommitFiles(ctx, paths, message)
}

func (s *selectiveFailingCommitter) CommitFilesShell(ctx context.Context, paths []string, message string) error {
	if contains(message, s.pattern) {
		return s.err
	}

	return s.inner.CommitFilesShell(ctx, paths, message)
}

func (s *selectiveFailingCommitter) ReloadRepo(ctx context.Context) error {
	return s.inner.ReloadRepo(ctx)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// realCommitter wraps a *gitops.Manager to satisfy the Committer interface
// without the cross-package visibility of the unexported committer alias.
type realCommitter struct {
	mgr *gitops.Manager
}

func newRealCommitter(mgr *gitops.Manager) *realCommitter { return &realCommitter{mgr: mgr} }

func (r *realCommitter) CommitFile(ctx context.Context, path, message string) error {
	return r.mgr.CommitFile(ctx, path, message)
}

func (r *realCommitter) CommitFiles(ctx context.Context, paths []string, message string) error {
	return r.mgr.CommitFiles(ctx, paths, message)
}

func (r *realCommitter) CommitFilesShell(ctx context.Context, paths []string, message string) error {
	return r.mgr.CommitFilesShell(ctx, paths, message)
}

func (r *realCommitter) ReloadRepo(ctx context.Context) error {
	return r.mgr.ReloadRepo(ctx)
}
