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
	gitMgr, err := gitops.NewManager(clone)
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(clone)
	require.NoError(t, err)

	bus = events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, gitMgr, lockMgr, bus, clone, nil, true, false)

	syncer = NewSyncer(gitMgr, store, svc, bus, clone, true, true, time.Minute)
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
	gitMgr, err := gitops.NewManager(dir)
	require.NoError(t, err)

	syncer := NewSyncer(gitMgr, nil, nil, nil, dir, true, true, time.Minute)
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

	out, err := runGit(context.Background(), dir, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "On branch")
}

func TestRunGit_Error(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	_, err := runGit(context.Background(), t.TempDir(), "log")
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
