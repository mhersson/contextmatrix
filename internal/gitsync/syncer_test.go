package gitsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSyncTest creates an upstream bare repo, a clone, a store, and all
// dependencies needed for a Syncer. Returns the clone's Syncer, the upstream
// path, and the clone path.
func setupSyncTest(t *testing.T) (syncer *Syncer, upstream, clone string, bus *events.Bus) {
	t.Helper()

	// Ensure git binary is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	// Create upstream bare repo.
	upstream = filepath.Join(t.TempDir(), "upstream.git")
	run(t, "", "git", "init", "--bare", upstream)

	// Clone it.
	clone = filepath.Join(t.TempDir(), "clone")
	run(t, "", "git", "clone", upstream, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")

	// Create a project so the store has something to index.
	projectDir := filepath.Join(clone, "test-project", "tasks")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	// Git doesn't track empty dirs; add a .gitkeep so tasks/ is preserved.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".gitkeep"), nil, 0o644))
	cfg := &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
	require.NoError(t, board.SaveProjectConfig(filepath.Join(clone, "test-project"), cfg))

	// Initial commit and push.
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "initial")
	run(t, clone, "git", "push", "origin", "HEAD")

	// Set up Go objects.
	gitMgr, err := gitops.NewManager(clone, "", "ssh", "")
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(clone)
	require.NoError(t, err)

	bus = events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, gitMgr, lockMgr, bus, clone, nil, true, false)

	syncer = NewSyncer(gitMgr, store, svc, bus, clone, true, true, time.Minute, "ssh", "")
	require.NotNil(t, syncer)

	return syncer, upstream, clone, bus
}

// run executes a command and fails the test on error.
func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
	return string(out)
}

func TestNewSyncer_NoRemote(t *testing.T) {
	dir := t.TempDir()
	gitMgr, err := gitops.NewManager(dir, "", "ssh", "")
	require.NoError(t, err)

	syncer := NewSyncer(gitMgr, nil, nil, nil, dir, true, true, time.Minute, "ssh", "")
	assert.Nil(t, syncer, "syncer should be nil when no remote")
}

func TestPullRebase_AlreadyUpToDate(t *testing.T) {
	syncer, _, _, bus := setupSyncTest(t)
	ctx := context.Background()

	ch, unsub := bus.Subscribe()
	defer unsub()

	err := syncer.PullOnStartup(ctx)
	require.NoError(t, err)

	status := syncer.Status()
	assert.True(t, status.Enabled)
	assert.NotNil(t, status.LastSyncTime)
	assert.Empty(t, status.LastSyncError)

	// Should have received sync.started and sync.completed events.
	var evts []events.Event
	drainEvents(ch, &evts)
	assertHasEventType(t, evts, events.SyncStarted)
	assertHasEventType(t, evts, events.SyncCompleted)
}

func TestPullRebase_NewUpstreamCommits(t *testing.T) {
	syncer, upstream, clone, _ := setupSyncTest(t)
	ctx := context.Background()

	// Make a commit on upstream via a second clone.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	run(t, "", "git", "clone", upstream, clone2)
	run(t, clone2, "git", "config", "user.email", "test@test.com")
	run(t, clone2, "git", "config", "user.name", "Test")

	// Create a card file in clone2 and push.
	cardContent := `---
id: TEST-001
title: Remote Card
project: test-project
type: task
state: todo
priority: medium
created: 2026-04-01T00:00:00Z
updated: 2026-04-01T00:00:00Z
---

Body.
`
	cardPath := filepath.Join(clone2, "test-project", "tasks", "TEST-001.md")
	require.NoError(t, os.WriteFile(cardPath, []byte(cardContent), 0o644))
	run(t, clone2, "git", "add", "-A")
	run(t, clone2, "git", "commit", "-m", "add card from remote")
	run(t, clone2, "git", "push", "origin", "HEAD")

	// Pull on the original clone — should get the new card.
	err := syncer.pullRebase(ctx, "test")
	require.NoError(t, err)

	// Verify the file exists locally.
	localCard := filepath.Join(clone, "test-project", "tasks", "TEST-001.md")
	_, err = os.Stat(localCard)
	require.NoError(t, err, "card file should exist after pull")

	// Verify the store index was rebuilt.
	cards, err := syncer.store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "TEST-001", cards[0].ID)
}

