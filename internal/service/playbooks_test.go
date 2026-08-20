package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingCommitter records CommitFilesShell calls; other methods no-op.
type recordingCommitter struct {
	mu    sync.Mutex
	paths [][]string
	msgs  []string
	fail  error
}

func (r *recordingCommitter) CommitFile(context.Context, string, string) error    { return nil }
func (r *recordingCommitter) CommitFiles(context.Context, []string, string) error { return nil }
func (r *recordingCommitter) CommitAll(context.Context, string) error             { return nil }
func (r *recordingCommitter) ReloadRepo(context.Context) error                    { return nil }
func (r *recordingCommitter) CommitFilesShell(_ context.Context, paths []string, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail != nil {
		return r.fail
	}

	r.paths = append(r.paths, paths)
	r.msgs = append(r.msgs, msg)

	return nil
}

type playbookTestEnv struct {
	svc       *PlaybookService
	cards     storage.Store
	committer *recordingCommitter
	bus       *events.Bus
	clk       *clock.FakeClock
	pushed    *int
}

func newPlaybookTestEnv(t *testing.T) *playbookTestEnv {
	t.Helper()
	dir := t.TempDir()

	// One real project with one card to reference.
	require.NoError(t, board.SaveProjectConfig(dir+"/project-alpha", validProjectConfigForPlaybooks()))
	cards, err := storage.NewFilesystemStore(dir)
	require.NoError(t, err)

	card := &board.Card{
		ID: "ALPHA-001", Title: "First card", Project: "project-alpha",
		Type: "task", State: "todo", Priority: "medium",
		Created: time.Now().UTC(), Updated: time.Now().UTC(),
	}
	require.NoError(t, cards.CreateCard(context.Background(), "project-alpha", card))

	pbStore, err := storage.NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	committer := &recordingCommitter{}
	queue := gitops.NewCommitQueueWithCommitter(committer, 0)

	t.Cleanup(func() { _ = queue.Close(context.Background()) })

	fake := clock.Fake(time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	bus := events.NewBus()

	svc := NewPlaybookService(pbStore, cards, bus, fake, true)
	svc.SetCommitQueue(queue)

	pushed := 0

	svc.SetOnCommit(func() { pushed++ })

	return &playbookTestEnv{svc: svc, cards: cards, committer: committer, bus: bus, clk: fake, pushed: &pushed}
}

// createCard adds an extra card to project-alpha for tests that need more
// than the fixture's default ALPHA-001.
func (env *playbookTestEnv) createCard(t *testing.T, id, state string) {
	t.Helper()

	card := &board.Card{
		ID: id, Title: id, Project: "project-alpha",
		Type: "task", State: state, Priority: "medium",
		Created: time.Now().UTC(), Updated: time.Now().UTC(),
	}
	require.NoError(t, env.cards.CreateCard(context.Background(), "project-alpha", card))
}

func validProjectConfigForPlaybooks() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name: "project-alpha", Prefix: "ALPHA", NextID: 2,
		States: []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:  []string{"task"}, Priorities: []string{"medium"},
		Transitions: map[string][]string{
			"todo": {"in_progress"}, "in_progress": {"done"}, "done": {},
			"stalled": {"todo"}, "not_planned": {"todo"},
		},
	}
}

func TestPlaybookService_CreateResolvesAndCommits(t *testing.T) {
	env := newPlaybookTestEnv(t)

	ch, unsub := env.bus.Subscribe()
	defer unsub()

	detail, err := env.svc.Create(context.Background(), CreatePlaybookInput{
		Title: "Alpha Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001", Note: "merge first"},
			{Type: board.EntryTypeManual, Text: "redeploy"},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "alpha-rollout", detail.ID)
	assert.Equal(t, "human:alice", detail.CreatedBy)
	assert.Equal(t, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC), detail.Created, "created_at from the injected clock")
	assert.Equal(t, detail.Created, detail.Updated)
	assert.Equal(t, 0, detail.Complete)
	assert.Equal(t, 2, detail.Total)
	require.Len(t, detail.Entries, 2)
	assert.Equal(t, "e1", detail.Entries[0].ID)
	assert.Equal(t, "First card", detail.Entries[0].CardTitle)
	assert.Equal(t, "todo", detail.Entries[0].CardState)
	assert.False(t, detail.Entries[0].Complete)

	assert.Equal(t, [][]string{{"playbooks/alpha-rollout.yaml"}}, env.committer.paths)
	assert.Contains(t, env.committer.msgs[0], "playbook(alpha-rollout): created")
	assert.Equal(t, 1, *env.pushed)

	ev := <-ch
	assert.Equal(t, events.PlaybookCreated, ev.Type)
	assert.Empty(t, ev.Project)
	assert.Equal(t, "alpha-rollout", ev.Data["id"])
}

func TestPlaybookService_CreateSlugCollisionUniquifies(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	first, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "Rollout", AgentID: "human:a"})
	require.NoError(t, err)
	second, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "Rollout!", AgentID: "human:a"})
	require.NoError(t, err)
	assert.Equal(t, "rollout", first.ID)
	assert.Equal(t, "rollout-2", second.ID)
}

