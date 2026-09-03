package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedNode is one ContextMatrix instance: its own clone of the upstream
// board repository plus the store, service and shared syncer wired on top.
type sharedNode struct {
	syncer *Syncer
	store  *storage.FilesystemStore
	svc    *service.CardService
	git    *gitops.Manager
	bus    *events.Bus
	dir    string
}

// setupSharedPair builds one bare upstream and two independent clones, each
// with its own store, service and shared syncer. Cards created through
// svcA are visible to svcB only after both have run Synced.
func setupSharedPair(t *testing.T) (a, b *sharedNode, upstream string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	upstream = filepath.Join(t.TempDir(), "upstream.git")
	run(t, "", "git", "init", "--bare", "-b", "main", upstream)

	seed := filepath.Join(t.TempDir(), "seed")
	run(t, "", "git", "clone", upstream, seed)
	run(t, seed, "git", "config", "user.email", "t@t")
	run(t, seed, "git", "config", "user.name", "t")

	require.NoError(t, os.MkdirAll(filepath.Join(seed, "test-project", "tasks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(seed, "test-project", "tasks", ".gitkeep"), nil, 0o644))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(seed, "test-project"), testProjectConfig()))

	run(t, seed, "git", "add", "-A")
	run(t, seed, "git", "commit", "-m", "initial")
	run(t, seed, "git", "push", "origin", "HEAD:main")

	return newSharedNode(t, upstream, "lap-a"), newSharedNode(t, upstream, "lap-b"), upstream
}

func testProjectConfig() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

func newSharedNode(t *testing.T, upstream, instance string) *sharedNode {
	t.Helper()

	dir := filepath.Join(t.TempDir(), instance)
	run(t, "", "git", "clone", upstream, dir)

	gitMgr, err := gitops.NewManager(dir, "", instance, gitopsTestProvider(t))
	require.NoError(t, err)
	gitMgr.SetAuthor("ContextMatrix", "contextmatrix@"+instance)

	store, err := storage.NewFilesystemStore(dir)
	require.NoError(t, err)

	bus := events.NewBus()
	svc := service.NewCardService(store, gitMgr, lock.NewManager(store, 30*time.Minute), bus, dir, nil, true, false)
	svc.SetSharedRepo(true)

	queue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(queue)
	t.Cleanup(func() { _ = queue.Close(context.Background()) })

	syncer := NewSyncer(gitMgr, store, svc, bus, dir, true, true, time.Minute,
		WithShared(instance), WithSyncTimeout(5*time.Second))
	require.NotNil(t, syncer)

	return &sharedNode{syncer: syncer, store: store, svc: svc, git: gitMgr, bus: bus, dir: dir}
}

func (n *sharedNode) create(t *testing.T, title string) *board.Card {
	t.Helper()

	c, err := n.svc.CreateCard(context.Background(), "test-project",
		service.CreateCardInput{Title: title, Type: "task", Priority: "medium"})
	require.NoError(t, err)

	return c
}

// diverge gives the node a local commit the remote does not have, by editing
// its own copy of the project's first card. Returns that card's ID.
func (n *sharedNode) diverge(t *testing.T, priority string) string {
	t.Helper()

	cards, err := n.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, cards)

	_, err = n.svc.UpdateCard(context.Background(), "test-project", cards[0].ID,
		service.UpdateCardInput{Title: cards[0].Title, Type: "task", State: "todo", Priority: priority})
	require.NoError(t, err)

	return cards[0].ID
}

// runExpectFail runs a command that is expected to exit non-zero, e.g. a
// conflicting git merge, and returns its combined output.
func runExpectFail(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	require.Error(t, err, "expected %s %v to fail, output: %s", name, args, string(out))

	return string(out)
}

func (n *sharedNode) sync(t *testing.T) SyncReport {
	t.Helper()

	r, err := n.syncer.Synced(context.Background(), "test", nil)
	require.NoError(t, err)

	return r
}

func TestSynced_PushesAndPullsWithoutConflict(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "from a")

	ra := a.sync(t)
	assert.True(t, ra.Pushed)

	rb := b.sync(t)
	assert.True(t, rb.ChangesPulled)

	cards, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, "from a", cards[0].Title)

	st := b.syncer.Status()
	assert.True(t, st.Shared)
	require.NotNil(t, st.RemoteReachable)
	assert.True(t, *st.RemoteReachable)
	assert.Equal(t, 0, st.UnpushedCommits)
}