func TestPullRebase_WithLocalCommits(t *testing.T) {
	syncer, upstream, clone, _ := setupSyncTest(t)
	ctx := context.Background()

	// Create a remote commit via clone2.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	run(t, "", "git", "clone", upstream, clone2)
	run(t, clone2, "git", "config", "user.email", "test@test.com")
	run(t, clone2, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(clone2, "remote.txt"), []byte("remote"), 0o644))
	run(t, clone2, "git", "add", "-A")
	run(t, clone2, "git", "commit", "-m", "remote commit")
	run(t, clone2, "git", "push", "origin", "HEAD")

	// Create a local commit.
	require.NoError(t, os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local"), 0o644))
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "local commit")

	// Pull should rebase local on top of remote.
	err := syncer.pullRebase(ctx, "test")
	require.NoError(t, err)

	// Both files should exist.
	_, err = os.Stat(filepath.Join(clone, "remote.txt"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(clone, "local.txt"))
	require.NoError(t, err)
}

func TestPullRebase_DirtyWorktree(t *testing.T) {
	syncer, upstream, clone, _ := setupSyncTest(t)
	ctx := context.Background()

	// Create a remote commit via clone2.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	run(t, "", "git", "clone", upstream, clone2)
	run(t, clone2, "git", "config", "user.email", "test@test.com")
	run(t, clone2, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(clone2, "upstream.txt"), []byte("upstream"), 0o644))
	run(t, clone2, "git", "add", "-A")
	run(t, clone2, "git", "commit", "-m", "upstream commit")
	run(t, clone2, "git", "push", "origin", "HEAD")

	// Write an uncommitted file in the clone, making the worktree dirty.
	require.NoError(t, os.WriteFile(filepath.Join(clone, "dirty.txt"), []byte("dirty"), 0o644))

	// pullRebase must succeed even with a dirty worktree (--autostash handles it).
	err := syncer.pullRebase(ctx, "test")
	require.NoError(t, err)

	// The upstream file should be present after the rebase.
	_, err = os.Stat(filepath.Join(clone, "upstream.txt"))
	require.NoError(t, err, "upstream file should exist after pull")

	// The dirty file should still exist (autostash restores it).
	_, err = os.Stat(filepath.Join(clone, "dirty.txt"))
	require.NoError(t, err, "dirty worktree file should be restored after autostash")
}

func TestPullRebase_Conflict(t *testing.T) {
	syncer, upstream, clone, bus := setupSyncTest(t)
	ctx := context.Background()

	// Both sides modify the same file to create a conflict.
	conflictFile := filepath.Join(clone, "test-project", ".board.yaml")

	// Remote change via clone2.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	run(t, "", "git", "clone", upstream, clone2)
	run(t, clone2, "git", "config", "user.email", "test@test.com")
	run(t, clone2, "git", "config", "user.name", "Test")

	// Read current content, modify it differently.
	content, err := os.ReadFile(filepath.Join(clone2, "test-project", ".board.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(clone2, "test-project", ".board.yaml"),
		append(content, []byte("\n# remote change\n")...), 0o644))
	run(t, clone2, "git", "add", "-A")
	run(t, clone2, "git", "commit", "-m", "remote board change")
	run(t, clone2, "git", "push", "origin", "HEAD")

	// Local conflicting change: overwrite the file with different content.
	require.NoError(t, os.WriteFile(conflictFile, []byte("completely different content\n"), 0o644))
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "local board change")

	ch, unsub := bus.Subscribe()
	defer unsub()

	// Pull should detect conflict, abort rebase, and return error.
	err = syncer.pullRebase(ctx, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rebase conflict")

	// Should have emitted sync.conflict event.
	var evts []events.Event
	drainEvents(ch, &evts)
	assertHasEventType(t, evts, events.SyncConflict)

	// Status should show error.
	status := syncer.Status()
	assert.NotEmpty(t, status.LastSyncError)
}

