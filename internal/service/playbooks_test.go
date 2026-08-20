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
