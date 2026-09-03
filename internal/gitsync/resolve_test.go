package gitsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
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

	ca := a.createUnpushed(t, "from a")
	cb := b.createUnpushed(t, "from b")
	require.Equal(t, ca.ID, cb.ID, "both clones mint the same first id")

	// b also references its own card from a second, non-conflicting card.
	dep := b.dependentUnpushed(t, "b dep", cb.ID)

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

	ca := a.createUnpushed(t, "from a")
	cb := b.createUnpushed(t, "from b")
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

	a.createUnpushed(t, "from a")
	b.createUnpushed(t, "from b")

	// Both sides minted one card, so the merged next_id is b's own and the
	// re-mint takes the id b would hand out next.
	remintPath := "test-project/tasks/" + b.nextCardID(t) + ".md"

	a.sync(t)

	require.NoError(t, os.WriteFile(filepath.Join(b.dir, ".git", "info", "exclude"),
		[]byte(remintPath+"\n"), 0o644))

	_, err := b.syncer.Synced(context.Background(), "test", nil)

	// Naming the path proves the cycle failed on staging that file, not on
	// something unrelated that would make the assertions below vacuous.
	require.ErrorContains(t, err, "stage "+remintPath)

	assert.False(t, b.git.MergeInProgress())
	assert.NoFileExists(t, filepath.Join(b.dir, remintPath), "a re-mint that failed to stage must not survive")

	clean, dirty, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean, dirty)
}

// TestResolve_RemintedCardFollowsItsOwnRenames covers a re-minted card whose
// own references point at another card re-minted in the same merge. boardmerge
// copies our card verbatim apart from its id, so the syncer has to rewrite the
// references inside the files it wrote as well.
func TestResolve_RemintedCardFollowsItsOwnRenames(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.sync(t)
	b.sync(t)

	// Both sides build the same two-card chain over the same two ids.
	a1 := a.createUnpushed(t, "a one")
	a2 := a.dependentUnpushed(t, "a two", a1.ID)
	b1 := b.createUnpushed(t, "b one")
	b2 := b.dependentUnpushed(t, "b two", b1.ID)

	require.Equal(t, a1.ID, b1.ID)
	require.Equal(t, a2.ID, b2.ID)

	a.sync(t)
	b.sync(t)
	a.sync(t)

	for _, n := range []*sharedNode{a, b} {
		byTitle := cardsByTitle(t, n)
		require.Len(t, byTitle, 4, "every card survives on %s", n.dir)

		assert.Equal(t, []string{byTitle["a one"].ID}, byTitle["a two"].DependsOn,
			"the remote chain is untouched on %s", n.dir)
		assert.Equal(t, []string{byTitle["b one"].ID}, byTitle["b two"].DependsOn,
			"the re-minted card follows the re-mint it depends on, on %s", n.dir)

		ids := map[string]bool{}
		for _, c := range byTitle {
			ids[c.ID] = true
		}

		assert.Len(t, ids, 4, "all four ids are distinct on %s", n.dir)
	}
}

// TestResolve_PlaybookEntryFollowsRemint covers the playbook half of the
// reference rewrite: a local playbook pointing at a card that gets re-minted
// follows it, while a manual gate step carries no card and is left alone.
func TestResolve_PlaybookEntryFollowsRemint(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.sync(t)
	b.sync(t)

	a.createUnpushed(t, "from a")
	cb := b.createUnpushed(t, "from b")

	gate := board.PlaybookEntry{ID: "e2", Type: board.EntryTypeManual, Text: "ship it"}
	b.writePlaybook(t, "release", board.PlaybookEntry{
		ID: "e1", Type: board.EntryTypeCard, Project: "test-project", Card: cb.ID,
	}, gate)

	a.sync(t)

	r := b.sync(t)

	remint := findRemint(r.Resolutions)
	require.NotNil(t, remint)

	a.sync(t)

	for _, n := range []*sharedNode{a, b} {
		pb := n.readPlaybook(t, "release")
		require.Len(t, pb.Entries, 2, "on %s", n.dir)

		assert.Equal(t, remint.NewID, pb.Entries[0].Card, "the card entry follows the re-mint on %s", n.dir)
		assert.Equal(t, gate, pb.Entries[1], "the manual gate step is untouched on %s", n.dir)
	}
}

// dependent creates a card in the node's test project that depends on dependsOn.
func (n *sharedNode) dependent(t *testing.T, title, dependsOn string) *board.Card {
	t.Helper()

	c, err := n.svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: title, Type: "task", Priority: "medium", DependsOn: []string{dependsOn},
	})
	require.NoError(t, err)

	return c
}

// nextCardID returns the id the node's test project would mint next, without
// consuming it.
func (n *sharedNode) nextCardID(t *testing.T) string {
	t.Helper()

	cfg, err := n.store.GetProject(context.Background(), "test-project")
	require.NoError(t, err)

	next := *cfg

	return board.GenerateCardID(&next)
}

// writePlaybook commits a playbook with the given entries into the node's clone.
func (n *sharedNode) writePlaybook(t *testing.T, id string, entries ...board.PlaybookEntry) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	data, err := board.SerializePlaybook(&board.Playbook{
		ID: id, Title: id, Created: now, Updated: now,
		NextEntryID: len(entries) + 1, Entries: entries,
	})
	require.NoError(t, err)

	rel := filepath.Join("playbooks", id+".yaml")
	require.NoError(t, os.MkdirAll(filepath.Join(n.dir, "playbooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(n.dir, rel), data, 0o644))
	require.NoError(t, n.git.CommitFilesShell(context.Background(), []string{rel}, "add playbook "+id))
}

func (n *sharedNode) readPlaybook(t *testing.T, id string) *board.Playbook {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(n.dir, "playbooks", id+".yaml"))
	require.NoError(t, err)

	pb, err := board.ParsePlaybook(data)
	require.NoError(t, err)

	return pb
}

func cardsByTitle(t *testing.T, n *sharedNode) map[string]*board.Card {
	t.Helper()

	cards, err := n.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	byTitle := map[string]*board.Card{}

	for _, c := range cards {
		byTitle[c.Title] = c
	}

	return byTitle
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