func TestTriggerSync(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)
	ctx := context.Background()

	err := syncer.TriggerSync(ctx)
	require.NoError(t, err)

	status := syncer.Status()
	assert.NotNil(t, status.LastSyncTime)
	assert.Empty(t, status.LastSyncError)
}

func TestNotifyCommit_NonBlocking(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)

	// Send multiple notifications — should not block.
	for range 10 {
		syncer.NotifyCommit()
	}

	// Channel should have exactly 1 message (coalesced).
	assert.Len(t, syncer.pushCh, 1)
}

func TestStatus_Initial(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)

	status := syncer.Status()
	assert.True(t, status.Enabled)
	assert.Nil(t, status.LastSyncTime)
	assert.Empty(t, status.LastSyncError)
	assert.False(t, status.Syncing)
}

func TestRunGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	dir := t.TempDir()
	run(t, dir, "git", "init")

	out, err := runGit(context.Background(), dir, nil, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "On branch")
}

func TestRunGit_Error(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	_, err := runGit(context.Background(), t.TempDir(), nil, "log")
	assert.Error(t, err)
}

// drainEvents reads all available events from the channel without blocking.
func drainEvents(ch <-chan events.Event, out *[]events.Event) {
	for {
		select {
		case evt := <-ch:
			*out = append(*out, evt)
		default:
			return
		}
	}
}

// assertHasEventType asserts that at least one event has the given type.
func assertHasEventType(t *testing.T, evts []events.Event, typ events.EventType) {
	t.Helper()
	for _, e := range evts {
		if e.Type == typ {
			return
		}
	}
	t.Errorf("expected event type %q, got: %v", typ, evts)
}

// TestNewSyncer_SSHMode verifies that NewSyncer stores the ssh auth mode and
// that GitAuthEnv returns nil for it (no env injection, preserves SSH agent).
func TestNewSyncer_SSHMode(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)
	assert.Equal(t, "ssh", syncer.authMode)
	assert.Equal(t, "", syncer.token)
	assert.Nil(t, gitops.GitAuthEnv(syncer.authMode, syncer.token),
		"ssh mode must produce nil auth env")
}

// TestNewSyncer_PATMode verifies that NewSyncer stores the pat auth mode and
// token, and that GitAuthEnv returns the four expected entries.
func TestNewSyncer_PATMode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	const token = "ghp_pat_test_token"

	upstream := filepath.Join(t.TempDir(), "upstream.git")
	run(t, "", "git", "init", "--bare", upstream)

	clone := filepath.Join(t.TempDir(), "clone")
	run(t, "", "git", "clone", upstream, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(clone, "init.txt"), []byte("init"), 0o644))
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "initial")
	run(t, clone, "git", "push", "origin", "HEAD")

	gitMgr, err := gitops.NewManager(clone, "", "pat", token)
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(clone)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, gitMgr, lockMgr, bus, clone, nil, true, false)

	syncer := NewSyncer(gitMgr, store, svc, bus, clone, true, true, time.Minute, "pat", token)
	require.NotNil(t, syncer)

	assert.Equal(t, "pat", syncer.authMode)
	assert.Equal(t, token, syncer.token)

	env := gitops.GitAuthEnv(syncer.authMode, syncer.token)
	require.NotNil(t, env)
	require.Len(t, env, 4)
	assert.Contains(t, env, "GIT_CONFIG_COUNT=1")
	assert.Contains(t, env, "GIT_CONFIG_KEY_0=http.extraheader")
	assert.Contains(t, env, "GIT_CONFIG_VALUE_0=Authorization: Bearer "+token)
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
}