func TestSynced_DirtyTreeIsCommittedNotStashed(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	// Hand edit on a, left uncommitted.
	p := filepath.Join(a.dir, "test-project", "tasks", c.ID+".md")

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(data, []byte("hand edit\n")...), 0o644))

	a.sync(t)

	out := run(t, a.dir, "git", "log", "--oneline", "-1")
	assert.Contains(t, out, "external edit")

	clean, _, err := a.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)

	b.sync(t)

	got, err := b.store.GetCard(context.Background(), "test-project", c.ID)
	require.NoError(t, err)
	assert.Contains(t, got.Body, "hand edit")
}

// TestSynced_DivergentBranchMergesThenPushes covers the clean-merge path: b
// diverges from the remote on a different file, so the merge needs no
// resolution and the push that follows it is an ordinary fast-forward. The
// rejected-push path is covered by TestSynced_PushRejectedThenRetriesAndPushes.
func TestSynced_DivergentBranchMergesThenPushes(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "a1")
	a.sync(t)
	b.sync(t)

	a.create(t, "a2") // a is ahead of the remote after this push
	a.sync(t)

	// b edits a different file, its own copy of the first card, while behind.
	first, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, first, 1)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", first[0].ID,
		service.UpdateCardInput{Title: first[0].Title, Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	r := b.sync(t)
	assert.True(t, r.Pushed)
	assert.True(t, r.ChangesPulled)
	assert.Empty(t, r.Resolutions)

	a.sync(t)

	ca, err := a.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	cb, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	assert.Len(t, ca, 2)
	assert.Len(t, cb, 2)

	edited, err := a.store.GetCard(context.Background(), "test-project", first[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "high", edited.Priority)
}

func TestSynced_RemoteUnreachable(t *testing.T) {
	a, _, upstream := setupSharedPair(t)

	require.NoError(t, os.RemoveAll(upstream))

	_, err := a.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	st := a.syncer.Status()
	require.NotNil(t, st.RemoteReachable)
	assert.False(t, *st.RemoteReachable)
	assert.NotEmpty(t, st.LastRemoteError)
}

func TestSynced_ConflictAbortsCleanlyWithoutResolver(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	_, err := a.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "A", Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	a.sync(t)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "B", Type: "task", State: "todo", Priority: "low"})
	require.NoError(t, err)

	// The sentinel proves the merge really conflicted and reached the hook,
	// rather than the cycle failing somewhere earlier.
	errNoResolver := errors.New("no resolver yet")
	b.syncer.resolveHook = func(context.Context, string, []string) ([]boardmerge.Resolution, error) {
		return nil, errNoResolver
	}

	_, err = b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, errNoResolver)
	assert.False(t, b.git.MergeInProgress())

	clean, _, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)
}

func TestSynced_RequiresSharedSyncer(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)

	_, err := syncer.Synced(context.Background(), "test", nil)
	require.Error(t, err)
}

