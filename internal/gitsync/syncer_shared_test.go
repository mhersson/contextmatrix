package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	pb     *service.PlaybookService
	runner service.SyncRunner
	git    *gitops.Manager
	bus    *events.Bus
	clk    *clock.FakeClock
	dir    string
	id     string
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
	clk := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))

	lockMgr := lock.NewManagerWithClock(store, 30*time.Minute, clk)
	lockMgr.SetShared(instance, 5*time.Minute, time.Hour)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, dir, nil, true, false)
	svc.SetSharedRepo(true)
	svc.SetLease(instance, time.Hour, time.Minute)

	queue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(queue)
	t.Cleanup(func() { _ = queue.Close(context.Background()) })

	syncer := NewSyncer(gitMgr, store, svc, bus, dir, true, true, time.Minute,
		WithShared(instance), WithSyncTimeout(5*time.Second), WithLeaseInterval(5*time.Minute))
	require.NotNil(t, syncer)

	runner := func(ctx context.Context, trigger string, m service.SyncMutation) (service.SyncOutcome, error) {
		r, err := syncer.SyncedMutation(ctx, trigger, m)

		return service.SyncOutcome{BodyRan: r.BodyRan, Pushed: r.Pushed, Resolutions: r.Resolutions}, err
	}

	svc.SetSyncRunner(runner)

	pbStore, err := storage.NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	pb := service.NewPlaybookService(pbStore, store, bus, clk, true)
	pb.SetCommitQueue(queue)
	pb.SetSyncRunner(runner, service.DirectCommitter(gitMgr))
	syncer.SetPlaybooks(pb)

	return &sharedNode{
		syncer: syncer, store: store, svc: svc, pb: pb, runner: runner,
		git: gitMgr, bus: bus, clk: clk, dir: dir, id: instance,
	}
}

func (n *sharedNode) claim(t *testing.T, id, agent string) *board.Card {
	t.Helper()

	c, err := n.svc.ClaimCard(context.Background(), "test-project", id, agent)
	require.NoError(t, err)

	return c
}

func (n *sharedNode) heartbeat(t *testing.T, id, agent string) {
	t.Helper()

	_, err := n.svc.HeartbeatCard(context.Background(), "test-project", id, agent)
	require.NoError(t, err)
}

func (n *sharedNode) sweep(t *testing.T) {
	t.Helper()

	require.NoError(t, n.svc.SweepStalled(context.Background()))
}

func (n *sharedNode) card(t *testing.T, id string) *board.Card {
	t.Helper()

	c, err := n.store.GetCard(context.Background(), "test-project", id)
	require.NoError(t, err)

	return c
}

func (n *sharedNode) lastCommit(t *testing.T) string {
	t.Helper()

	return strings.TrimSpace(run(t, n.dir, "git", "log", "--format=%s", "-1"))
}

func boardEntry(agent string) board.ActivityEntry {
	return board.ActivityEntry{Agent: agent, Action: "note", Message: "x"}
}

// writeClaim marks a card as held by agent through instance via, at the given
// epoch, and commits it, as a peer's pushed claim would look after a pull.
func (n *sharedNode) writeClaim(t *testing.T, id, agent, via string, epoch int) {
	t.Helper()

	ctx := context.Background()

	c, err := n.store.GetCard(ctx, "test-project", id)
	require.NoError(t, err)

	now := n.clk.Now()
	c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.LastHeartbeat, c.ClaimEpoch = agent, via, &now, &now, epoch
	c.State = "in_progress"
	require.NoError(t, n.store.UpdateCard(ctx, "test-project", c))
	require.NoError(t, n.git.CommitFilesShell(ctx, []string{"test-project/tasks/" + id + ".md"}, "claim by "+via))
}

func (n *sharedNode) writeStall(t *testing.T, id string, epoch int) {
	t.Helper()

	ctx := context.Background()

	c, err := n.store.GetCard(ctx, "test-project", id)
	require.NoError(t, err)

	c.ClearClaim()
	c.State, c.ClaimEpoch = "stalled", epoch
	require.NoError(t, n.store.UpdateCard(ctx, "test-project", c))
	require.NoError(t, n.git.CommitFilesShell(ctx, []string{"test-project/tasks/" + id + ".md"}, "stall"))
}

func (n *sharedNode) create(t *testing.T, title string) *board.Card {
	t.Helper()

	c, err := n.svc.CreateCard(context.Background(), "test-project",
		service.CreateCardInput{Title: title, Type: "task", Priority: "medium"})
	require.NoError(t, err)

	return c
}