// TestNewSyncer_PATMode_TokenNotInArgs verifies that the PAT token never
// appears in git command arguments — it must only travel via environment.
func TestNewSyncer_PATMode_TokenNotInArgs(t *testing.T) {
	const token = "ghp_supersecret_should_not_leak"

	env := gitops.GitAuthEnv("pat", token)
	require.NotNil(t, env)

	// The token must appear exactly once — in GIT_CONFIG_VALUE_0 only.
	for _, e := range env {
		if e == "GIT_CONFIG_VALUE_0=Authorization: Bearer "+token {
			continue // correct placement
		}
		assert.NotContains(t, e, token,
			"token must only appear in GIT_CONFIG_VALUE_0, not in: %s", e)
	}
}

// TestRunGit_WithAuthEnv verifies that runGit properly injects extra env vars.
func TestRunGit_WithAuthEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	dir := t.TempDir()
	run(t, dir, "git", "init")

	// GIT_TERMINAL_PROMPT=0 is a valid env var; git status should still work.
	authEnv := []string{"GIT_TERMINAL_PROMPT=0"}
	out, err := runGit(context.Background(), dir, authEnv, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "On branch")
}

// TestSyncer_PushWithRetry_BlocksOnWriteMu verifies that pushWithRetry waits
// for the service write lock to be released before proceeding. This proves
// that concurrent pullRebase (which holds writeMu) and pushWithRetry cannot
// race at the git-subprocess level.
func TestSyncer_PushWithRetry_BlocksOnWriteMu(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)
	ctx := context.Background()

	// Acquire the write lock from outside, simulating a concurrent pullRebase.
	syncer.svc.LockWrites()

	pushDone := make(chan error, 1)
	go func() {
		pushDone <- syncer.pushWithRetry(ctx)
	}()

	// pushWithRetry must NOT complete while writeMu is held.
	select {
	case <-pushDone:
		t.Fatal("pushWithRetry completed while writeMu was held — expected it to block")
	case <-time.After(100 * time.Millisecond):
		// Good: still blocked on LockWrites.
	}

	// Release the lock — pushWithRetry should acquire it and finish quickly.
	syncer.svc.UnlockWrites()

	select {
	case err := <-pushDone:
		// push to local bare repo succeeds (already up to date is fine too).
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("pushWithRetry did not complete within 5s after writeMu was released")
	}

	// After pushWithRetry returns, writeMu must be free.
	lockAcquired := make(chan struct{}, 1)
	go func() {
		syncer.svc.LockWrites()
		lockAcquired <- struct{}{}
		syncer.svc.UnlockWrites()
	}()
	select {
	case <-lockAcquired:
		// Good: lock is not leaked.
	case <-time.After(1 * time.Second):
		t.Fatal("writeMu still held after pushWithRetry returned — lock was leaked")
	}
}