// TestSynced_AbortsMergeLeftByAnEarlierCycle covers the crash window between
// Merge and CommitMerge. Without the guard, git status reports the unmerged
// paths as ordinary dirty files, commitLeftovers stages the conflict markers,
// and because MERGE_HEAD is present the commit concludes the merge and pushes
// marker-laden files to every peer.
func TestSynced_AbortsMergeLeftByAnEarlierCycle(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	_, err := a.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "A", Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	a.sync(t)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "B", Type: "task", State: "todo", Priority: "low"})
	require.NoError(t, err)

	// Leave a conflicted merge on disk by hand.
	run(t, b.dir, "git", "fetch", "origin")
	runExpectFail(t, b.dir, "git", "-c", "user.name=t", "-c", "user.email=t@t",
		"merge", "--no-edit", "--no-ff", "origin/main")
	require.True(t, b.git.MergeInProgress())

	cardPath := filepath.Join(b.dir, "test-project", "tasks", c.ID+".md")

	conflicted, err := os.ReadFile(cardPath)
	require.NoError(t, err)
	require.Contains(t, string(conflicted), "<<<<<<<", "the hand merge must leave conflict markers")

	errNoResolver := errors.New("no resolver yet")
	b.syncer.resolveHook = func(context.Context, string, []string) ([]boardmerge.Resolution, error) {
		return nil, errNoResolver
	}

	// The cycle clears the stale merge, then conflicts on its own merge and
	// aborts that too, so it fails on the resolver rather than on the wreckage.
	_, err = b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, errNoResolver)

	assert.False(t, b.git.MergeInProgress())

	clean, _, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)

	log := run(t, b.dir, "git", "log", "--oneline", "-10")
	assert.NotContains(t, log, "external edit", "the conflict markers must not be committed")

	restored, err := os.ReadFile(cardPath)
	require.NoError(t, err)
	assert.NotContains(t, string(restored), "<<<<<<<")
	assert.Contains(t, string(restored), "title: B", "the abort restores this instance's own commit")
}

func TestSynced_PushRejectedThenRetriesAndPushes(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "a1")
	a.sync(t)
	b.sync(t)

	cardID := b.diverge(t, "high")

	attempts := 0
	bodyRuns := 0

	b.syncer.retryBackoff = 10 * time.Millisecond
	b.syncer.prePushHook = func(attempt int) {
		attempts++

		// Advance the remote once, so b's first push is rejected as a
		// non-fast-forward and the cycle has to re-integrate.
		if attempt == 0 {
			a.create(t, "a2")
			require.True(t, a.sync(t).Pushed)
		}
	}

	r, err := b.syncer.Synced(context.Background(), "test", func(context.Context) error {
		bodyRuns++

		return nil
	})
	require.NoError(t, err)

	assert.True(t, r.Pushed)
	assert.True(t, r.ChangesPulled, "the rejected push forces a re-integration")
	assert.Equal(t, 2, attempts, "the first push is rejected, the second succeeds")
	assert.Equal(t, 1, bodyRuns, "the body runs once per cycle, not once per attempt")

	// Both sides survive the merge the retry performed.
	cards, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)

	edited, err := b.store.GetCard(context.Background(), "test-project", cardID)
	require.NoError(t, err)
	assert.Equal(t, "high", edited.Priority)
}

func TestSynced_ContendedRemoteExhaustsRetries(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "a1")
	a.sync(t)
	b.sync(t)

	b.diverge(t, "high")

	attempts := 0

	b.syncer.retryBackoff = 10 * time.Millisecond
	b.syncer.prePushHook = func(int) {
		attempts++

		// Every attempt loses the race: the remote always moves first.
		a.create(t, fmt.Sprintf("a%d", attempts+1))
		require.True(t, a.sync(t).Pushed)
	}

	_, err := b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, ErrSyncContended)
	assert.Equal(t, defaultMaxAttempts, attempts)

	assert.False(t, b.git.MergeInProgress())

	clean, _, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)
}

func TestSynced_BodyRunsAfterIntegrationAndIsPushed(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "seed")
	a.sync(t)
	b.sync(t)

	// b advances the remote, so a's cycle has to integrate before the body runs.
	fromB := b.create(t, "from b")
	require.True(t, b.sync(t).Pushed)

	rel := filepath.Join("test-project", "notes.md")
	sawPulledCard := false

	r, err := a.syncer.Synced(context.Background(), "test", func(ctx context.Context) error {
		_, statErr := os.Stat(filepath.Join(a.dir, "test-project", "tasks", fromB.ID+".md"))
		sawPulledCard = statErr == nil

		if err := os.WriteFile(filepath.Join(a.dir, rel), []byte("written by the body\n"), 0o644); err != nil {
			return err
		}

		return a.git.CommitFilesShell(ctx, []string{rel}, "body commit")
	})
	require.NoError(t, err)

	assert.True(t, sawPulledCard, "the body must see what the integration pulled")
	assert.True(t, r.ChangesPulled)
	assert.True(t, r.Pushed)

	b.sync(t)

	got, err := os.ReadFile(filepath.Join(b.dir, rel))
	require.NoError(t, err)
	assert.Equal(t, "written by the body\n", string(got))
}