func TestPlaybookService_CreateAllOrNothing(t *testing.T) {
	env := newPlaybookTestEnv(t)
	_, err := env.svc.Create(context.Background(), CreatePlaybookInput{
		Title: "Bad", AgentID: "human:a",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeManual, Text: "fine"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-999"},
		},
	})
	require.ErrorIs(t, err, ErrInvalidPlaybookEntry)
	assert.Contains(t, err.Error(), "entry 1")

	list, err := env.svc.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list, "nothing written on batch failure")
	assert.Empty(t, env.committer.paths, "nothing committed on batch failure")
}

func TestPlaybookService_CreateDuplicateCardRejected(t *testing.T) {
	env := newPlaybookTestEnv(t)
	_, err := env.svc.Create(context.Background(), CreatePlaybookInput{
		Title: "Dup", AgentID: "human:a",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
		},
	})
	assert.ErrorIs(t, err, ErrDuplicateCardEntry)
}

func TestPlaybookService_ProgressCountsTerminalAndMissing(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Progress", AgentID: "human:a",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	// Card reaches a terminal state -> entry complete.
	card, err := env.cards.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)

	card.State = "done"
	require.NoError(t, env.cards.UpdateCard(ctx, "project-alpha", card))

	detail, err := env.svc.Get(ctx, "progress")
	require.NoError(t, err)
	assert.True(t, detail.Entries[0].Complete)
	assert.Equal(t, 1, detail.Complete)

	// Card deleted -> broken ref: kept, Missing, incomplete, still in Total.
	require.NoError(t, env.cards.DeleteCard(ctx, "project-alpha", "ALPHA-001"))
	detail, err = env.svc.Get(ctx, "progress")
	require.NoError(t, err)
	assert.True(t, detail.Entries[0].Missing)
	assert.False(t, detail.Entries[0].Complete)
	assert.Equal(t, 0, detail.Complete)
	assert.Equal(t, 1, detail.Total)
}

func TestPlaybookService_UpdateMetaAndDelete(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "Meta", AgentID: "human:a"})
	require.NoError(t, err)

	newTitle := "Renamed"
	detail, err := env.svc.UpdateMeta(ctx, "meta", UpdatePlaybookInput{Title: &newTitle}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", detail.Title)
	assert.Equal(t, "meta", detail.ID, "id is immutable under title edits")

	require.NoError(t, env.svc.Delete(ctx, "meta", "human:a"))
	_, err = env.svc.Get(ctx, "meta")
	assert.ErrorIs(t, err, storage.ErrPlaybookNotFound)
}

func TestPlaybookService_CommitFailureRollsBack(t *testing.T) {
	env := newPlaybookTestEnv(t)
	env.committer.fail = errors.New("boom")

	_, err := env.svc.Create(context.Background(), CreatePlaybookInput{Title: "Doomed", AgentID: "human:a"})
	require.Error(t, err)

	list, listErr := env.svc.List(context.Background())
	require.NoError(t, listErr)
	assert.Empty(t, list, "store rolled back after commit failure")
	assert.Equal(t, 0, *env.pushed)
}

func TestPlaybookService_ConcurrentMutationsSerialized(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "Race", AgentID: "human:a"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, aerr := env.svc.AddEntry(ctx, "race", PlaybookEntryInput{Type: board.EntryTypeManual, Text: "step"}, "human:a")
			assert.NoError(t, aerr)
		})
	}

	wg.Wait()

	detail, err := env.svc.Get(ctx, "race")
	require.NoError(t, err)
	assert.Equal(t, 20, detail.Total, "no lost updates")

	seen := map[string]bool{}
	for _, e := range detail.Entries {
		assert.False(t, seen[e.ID], "entry IDs unique under concurrency")
		seen[e.ID] = true
	}
}

func TestPlaybookService_AddEntryAppends(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Add", AgentID: "human:a",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeManual, Text: "first"}},
	})
	require.NoError(t, err)

	detail, err := env.svc.AddEntry(ctx, "add", PlaybookEntryInput{Type: board.EntryTypeManual, Text: "second"}, "human:a")
	require.NoError(t, err)
	require.Len(t, detail.Entries, 2)
	assert.Equal(t, "second", detail.Entries[1].Text)
	assert.Equal(t, "e2", detail.Entries[1].ID)
}

func TestPlaybookService_EntryIDsNeverReused(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "IDs", AgentID: "human:a",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeManual, Text: "one"}},
	})
	require.NoError(t, err)

	_, err = env.svc.RemoveEntry(ctx, "ids", "e1", "human:a")
	require.NoError(t, err)

	detail, err := env.svc.AddEntry(ctx, "ids", PlaybookEntryInput{Type: board.EntryTypeManual, Text: "two"}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, "e2", detail.Entries[0].ID, "deleted e1 is never reused")
}