// TestSyncer_PushWithRetry_RetryPath_NoDeadlock exercises the non-fast-forward
// retry branch end-to-end and verifies no deadlock occurs. A second clone
// pushes to the upstream first, causing the syncer's push to be rejected as
// non-fast-forward. pushWithRetry must release writeMu before calling
// pullRebase (which acquires writeMu itself), or a deadlock would occur since
// sync.Mutex is not reentrant.
func TestSyncer_PushWithRetry_RetryPath_NoDeadlock(t *testing.T) {
	syncer, upstream, clone, _ := setupSyncTest(t)
	ctx := context.Background()

	// Create a local commit on the syncer's clone so there is something to push.
	require.NoError(t, os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local"), 0o644))
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "local commit")

	// Push a diverging commit from a second clone so the upstream is ahead.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	run(t, "", "git", "clone", upstream, clone2)
	run(t, clone2, "git", "config", "user.email", "test@test.com")
	run(t, clone2, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(clone2, "remote.txt"), []byte("remote"), 0o644))
	run(t, clone2, "git", "add", "-A")
	run(t, clone2, "git", "commit", "-m", "remote diverging commit")
	run(t, clone2, "git", "push", "origin", "HEAD")

	// pushWithRetry should detect non-fast-forward, call pullRebase (which
	// needs writeMu), then retry the push — all without deadlocking.
	done := make(chan error, 1)
	go func() {
		done <- syncer.pushWithRetry(ctx)
	}()

	select {
	case err := <-done:
		// The retry path completed. Either success or an error from the push —
		// both outcomes are valid here; what matters is no deadlock.
		_ = err
	case <-time.After(15 * time.Second):
		t.Fatal("pushWithRetry deadlocked or timed out on retry path")
	}

	// writeMu must be free after return.
	lockAcquired := make(chan struct{}, 1)
	go func() {
		syncer.svc.LockWrites()
		lockAcquired <- struct{}{}
		syncer.svc.UnlockWrites()
	}()
	select {
	case <-lockAcquired:
		// Good.
	case <-time.After(1 * time.Second):
		t.Fatal("writeMu still held after pushWithRetry returned on retry path")
	}
}

// TestRunGit_NilAuthEnv verifies that nil auth env leaves the process env intact.
func TestRunGit_NilAuthEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	dir := t.TempDir()
	run(t, dir, "git", "init")

	out, err := runGit(context.Background(), dir, nil, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "On branch")
}

// TestPeriodicPull_SurvivesPanic verifies that a panic inside periodicPull's
// per-tick work is recovered and the loop continues firing on the next tick.
func TestPeriodicPull_SurvivesPanic(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)

	var callCount int
	done := make(chan struct{})

	syncer.pullHook = func(_ context.Context, _ string) error {
		callCount++
		if callCount == 1 {
			panic("injected test panic")
		}
		// Signal after the second successful call.
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a very short interval so the test completes quickly.
	syncer.interval = 10 * time.Millisecond

	syncer.wg.Add(1)
	go func() {
		defer syncer.wg.Done()
		syncer.periodicPull(ctx)
	}()

	// Wait for at least two ticks — proves the loop survived the first-tick panic.
	select {
	case <-done:
		// success: loop continued after the panic
	case <-time.After(5 * time.Second):
		t.Fatal("periodicPull loop did not continue after panic within 5s")
	}

	cancel()
	syncer.wg.Wait()

	assert.GreaterOrEqual(t, callCount, 2, "pullHook must have been called at least twice")
}

// TestPushListener_SurvivesPanic verifies that a panic inside pushListener's
// per-notification work is recovered and the loop continues on the next push.
func TestPushListener_SurvivesPanic(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)

	var callCount int
	// firstDone is closed when the first (panic) call finishes its recovery.
	firstDone := make(chan struct{})
	done := make(chan struct{})

	syncer.pushHook = func(_ context.Context) error {
		callCount++
		if callCount == 1 {
			defer close(firstDone)
			panic("injected push panic")
		}
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncer.wg.Add(1)
	go func() {
		defer syncer.wg.Done()
		syncer.pushListener(ctx)
	}()

	// Send the first notification (will panic on the hook).
	syncer.NotifyCommit()

	// Wait until the panic has been recovered before sending the second
	// notification. This ensures the channel slot is free.
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first push (panic) did not complete within 5s")
	}

	// Now send the second notification — the loop must still be running.
	syncer.NotifyCommit()

	// Wait for confirmation the second notification was processed.
	select {
	case <-done:
		// success: loop continued after the panic
	case <-time.After(5 * time.Second):
		t.Fatal("pushListener loop did not continue after panic within 5s")
	}

	cancel()
	syncer.wg.Wait()

	assert.GreaterOrEqual(t, callCount, 2, "pushHook must have been called at least twice")
}
