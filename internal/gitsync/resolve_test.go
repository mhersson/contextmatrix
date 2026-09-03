package gitsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_ConcurrentEditsConverge(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	_, err := a.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "A", Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	a.sync(t)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "x", Type: "task", State: "todo", Priority: "medium", Labels: []string{"l"}})
	require.NoError(t, err)

	r := b.sync(t)
	assert.NotEmpty(t, r.Resolutions)

	a.sync(t)

	ga, err := a.store.GetCard(context.Background(), "test-project", c.ID)
	require.NoError(t, err)

	gb, err := b.store.GetCard(context.Background(), "test-project", c.ID)
	require.NoError(t, err)

	assert.Equal(t, "A", ga.Title)
	assert.Equal(t, "high", ga.Priority)
	assert.Equal(t, []string{"l"}, ga.Labels)
	assert.Equal(t, ga.Title, gb.Title)
	assert.Equal(t, ga.Labels, gb.Labels)
	assert.False(t, a.git.MergeInProgress())
	assert.False(t, b.git.MergeInProgress())
}

func TestResolve_DuplicateIDsRemintedAndRefsRewritten(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.sync(t)
	b.sync(t)

	ca := a.create(t, "from a")
	cb := b.create(t, "from b")
	require.Equal(t, ca.ID, cb.ID, "both clones mint the same first id")

	// b also references its own card from a second, non-conflicting card.
	dep, err := b.svc.CreateCard(context.Background(), "test-project",
		service.CreateCardInput{Title: "b dep", Type: "task", Priority: "low", DependsOn: []string{cb.ID}})
	require.NoError(t, err)

	a.sync(t)

	r := b.sync(t)

	remint := findRemint(r.Resolutions)
	require.NotNil(t, remint, "expected an add/add re-mint")

	a.sync(t)

	for _, n := range []*sharedNode{a, b} {
		cards, err := n.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
		require.NoError(t, err)

		titles := map[string]string{}
		for _, c := range cards {
			titles[c.Title] = c.ID
		}

		assert.Equal(t, ca.ID, titles["from a"], "remote keeps the id")
		assert.Equal(t, remint.NewID, titles["from b"], "local re-minted")

		got, err := n.store.GetCard(context.Background(), "test-project", dep.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{remint.NewID}, got.DependsOn, "local reference rewritten on %s", n.dir)

		cfg, err := n.store.GetProject(context.Background(), "test-project")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, cfg.NextID, 4)
	}
}

func TestResolve_DeleteWinsOverEdit(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	require.NoError(t, a.svc.DeleteCard(context.Background(), "test-project", c.ID))
	a.sync(t)

	_, err := b.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "edited", Type: "task", State: "todo", Priority: "medium"})
	require.NoError(t, err)

	b.sync(t)

	_, err = b.store.GetCard(context.Background(), "test-project", c.ID)
	require.Error(t, err)

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)
}

func TestResolve_FailureLeavesCleanTree(t *testing.T) {
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

	// Resolve for real, then fail: the abort must undo the staged resolution.
	b.syncer.resolveHook = func(ctx context.Context, branch string, ours []string) ([]boardmerge.Resolution, error) {
		if _, err := b.syncer.resolveConflicts(ctx, branch, ours); err != nil {
			return nil, err
		}

		return nil, errors.New("injected failure")
	}

	_, err = b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorContains(t, err, "injected failure")
	assert.False(t, b.git.MergeInProgress())

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)

	// With the hook cleared the next cycle converges.
	b.syncer.resolveHook = nil

	r := b.sync(t)
	assert.NotEmpty(t, r.Resolutions)
}

// TestResolve_FailureAfterRemintRemovesTheExtraFile covers the failure timing
// the resolver's own cleanup cannot see: resolveConflicts succeeded and wrote a
// re-minted card, and the cycle failed afterwards. Left behind, that file is
// committed by the next cycle as an external edit and the merge after it
// re-mints the original a second time, so the card ends up duplicated.
func TestResolve_FailureAfterRemintRemovesTheExtraFile(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.sync(t)
	b.sync(t)

	ca := a.create(t, "from a")
	cb := b.create(t, "from b")
	require.Equal(t, ca.ID, cb.ID)

	a.sync(t)

	b.syncer.resolveHook = func(ctx context.Context, branch string, ours []string) ([]boardmerge.Resolution, error) {
		res, err := b.syncer.resolveConflicts(ctx, branch, ours)
		if err != nil {
			return nil, err
		}

		require.NotNil(t, findRemint(res), "the setup must produce an add/add re-mint")

		return nil, errors.New("injected failure")
	}

	_, err := b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorContains(t, err, "injected failure")
	assert.False(t, b.git.MergeInProgress())

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)
	assert.Equal(t, []string{cb.ID + ".md"}, cardFiles(t, b), "the re-minted card must not survive the abort")

	b.syncer.resolveHook = nil

	r := b.sync(t)

	remint := findRemint(r.Resolutions)
	require.NotNil(t, remint)

	log := run(t, b.dir, "git", "log", "--oneline", "-20")
	assert.NotContains(t, log, "external edit", "no leftover was committed")

	cards, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	titles := map[string]string{}
	for _, c := range cards {
		titles[c.Title] = c.ID
	}

	assert.Len(t, cards, 2, "exactly one re-minted card, not two")
	assert.Equal(t, ca.ID, titles["from a"])
	assert.Equal(t, remint.NewID, titles["from b"])
}

// TestResolve_StagingFailureRemovesTheExtraFile pins the other half of the
// cleanup: the re-minted card is written before it is staged, so a staging
// failure must not leave the file on disk. An excluded path is the cheapest
// way to make git refuse the add; what matters is that the resolver fails
// without leaving a card file the next cycle would commit.
func TestResolve_StagingFailureRemovesTheExtraFile(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.sync(t)
	b.sync(t)

	a.create(t, "from a")
	b.create(t, "from b")
	a.sync(t)

	// b's re-mint takes the next id after the merged next_id of 2.
	remintPath := filepath.Join("test-project", "tasks", "TEST-002.md")
	require.NoError(t, os.WriteFile(filepath.Join(b.dir, ".git", "info", "exclude"),
		[]byte(remintPath+"\n"), 0o644))

	_, err := b.syncer.Synced(context.Background(), "test", nil)
	require.Error(t, err)

	assert.False(t, b.git.MergeInProgress())
	assert.NoFileExists(t, filepath.Join(b.dir, remintPath), "a re-mint that failed to stage must not survive")

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)
}

func findRemint(res []boardmerge.Resolution) *boardmerge.Resolution {
	for i := range res {
		if res[i].Rule == boardmerge.RuleAddAddRemint {
			return &res[i]
		}
	}

	return nil
}

// cardFiles lists the card file names in the node's test project, sorted.
func cardFiles(t *testing.T, n *sharedNode) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(n.dir, "test-project", "tasks"))
	require.NoError(t, err)

	var names []string

	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			names = append(names, e.Name())
		}
	}

	return names
}