// unpushed turns push verification off for the duration of fn, so the cards
// it creates stay on a local commit the remote has never seen. That is the
// state two clones are in when both mint before either pushes, and it is the
// only way to build an add/add id collision: a verified create pushes before
// it returns, so the second clone would already hold the first clone's card.
func (n *sharedNode) unpushed(fn func()) {
	n.svc.SetSyncRunner(nil)
	defer n.svc.SetSyncRunner(n.runner)

	fn()
}

func (n *sharedNode) createUnpushed(t *testing.T, title string) *board.Card {
	t.Helper()

	var c *board.Card

	n.unpushed(func() { c = n.create(t, title) })

	return c
}

func (n *sharedNode) dependentUnpushed(t *testing.T, title, dependsOn string) *board.Card {
	t.Helper()

	var c *board.Card

	n.unpushed(func() { c = n.dependent(t, title, dependsOn) })

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

// TestSynced_PushesWhenTheBranchIsNotOnTheRemoteYet covers first-time setup:
// the upstream repository is empty, so origin/<branch> does not exist and an
// ahead count has no revision to count against. The cycle must push anyway,
// or the first instance of a shared board never reaches the remote and no
// colleague can clone it.
func TestSynced_PushesWhenTheBranchIsNotOnTheRemoteYet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	ctx := context.Background()
	upstream := filepath.Join(t.TempDir(), "upstream.git")

	run(t, "", "git", "init", "--bare", "-b", "main", upstream)

	a := newSharedNode(t, upstream, "lap-a")

	// Nothing was ever pushed, so this instance seeds the board itself.
	require.NoError(t, os.MkdirAll(filepath.Join(a.dir, "test-project", "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(a.dir, "test-project"), testProjectConfig()))
	require.NoError(t, a.store.ReloadIndex(ctx))

	// The create runs its own cycle, and that is the one facing a remote
	// without the branch: it has to push the whole local history anyway.
	card := a.create(t, "first")
	assert.Equal(t, 0, a.syncer.Status().UnpushedCommits, "the create pushed the branch the remote lacked")

	_, err := a.syncer.Synced(ctx, "test", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, a.syncer.Status().UnpushedCommits)

	// The upstream now carries the branch, so a colleague can clone it.
	tracked := run(t, upstream, "git", "ls-tree", "-r", "--name-only", "main")
	assert.Contains(t, tracked, "test-project/tasks/"+card.ID+".md")

	b := newSharedNode(t, upstream, "lap-b")
	b.sync(t)

	got, err := b.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "first", got.Title)
}

// TestSynced_RecordsResolutionsWhenThePushFails pins the status log against
// the failure this topology exists for: the merge resolved and committed, and
// then the push did not land. Those resolutions describe commits that are on
// HEAD either way, and a developer who has just lost a push is exactly the one
// who goes looking for what the resolver decided.
func TestSynced_RecordsResolutionsWhenThePushFails(t *testing.T) {
	ctx := context.Background()
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	// Both sides edit the same card, so b has to resolve to integrate.
	_, err := a.svc.UpdateCard(ctx, "test-project", c.ID,
		service.UpdateCardInput{Title: "from a", Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	a.sync(t)

	_, err = b.svc.UpdateCard(ctx, "test-project", c.ID,
		service.UpdateCardInput{Title: "from b", Type: "task", State: "todo", Priority: "low"})
	require.NoError(t, err)

	// One attempt, and the remote moves before it: b merges, resolves and
	// commits, then loses the only push it gets.
	b.syncer.maxAttempts = 1
	b.syncer.retryBackoff = time.Millisecond
	b.syncer.prePushHook = func(int) {
		a.create(t, "a2") // the verified create pushes on its own
		require.Equal(t, 0, a.syncer.Status().UnpushedCommits, "the remote must have moved")
	}

	report, err := b.syncer.Synced(ctx, "test", nil)
	require.ErrorIs(t, err, ErrSyncContended)
	require.NotEmpty(t, report.Resolutions, "the merge must have resolved something to record")

	recorded := b.syncer.Status().Resolutions
	require.Len(t, recorded, len(report.Resolutions))

	for i, want := range report.Resolutions {
		assert.Equal(t, want, recorded[i].Resolution)
		assert.Equal(t, "test", recorded[i].Trigger)
	}
}

func TestSynced_PushesAndPullsWithoutConflict(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "from a") // the verified create pushes on its own

	a.sync(t)
	assert.Equal(t, 0, a.syncer.Status().UnpushedCommits)

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
			a.create(t, "a2") // the verified create pushes on its own
			require.Equal(t, 0, a.syncer.Status().UnpushedCommits, "the remote must have moved")
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
		a.create(t, fmt.Sprintf("a%d", attempts+1)) // the verified create pushes on its own
		require.Equal(t, 0, a.syncer.Status().UnpushedCommits, "the remote must have moved")
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
	fromB := b.create(t, "from b") // the verified create pushes on its own
	require.Equal(t, 0, b.syncer.Status().UnpushedCommits, "the remote must have moved")

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

func TestSyncedMutation_UndoRunsWhenThePushNeverLands(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "seed")
	a.sync(t)
	b.sync(t)

	rel := filepath.Join("test-project", "note.md")
	undone := false

	b.syncer.maxAttempts = 1
	b.syncer.retryBackoff = time.Millisecond
	b.syncer.prePushHook = func(int) {
		a.create(t, "a moved first") // the verified create pushes on its own
		require.Equal(t, 0, a.syncer.Status().UnpushedCommits, "the remote must have moved")
	}

	report, err := b.syncer.SyncedMutation(context.Background(), "test", service.SyncMutation{
		Apply: func(ctx context.Context) error {
			if err := os.WriteFile(filepath.Join(b.dir, rel), []byte("applied\n"), 0o644); err != nil {
				return err
			}

			return b.git.CommitFilesShell(ctx, []string{rel}, "apply")
		},
		Undo: func(ctx context.Context) error {
			undone = true

			if err := os.Remove(filepath.Join(b.dir, rel)); err != nil {
				return err
			}

			return b.git.CommitFilesShell(ctx, []string{rel}, "undo")
		},
	})
	require.ErrorIs(t, err, ErrSyncContended)
	assert.True(t, report.BodyRan)
	assert.True(t, undone)

	_, statErr := os.Stat(filepath.Join(b.dir, rel))
	assert.True(t, os.IsNotExist(statErr), "the applied file is gone")

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)
	assert.False(t, b.git.MergeInProgress())
}

func TestSyncedMutation_ApplyErrorSkipsUndo(t *testing.T) {
	a, _, _ := setupSharedPair(t)

	errApply := errors.New("apply refused")
	undone := false

	report, err := a.syncer.SyncedMutation(context.Background(), "test", service.SyncMutation{
		Apply: func(context.Context) error { return errApply },
		Undo: func(context.Context) error {
			undone = true

			return nil
		},
	})
	require.ErrorIs(t, err, errApply)
	assert.True(t, report.BodyRan)
	assert.False(t, undone, "a mutation that fails leaves nothing to undo")
}

// TestSynced_ConfirmsOwnLeases covers the fence after a restart: a claim this
// instance holds is unconfirmed until a cycle succeeds.
func TestSynced_ConfirmsOwnLeases(t *testing.T) {
	a, _, _ := setupSharedPair(t)
	ctx := context.Background()

	c := a.create(t, "x")
	a.writeClaim(t, c.ID, "agent-x", a.id, 1)

	_, err := a.svc.ReleaseCard(ctx, "test-project", c.ID, "agent-x")
	require.ErrorIs(t, err, service.ErrClaimFenced)

	a.sync(t)

	_, err = a.svc.ReleaseCard(ctx, "test-project", c.ID, "agent-x")
	require.NoError(t, err)
}

func TestSynced_ReportsClaimLostAfterAPeerStalledTheCard(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	a.writeClaim(t, c.ID, "agent-x", a.id, 1)
	a.sync(t)
	b.sync(t)

	b.writeStall(t, c.ID, 2)
	require.True(t, b.sync(t).Pushed)

	ch, unsubscribe := a.bus.Subscribe()
	defer unsubscribe()

	a.sync(t)

	var got []events.Event

	drainEvents(ch, &got)
	assertHasEventType(t, got, events.ClaimLost)

	for _, e := range got {
		if e.Type == events.ClaimLost {
			assert.Equal(t, c.ID, e.CardID)
			assert.Equal(t, "agent-x", e.Data["previous_agent"])
			assert.Equal(t, 2, e.Data["claim_epoch"])
		}
	}

	_, err := a.svc.HeartbeatCard(context.Background(), "test-project", c.ID, "agent-x")
	require.Error(t, err, "the local agent has lost the card")
}

func TestSynced_ClaimsAtRiskOncePushesFailPastTheLeaseInterval(t *testing.T) {
	a, _, upstream := setupSharedPair(t)
	a.syncer.leaseInterval = 0 // the first failure already puts claims at risk

	require.NoError(t, os.RemoveAll(upstream))

	_, err := a.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	st := a.syncer.Status()
	require.NotNil(t, st.PushFailingSince)
	assert.True(t, st.ClaimsAtRisk)
}