func TestSynced_BodyErrorFailsCleanly(t *testing.T) {
	a, _, _ := setupSharedPair(t)

	errBody := errors.New("body failed")

	_, err := a.syncer.Synced(context.Background(), "test", func(context.Context) error {
		return errBody
	})
	require.ErrorIs(t, err, errBody)

	assert.False(t, a.git.MergeInProgress())

	clean, _, err := a.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)
}

// TestSynced_PublishesCardEventsAfterPull covers the in-place board update: a
// small pull publishes one per-card event per changed card alongside the
// sync.completed the UI uses to refetch after a large one.
func TestSynced_PublishesCardEventsAfterPull(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	ch, unsubscribe := b.bus.Subscribe()
	defer unsubscribe()

	a.create(t, "x")
	a.sync(t)
	b.sync(t)

	var got []events.Event

	drainEvents(ch, &got)

	assertHasEventType(t, got, events.CardCreated)
	assertHasEventType(t, got, events.SyncCompleted)

	for _, e := range got {
		if e.Type == events.CardCreated {
			assert.Equal(t, "sync", e.Data["source"])
			assert.Equal(t, "test-project", e.Project)
		}
	}
}

// TestSharedPeriodicTick_RunsSyncedOnJitteredSchedule drives the shared
// periodic loop off a fake clock: one advance past the longest jittered wait
// (1.25 * interval) must run a full shared cycle.
func TestSharedPeriodicTick_RunsSyncedOnJitteredSchedule(t *testing.T) {
	a, _, _ := setupSharedPair(t)

	fake := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	a.syncer.SetClock(fake)
	a.syncer.interval = time.Minute

	ch, unsubscribe := a.bus.Subscribe()
	defer unsubscribe()

	ctx, cancel := context.WithCancel(context.Background())

	t.Cleanup(func() {
		cancel()
		a.syncer.Wait()
	})

	a.syncer.Start(ctx)

	// The loop registers its jittered timer with the fake clock before it
	// blocks; advancing earlier would fire nothing.
	require.Eventually(t, func() bool {
		return fake.PendingTimers() > 0
	}, 2*time.Second, time.Millisecond, "periodic loop never registered a timer")

	fake.Advance(time.Duration(1.25 * float64(a.syncer.interval)))

	deadline := time.After(10 * time.Second)

	for {
		select {
		case e := <-ch:
			if e.Type == events.SyncCompleted {
				assert.Equal(t, "periodic", e.Data["trigger"])

				return
			}
		case <-deadline:
			t.Fatal("no sync.completed event after the jittered periodic tick")
		}
	}
}

// TestSharedEntryPointsRunTheMergeCycle covers the startup and manual entry
// points. Both must route through the shared cycle, which commits a dirty tree
// as an external edit and pushes it; the rebase path autostashes it instead and
// leaves nothing for a peer to pull.
func TestSharedEntryPointsRunTheMergeCycle(t *testing.T) {
	cases := []struct {
		name string
		call func(context.Context, *Syncer) error
	}{
		{"startup", func(ctx context.Context, s *Syncer) error { return s.PullOnStartup(ctx) }},
		{"manual", func(ctx context.Context, s *Syncer) error { return s.TriggerSync(ctx) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b, _ := setupSharedPair(t)

			c := a.create(t, "x")
			a.sync(t)
			b.sync(t)

			p := filepath.Join(a.dir, "test-project", "tasks", c.ID+".md")

			data, err := os.ReadFile(p)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(p, append(data, []byte("hand edit\n")...), 0o644))

			require.NoError(t, tc.call(context.Background(), a.syncer))

			assert.Contains(t, run(t, a.dir, "git", "log", "--oneline", "-1"), "external edit")

			clean, _, err := a.git.IsClean(context.Background())
			require.NoError(t, err)
			assert.True(t, clean)

			b.sync(t)

			got, err := b.store.GetCard(context.Background(), "test-project", c.ID)
			require.NoError(t, err)
			assert.Contains(t, got.Body, "hand edit", "the cycle must have pushed the leftover commit")
		})
	}
}