func TestPlaybookService_DoneToggle(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Done", AgentID: "human:a",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeManual, Text: "gate"}},
	})
	require.NoError(t, err)

	yes, no := true, false

	detail, err := env.svc.UpdateEntry(ctx, "done", "e1", UpdateEntryInput{Done: &yes}, "human:bob")
	require.NoError(t, err)

	e := detail.Entries[0]
	assert.True(t, e.Done)
	assert.Equal(t, "human:bob", e.DoneBy)
	require.NotNil(t, e.DoneAt)
	assert.Equal(t, 1, detail.Complete)

	detail, err = env.svc.UpdateEntry(ctx, "done", "e1", UpdateEntryInput{Done: &no}, "human:bob")
	require.NoError(t, err)

	e = detail.Entries[0]
	assert.False(t, e.Done)
	assert.Empty(t, e.DoneBy, "unchecking clears the stamp")
	assert.Nil(t, e.DoneAt)

	env.clk.Advance(time.Hour)
	detail, err = env.svc.UpdateEntry(ctx, "done", "e1", UpdateEntryInput{Done: &yes}, "human:carol")
	require.NoError(t, err)
	assert.Equal(t, "human:carol", detail.Entries[0].DoneBy, "re-check restamps from the new caller")
	require.NotNil(t, detail.Entries[0].DoneAt)
	assert.Equal(t, time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC), *detail.Entries[0].DoneAt, "done_at from the advanced clock")
	assert.Equal(t, time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC), detail.Updated, "updated_at bumped from the advanced clock")
}

func TestPlaybookService_UpdateEntryTypeValidation(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Types", AgentID: "human:a",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	yes := true
	_, err = env.svc.UpdateEntry(ctx, "types", "e1", UpdateEntryInput{Done: &yes}, "human:a")
	require.ErrorIs(t, err, ErrInvalidPlaybookEntry, "done on a card entry is invalid")

	text := "nope"
	_, err = env.svc.UpdateEntry(ctx, "types", "e1", UpdateEntryInput{Text: &text}, "human:a")
	require.ErrorIs(t, err, ErrInvalidPlaybookEntry, "text on a card entry is invalid")

	note := "notes are fine on both types"
	detail, err := env.svc.UpdateEntry(ctx, "types", "e1", UpdateEntryInput{Note: &note}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, note, detail.Entries[0].Note)
}

func TestPlaybookService_MoveSemantics(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Move", AgentID: "human:a",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeManual, Text: "a"},
			{Type: board.EntryTypeManual, Text: "b"},
			{Type: board.EntryTypeManual, Text: "c"},
		},
	})
	require.NoError(t, err)

	order := func(d *PlaybookDetail) []string {
		ids := make([]string, len(d.Entries))
		for i, e := range d.Entries {
			ids[i] = e.ID
		}

		return ids
	}

	pos := 0 // move e3 to the front
	detail, err := env.svc.UpdateEntry(ctx, "move", "e3", UpdateEntryInput{Position: &pos}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, []string{"e3", "e1", "e2"}, order(detail))

	pos = 99 // beyond end clamps to end
	detail, err = env.svc.UpdateEntry(ctx, "move", "e3", UpdateEntryInput{Position: &pos}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, []string{"e1", "e2", "e3"}, order(detail))

	pos = -1
	_, err = env.svc.UpdateEntry(ctx, "move", "e1", UpdateEntryInput{Position: &pos}, "human:a")
	assert.ErrorIs(t, err, ErrInvalidPlaybookEntry, "negative position rejected")
}

func TestPlaybookService_EntryNotFound(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()
	_, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "NF", AgentID: "human:a"})
	require.NoError(t, err)

	_, err = env.svc.RemoveEntry(ctx, "nf", "e9", "human:a")
	assert.ErrorIs(t, err, ErrPlaybookEntryNotFound)
}

func TestPlaybookService_ListSegments(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "done")
	env.createCard(t, "ALPHA-003", "todo")

	card, err := env.cards.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)

	card.State = "in_progress"
	require.NoError(t, env.cards.UpdateCard(ctx, "project-alpha", card))

	_, err = env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Segments", AgentID: "human:a",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}, // active
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, // complete
			{Type: board.EntryTypeManual, Text: "manual done"},                       // complete, once toggled
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-003"}, // missing, once deleted
		},
	})
	require.NoError(t, err)

	yes := true
	_, err = env.svc.UpdateEntry(ctx, "segments", "e3", UpdateEntryInput{Done: &yes}, "human:a")
	require.NoError(t, err)

	require.NoError(t, env.cards.DeleteCard(ctx, "project-alpha", "ALPHA-003"))

	list, err := env.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	summary := list[0]
	assert.Equal(t, []string{"active", "complete", "complete", "missing"}, summary.Segments)
	assert.Equal(t, 2, summary.Complete)
	assert.Equal(t, 4, summary.Total)
	assert.Equal(t, 1, summary.Projects, "distinct projects among card entries")
}
