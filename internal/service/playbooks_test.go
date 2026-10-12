package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
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
	cardSvc   *CardService
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

	gitMgr, err := gitops.NewManager(dir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	cardSvc := NewCardService(cards, gitMgr, lock.NewManager(cards, 30*time.Minute), bus, dir, nil, false, false)
	cardSvc.SetPlaybookLister(pbStore)

	svc := NewPlaybookService(pbStore, cards, bus, fake, true)
	svc.SetCommitQueue(queue)
	svc.SetCardForcer(cardSvc)

	pushed := 0

	svc.SetOnCommit(func() { pushed++ })

	return &playbookTestEnv{svc: svc, cardSvc: cardSvc, cards: cards, committer: committer, bus: bus, clk: fake, pushed: &pushed}
}

// createProject adds a project with one todo card <prefix>-001. repo may be
// empty to model a project without a GitHub repository.
func (env *playbookTestEnv) createProject(t *testing.T, name, prefix, repo string) {
	t.Helper()

	cfg := validProjectConfigForPlaybooks()
	cfg.Name, cfg.Prefix, cfg.Repo = name, prefix, repo
	require.NoError(t, env.cards.SaveProject(context.Background(), cfg))

	card := &board.Card{
		ID: prefix + "-001", Title: prefix + " first", Project: name,
		Type: "task", State: "todo", Priority: "medium",
		Created: time.Now().UTC(), Updated: time.Now().UTC(),
	}
	require.NoError(t, env.cards.CreateCard(context.Background(), name, card))
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
		Repo:   "https://github.com/acme/alpha.git",
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

func TestSummarizeDetail_NextAndGates(t *testing.T) {
	entry := func(typ, project, card, title, text string, complete bool) PlaybookEntryDetail {
		return PlaybookEntryDetail{
			Type:      typ,
			Project:   project,
			Card:      card,
			Text:      text,
			CardTitle: title,
			Complete:  complete,
		}
	}

	t.Run("names the first incomplete card entry", func(t *testing.T) {
		s := SummarizeDetail(&PlaybookDetail{Entries: []PlaybookEntryDetail{
			entry(board.EntryTypeCard, "alpha", "ALPHA-1", "First", "", true),
			entry(board.EntryTypeCard, "beta", "BETA-2", "Second", "", false),
			entry(board.EntryTypeManual, "", "", "", "ship it", false),
		}})
		require.NotNil(t, s.Next)
		assert.Equal(t, &PlaybookNext{Type: board.EntryTypeCard, Project: "beta", Card: "BETA-2", Title: "Second"}, s.Next)
		assert.Equal(t, []int{2}, s.Gates, "indexes of manual entries")
	})

	t.Run("names a manual frontier by its text", func(t *testing.T) {
		s := SummarizeDetail(&PlaybookDetail{Entries: []PlaybookEntryDetail{
			entry(board.EntryTypeManual, "", "", "", "ship it", false),
			entry(board.EntryTypeCard, "alpha", "ALPHA-1", "First", "", false),
		}})
		assert.Equal(t, &PlaybookNext{Type: board.EntryTypeManual, Title: "ship it"}, s.Next)
		assert.Equal(t, []int{0}, s.Gates)
	})

	t.Run("names a missing card frontier with an empty title", func(t *testing.T) {
		missing := entry(board.EntryTypeCard, "alpha", "ALPHA-9", "", "", false)
		missing.Missing = true
		s := SummarizeDetail(&PlaybookDetail{Entries: []PlaybookEntryDetail{
			entry(board.EntryTypeCard, "alpha", "ALPHA-1", "First", "", true),
			missing,
			entry(board.EntryTypeCard, "alpha", "ALPHA-2", "Second", "", false),
		}})
		assert.Equal(t, &PlaybookNext{Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-9"}, s.Next)
	})

	t.Run("summarizes an empty playbook without next, gates or segments", func(t *testing.T) {
		s := SummarizeDetail(&PlaybookDetail{})
		assert.Nil(t, s.Next)
		assert.Nil(t, s.Gates)
		assert.Empty(t, s.Segments)
	})

	t.Run("omits next when every entry is complete and gates when none are manual", func(t *testing.T) {
		s := SummarizeDetail(&PlaybookDetail{Entries: []PlaybookEntryDetail{
			entry(board.EntryTypeCard, "alpha", "ALPHA-1", "First", "", true),
		}})
		assert.Nil(t, s.Next)
		assert.Nil(t, s.Gates)
	})
}

func TestPlaybookCreateVerified_PublishesNothingWhenThePushNeverLands(t *testing.T) {
	env := newPlaybookTestEnv(t)

	env.svc.SetSyncRunner(func(ctx context.Context, _ string, m SyncMutation) (SyncOutcome, error) {
		env.svc.LockWrites()
		defer env.svc.UnlockWrites()

		if err := m.Apply(ctx); err != nil {
			return SyncOutcome{BodyRan: true}, err
		}

		if err := m.Undo(ctx); err != nil {
			return SyncOutcome{BodyRan: true}, err
		}

		return SyncOutcome{BodyRan: true}, errors.New("push: remote unreachable")
	}, func(_ context.Context, _ []string, _ string) error { return nil })

	ch, unsub := env.bus.Subscribe()
	defer unsub()

	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "Release train", AgentID: "human:a"})
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	list, err := env.svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "the undo removed the playbook")

	select {
	case e := <-ch:
		t.Fatalf("published %s for a create that never landed", e.Type)
	default:
	}
}

func TestPlaybookCreateVerified_RunsInsideTheCycle(t *testing.T) {
	env := newPlaybookTestEnv(t)
	calls, commits := 0, 0

	env.svc.SetSyncRunner(func(ctx context.Context, trigger string, m SyncMutation) (SyncOutcome, error) {
		calls++

		env.svc.LockWrites()
		defer env.svc.UnlockWrites()

		if err := m.Apply(ctx); err != nil {
			return SyncOutcome{BodyRan: true}, err
		}

		return SyncOutcome{BodyRan: true, Pushed: true}, nil
	}, func(_ context.Context, paths []string, _ string) error {
		commits++

		assert.Equal(t, []string{"playbooks/release-train.yaml"}, paths)

		return nil
	})

	detail, err := env.svc.Create(context.Background(), CreatePlaybookInput{Title: "Release train", AgentID: "human:a"})
	require.NoError(t, err)
	assert.Equal(t, "release-train", detail.ID)
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, commits, "the cycle commits directly; the queue is paused inside it")
	assert.Empty(t, env.committer.msgs, "nothing went through the queue")
}

func TestPlaybookService_MakeRunnableForcesSettings(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")
	env.createCard(t, "ALPHA-003", "done")

	detail, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeManual, Text: "deploy"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-003"},
		},
	})
	require.NoError(t, err)
	assert.False(t, detail.Runnable)

	got, err := env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true), BaseBranch: new("main")}, "human:alice")
	require.NoError(t, err)
	assert.True(t, got.Runnable)
	assert.Equal(t, "main", got.BaseBranch)

	for _, id := range []string{"ALPHA-001", "ALPHA-002"} {
		card, err := env.cardSvc.GetCard(ctx, "project-alpha", id)
		require.NoError(t, err)
		assert.True(t, card.HasPlaybookSettings("playbook/rollout"), id)
		require.NotNil(t, card.PlaybookLock, id)
		assert.Equal(t, "rollout", card.PlaybookLock.ID)
	}

	done, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-003")
	require.NoError(t, err)
	assert.False(t, done.Autonomous, "terminal cards are left alone")

	// Idempotent: a second make-runnable is a no-op on the cards.
	before, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)
	after, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.Len(t, after.ActivityLog, len(before.ActivityLog))
}

func TestPlaybookService_MakeRunnableRefusesProjectWithoutRepo(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createProject(t, "project-beta", "BETA", "")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Mixed", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeCard, Project: "project-beta", Card: "BETA-001"},
		},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "mixed", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookProjectNoRepo)
	assert.Contains(t, err.Error(), "project-beta")

	// Nothing was forced and the flag did not flip.
	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.False(t, card.Autonomous)

	got, err := env.svc.Get(ctx, "mixed")
	require.NoError(t, err)
	assert.False(t, got.Runnable)
}

func TestPlaybookService_MakeRunnableRefusesCardOwnedElsewhere(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "First", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "first", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	_, err = env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Second", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
		},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "second", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardOwned)
	assert.Contains(t, err.Error(), "ALPHA-001")
	assert.Contains(t, err.Error(), "first")

	// This add succeeds because "second" is not runnable at this point (its
	// UpdateMeta call above failed), so ALPHA-002 is not owned by any
	// runnable playbook yet.
	_, err = env.svc.AddEntry(ctx, "first", PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, "human:alice")
	require.NoError(t, err, "ALPHA-002 is only in the non-runnable second playbook")

	_, err = env.svc.UpdateMeta(ctx, "second", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardOwned)
}

// TestPlaybookService_AddEntryRefusesCardOwnedElsewhere verifies AddEntry
// applies the same cross-playbook ownership check as MakeRunnable: a card
// already owned by another runnable playbook cannot be added to a second
// runnable playbook.
func TestPlaybookService_AddEntryRefusesCardOwnedElsewhere(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "First", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "first", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	_, err = env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Second", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}},
	})
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "second", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	_, err = env.svc.AddEntry(ctx, "second", PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardOwned)
	assert.Contains(t, err.Error(), "ALPHA-001")
}

func TestPlaybookService_AddEntryForcesOnRunnable(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	_, err = env.svc.AddEntry(ctx, "rollout", PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, "human:alice")
	require.NoError(t, err)

	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-002")
	require.NoError(t, err)
	assert.True(t, card.HasPlaybookSettings("playbook/rollout"))

	// A manual entry needs no forcing and no repo.
	_, err = env.svc.AddEntry(ctx, "rollout", PlaybookEntryInput{Type: board.EntryTypeManual, Text: "deploy"}, "human:alice")
	require.NoError(t, err)

	// A card in a project without a repo cannot join a runnable playbook.
	env.createProject(t, "project-beta", "BETA", "")
	_, err = env.svc.AddEntry(ctx, "rollout", PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-beta", Card: "BETA-001"}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookProjectNoRepo)
}

func TestPlaybookService_RunnableFalseAndBaseBranchRules(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	// SetRun on a non-runnable playbook is refused.
	now := env.clk.Now()
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusRunning, StartedAt: now, UpdatedAt: now}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookNotRunnable)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true), BaseBranch: new("main")}, "human:alice")
	require.NoError(t, err)

	got, err := env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusRunning, StartedAt: now, UpdatedAt: now, Entry: "e1"}, "human:alice")
	require.NoError(t, err)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusRunning, got.Run.Status)

	// Active run: neither the flag nor the base branch may change.
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(false)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookRunActive)
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{BaseBranch: new("develop")}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookRunActive)

	// Title edits still work during a run.
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Title: new("Rollout v2")}, "human:alice")
	require.NoError(t, err)

	// Stopped run: unchecking is allowed, drops the run block, keeps cards.
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusStopped, StartedAt: now, UpdatedAt: now}, "human:alice")
	require.NoError(t, err)

	got, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(false)}, "human:alice")
	require.NoError(t, err)
	assert.False(t, got.Runnable)
	assert.Nil(t, got.Run)

	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.True(t, card.HasPlaybookSettings("playbook/rollout"), "settings are never reverted")
	assert.Nil(t, card.PlaybookLock, "but the lock is gone")
}

func TestPlaybookService_MakeRunnableWithoutForcerFails(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.svc.SetCardForcer(nil)

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forcer")
}

func TestPlaybookService_ReassertValidatesOwnership(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")

	// Two runnable playbooks that do not overlap.
	for _, tc := range []struct{ title, card string }{{"First", "ALPHA-001"}, {"Second", "ALPHA-002"}} {
		_, err := env.svc.Create(ctx, CreatePlaybookInput{
			Title: tc.title, AgentID: "human:alice",
			Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: tc.card}},
		})
		require.NoError(t, err)
		_, err = env.svc.UpdateMeta(ctx, strings.ToLower(tc.title), UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
		require.NoError(t, err)
	}

	// Simulate a hand-edited file: "second" now also lists ALPHA-001, owned by "first".
	p, err := env.svc.store.Get(ctx, "second")
	require.NoError(t, err)

	p.Entries = append(p.Entries, board.PlaybookEntry{ID: "e2", Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"})
	p.NextEntryID = 3
	require.NoError(t, env.svc.store.Save(ctx, p))

	// Re-asserting runnable on "second" must refuse rather than force ALPHA-001 into a second owner.
	_, err = env.svc.UpdateMeta(ctx, "second", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardOwned)
	assert.Contains(t, err.Error(), "ALPHA-001")

	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.Equal(t, "playbook/first", card.BaseBranch, "still owned by first")
}

func TestPlaybookService_DetailCarriesRunFieldsAndRepos(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createProject(t, "project-beta", "BETA", "git@github.com:acme/beta.git")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-beta", Card: "BETA-001"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeManual, Text: "deploy"},
		},
	})
	require.NoError(t, err)

	plain, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.False(t, plain.Runnable)
	assert.Empty(t, plain.Branch)
	assert.Nil(t, plain.Repos)

	got, err := env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)
	assert.True(t, got.Runnable)
	assert.Equal(t, "playbook/rollout", got.Branch)
	require.Len(t, got.Repos, 2, "one link per distinct project, in entry order")
	assert.Equal(t, "project-beta", got.Repos[0].Project)
	assert.Equal(t, "https://github.com/acme/beta/compare/playbook/rollout?expand=1", got.Repos[0].CompareURL)
	assert.Equal(t, "project-alpha", got.Repos[1].Project)
	assert.Equal(t, "https://github.com/acme/alpha/compare/playbook/rollout?expand=1", got.Repos[1].CompareURL)

	got, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{BaseBranch: new("main")}, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/acme/beta/compare/main...playbook/rollout?expand=1", got.Repos[0].CompareURL)

	now := env.clk.Now()
	got, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusWaiting, StartedAt: now, UpdatedAt: now, Entry: "e3", Reason: "awaiting check-off"}, "human:alice")
	require.NoError(t, err)
	require.NotNil(t, got.Run)
	assert.Equal(t, "e3", got.Run.Entry)

	summaries, err := env.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.True(t, summaries[0].Runnable)
	assert.Equal(t, board.RunStatusWaiting, summaries[0].RunStatus)

	slim := SummarizeDetail(got)
	assert.True(t, slim.Runnable)
	assert.Equal(t, board.RunStatusWaiting, slim.RunStatus)
}

func TestPlaybookService_RunEventsCarryRunStatus(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)
	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	ch, unsub := env.bus.Subscribe()
	defer unsub()

	now := env.clk.Now()
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusWaiting, StartedAt: now, UpdatedAt: now, Entry: "e1"}, "human:alice")
	require.NoError(t, err)

	select {
	case ev := <-ch:
		assert.Equal(t, events.PlaybookUpdated, ev.Type)
		assert.Equal(t, "rollout", ev.Data["id"])
		assert.Equal(t, board.RunStatusWaiting, ev.Data["run_status"])
		assert.Equal(t, "e1", ev.Data["run_entry"])
	case <-time.After(time.Second):
		t.Fatal("no playbook.updated event")
	}

	// A metadata edit on a playbook with no run carries no run keys.
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, nil, "human:alice")
	require.NoError(t, err)
	<-ch

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Title: new("Renamed")}, "human:alice")
	require.NoError(t, err)

	ev := <-ch
	_, has := ev.Data["run_status"]
	assert.False(t, has)
}

func TestPlaybookService_SetRunIfGuardsTheWrite(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	now := env.clk.Now()
	stored, err := env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{
		Status: board.RunStatusRunning, StartedAt: now, UpdatedAt: now, Entry: "e1",
	}, "human:alice")
	require.NoError(t, err)
	require.NotNil(t, stored.Run)

	updatedAt := stored.Updated

	// A guard that refuses returns its own error and writes nothing: neither
	// the run block nor the playbook's updated_at moves.
	refuse := errors.New("run moved underneath")

	env.clk.Advance(time.Minute)

	_, err = env.svc.SetRunIf(ctx, "rollout",
		func(current *board.PlaybookRun) error {
			require.NotNil(t, current, "the guard sees the freshly loaded block")
			assert.Equal(t, board.RunStatusRunning, current.Status)

			return refuse
		},
		&board.PlaybookRun{Status: board.RunStatusStopped, StartedAt: now, UpdatedAt: env.clk.Now()}, "human:alice")
	require.ErrorIs(t, err, refuse)

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusRunning, got.Run.Status, "the refused write left the block alone")
	assert.Equal(t, "e1", got.Run.Entry)
	assert.Equal(t, updatedAt, got.Updated, "and never touched updated_at")

	// A guard that passes writes.
	got, err = env.svc.SetRunIf(ctx, "rollout",
		func(*board.PlaybookRun) error { return nil },
		&board.PlaybookRun{Status: board.RunStatusStopped, StartedAt: now, UpdatedAt: env.clk.Now()}, "human:alice")
	require.NoError(t, err)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusStopped, got.Run.Status)
}

func TestPlaybookService_RepoLinksResolveTheDefaultBranch(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createProject(t, "project-beta", "BETA", "git@github.com:acme/beta.git")

	var calls []string

	env.svc.SetDefaultBranchResolver(func(_ context.Context, project, owner, repo string) (string, error) {
		calls = append(calls, project)

		if owner == "acme" && repo == "alpha" {
			return "trunk", nil
		}

		return "", errors.New("github down")
	})

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-beta", Card: "BETA-001"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
		},
	})
	require.NoError(t, err)

	got, err := env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)
	require.Len(t, got.Repos, 2)
	// A failed lookup falls back to the branch-only form; a resolved default
	// branch becomes the base so GitHub renders the compare page.
	assert.Equal(t, "https://github.com/acme/beta/compare/playbook/rollout?expand=1", got.Repos[0].CompareURL)
	assert.Equal(t, "https://github.com/acme/alpha/compare/trunk...playbook/rollout?expand=1", got.Repos[1].CompareURL)
	assert.Equal(t, []string{"project-beta", "project-alpha"}, calls)

	// A read within the retry window reuses the name and does not retry the
	// failure, so SSE-driven refetches never hammer GitHub.
	got, err = env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/acme/alpha/compare/trunk...playbook/rollout?expand=1", got.Repos[1].CompareURL)
	assert.Equal(t, []string{"project-beta", "project-alpha"}, calls, "cached name and cached failure")

	env.clk.Advance(2 * time.Minute)

	_, err = env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.Equal(t, []string{"project-beta", "project-alpha", "project-beta"}, calls, "only the failure is retried")

	// An explicit base branch never consults the resolver.
	got, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{BaseBranch: new("main")}, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/acme/beta/compare/main...playbook/rollout?expand=1", got.Repos[0].CompareURL)
	assert.Equal(t, "https://github.com/acme/alpha/compare/main...playbook/rollout?expand=1", got.Repos[1].CompareURL)
	assert.Len(t, calls, 3)
}

func TestPlaybookService_RemoveEntryDuringRun(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeManual, Text: "deploy"},
			{Type: board.EntryTypeManual, Text: "verify"},
		},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	now := env.clk.Now()
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{
		Status: board.RunStatusWaiting, StartedAt: now, UpdatedAt: now, Entry: "e1", Reason: "ALPHA-001 parked",
	}, "human:alice")
	require.NoError(t, err)

	// The run's current entry is refused while the run is active: its card
	// would keep running unowned and still merge into the playbook branch.
	_, err = env.svc.RemoveEntry(ctx, "rollout", "e1", "human:alice")
	require.ErrorIs(t, err, ErrPlaybookRunActive)
	assert.Contains(t, err.Error(), "stop the run first")

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	require.Len(t, got.Entries, 3)
	assert.Equal(t, "e1", got.Entries[0].ID)
	require.NotNil(t, got.Run)
	assert.Equal(t, "e1", got.Run.Entry)
	assert.Equal(t, "ALPHA-001 parked", got.Run.Reason)

	// A queued entry stays removable and the run block is untouched; the
	// walker's next pass finds a new frontier.
	got, err = env.svc.RemoveEntry(ctx, "rollout", "e2", "human:alice")
	require.NoError(t, err)
	require.Len(t, got.Entries, 2)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusWaiting, got.Run.Status)
	assert.Equal(t, "e1", got.Run.Entry)
	assert.Equal(t, "ALPHA-001 parked", got.Run.Reason)

	// Once the run is stopped its former entry can go. Validate rejects a
	// run entry that names no entry, so the dangling pointer clears.
	ended := env.clk.Now()
	_, err = env.svc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{
		Status: board.RunStatusStopped, StartedAt: now, UpdatedAt: ended, EndedAt: &ended,
		Entry: "e1", Reason: "stopped by human:alice",
	}, "human:alice")
	require.NoError(t, err)

	got, err = env.svc.RemoveEntry(ctx, "rollout", "e1", "human:alice")
	require.NoError(t, err)
	require.Len(t, got.Entries, 1)
	assert.Equal(t, "e3", got.Entries[0].ID)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusStopped, got.Run.Status)
	assert.Empty(t, got.Run.Entry)
	assert.Empty(t, got.Run.Reason)
}

// failingForcer refuses one card and delegates every other card to the
// real card service, so a make-runnable can fail part-way.
type failingForcer struct {
	inner PlaybookCardForcer
	card  string
}

func (f failingForcer) ForcePlaybookSettings(ctx context.Context, project, id, playbookID, branch, agentID string) (*board.Card, error) {
	if id == f.card {
		return nil, errors.New("commit failed: disk full")
	}

	return f.inner.ForcePlaybookSettings(ctx, project, id, playbookID, branch, agentID)
}

// syncBuffer is a bytes.Buffer safe for the logger's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// captureLogs routes slog's default logger into a buffer for one test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()

	buf := &syncBuffer{}

	prev := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return buf
}

func TestPlaybookService_MakeRunnableNamesTheCardsItCouldNotForce(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")
	env.svc.SetCardForcer(failingForcer{inner: env.cardSvc, card: "ALPHA-002"})

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
		},
	})
	require.NoError(t, err)

	logs := captureLogs(t)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardForce)
	assert.Contains(t, err.Error(), "project-alpha/ALPHA-002")
	assert.NotContains(t, err.Error(), "disk full", "the cause stays in the server log")

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.False(t, got.Runnable, "a failed force leaves the flag off")

	first, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.True(t, first.HasPlaybookSettings("playbook/rollout"), "cards forced before the failure keep their settings")

	assert.Contains(t, logs.String(), "disk full", "the raw cause is logged")
	assert.Contains(t, logs.String(), "the cards keep them")
	assert.Contains(t, logs.String(), "project-alpha/ALPHA-001")
}

func TestPlaybookService_MakeRunnableCommitFailureWarnsAboutForcedCards(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	logs := captureLogs(t)

	// The playbook's own commit fails after the card was forced; the card
	// commit goes through the card service's git manager and succeeds.
	env.committer.fail = errors.New("boom")

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.Error(t, err)

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.False(t, got.Runnable, "the playbook rolled back")

	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.True(t, card.HasPlaybookSettings("playbook/rollout"), "the card write outlived the rollback")

	assert.Contains(t, logs.String(), "the cards keep them")
	assert.Contains(t, logs.String(), "project-alpha/ALPHA-001")
}

func TestPlaybookService_AddEntryCommitFailureWarnsAboutTheForcedCard(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.createCard(t, "ALPHA-002", "todo")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	logs := captureLogs(t)

	// The playbook's own commit fails after the new card was forced.
	env.committer.fail = errors.New("boom")

	_, err = env.svc.AddEntry(ctx, "rollout", PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, "human:alice")
	require.Error(t, err)

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.Len(t, got.Entries, 1, "the entry rolled back")

	card, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-002")
	require.NoError(t, err)
	assert.True(t, card.HasPlaybookSettings("playbook/rollout"), "the card write outlived the rollback")
	assert.Contains(t, logs.String(), "the cards keep them")
	assert.Contains(t, logs.String(), "project-alpha/ALPHA-002")
}

// importedCard writes a jira-sourced card straight to the store.
func (env *playbookTestEnv) importedCard(t *testing.T, id, state string, vetted bool) {
	t.Helper()

	card := &board.Card{
		ID: id, Title: id, Project: "project-alpha",
		Type: "task", State: state, Priority: "medium", Vetted: vetted,
		Source:  &board.Source{System: "jira", ExternalID: "PROJ-" + id, ExternalURL: "https://acme.atlassian.net/browse/PROJ-1"},
		Created: time.Now().UTC(), Updated: time.Now().UTC(),
	}
	require.NoError(t, env.cards.CreateCard(context.Background(), "project-alpha", card))
}

func TestPlaybookService_MakeRunnableRefusesUnvettedCards(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.importedCard(t, "ALPHA-002", "todo", false)
	env.importedCard(t, "ALPHA-003", "done", false) // terminal: never runs, never flagged

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Imported", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}, // no source
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-003"},
		},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "imported", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardNotVetted)
	assert.Contains(t, err.Error(), "project-alpha/ALPHA-002")
	assert.NotContains(t, err.Error(), "ALPHA-003")
	assert.NotContains(t, err.Error(), "ALPHA-001")

	// Nothing was forced and the flag did not flip.
	first, err := env.cardSvc.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)
	assert.False(t, first.Autonomous)

	got, err := env.svc.Get(ctx, "imported")
	require.NoError(t, err)
	assert.False(t, got.Runnable)

	// Vetting the card clears the refusal.
	card, err := env.cards.GetCard(ctx, "project-alpha", "ALPHA-002")
	require.NoError(t, err)

	card.Vetted = true
	require.NoError(t, env.cards.UpdateCard(ctx, "project-alpha", card))

	got, err = env.svc.UpdateMeta(ctx, "imported", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)
	assert.True(t, got.Runnable)
}

func TestPlaybookService_AddEntryToRunnableAcceptsUnvettedCard(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	env.importedCard(t, "ALPHA-002", "todo", false)

	got, err := env.svc.AddEntry(ctx, "rollout",
		PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, "human:alice")
	require.NoError(t, err, "the runner gates an unvetted card; adding it is allowed")
	assert.Len(t, got.Entries, 2)
}

func TestPlaybookService_MakeRunnableSkipsMissingCardsForVetting(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	env.importedCard(t, "ALPHA-002", "todo", false)

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Gone", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}, // sourceless
			{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
		},
	})
	require.NoError(t, err)

	require.NoError(t, env.cards.DeleteCard(ctx, "project-alpha", "ALPHA-002"))

	got, err := env.svc.UpdateMeta(ctx, "gone", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err, "a missing card is already flagged on read; it never blocks vetting")
	assert.True(t, got.Runnable)
}

func TestPlaybookService_ReassertRunnableRefusesUnvettedAddedCard(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	env.importedCard(t, "ALPHA-002", "todo", false)

	_, err = env.svc.AddEntry(ctx, "rollout",
		PlaybookEntryInput{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"}, "human:alice")
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.ErrorIs(t, err, ErrPlaybookCardNotVetted)
	assert.Contains(t, err.Error(), "project-alpha/ALPHA-002")

	got, err := env.svc.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.True(t, got.Runnable, "the refusal leaves the playbook runnable")
}

// runMoveFixture is the starting point of a run-move case: a runnable
// "rollout" with e1..e3 on cards ALPHA-001..ALPHA-003 and a manual e4.
type runMoveFixture struct {
	importedE2 bool               // e2's card is an unvetted import, else a plain card
	done       []string           // cards moved to done before the run is written
	run        *board.PlaybookRun // nil leaves the playbook without a run
}

func (f runMoveFixture) build(t *testing.T) *playbookTestEnv {
	t.Helper()

	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	if f.importedE2 {
		env.importedCard(t, "ALPHA-002", "todo", false)
	} else {
		env.createCard(t, "ALPHA-002", "todo")
	}

	env.createCard(t, "ALPHA-003", "todo")

	_, err := env.svc.Create(ctx, CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-001"}},
	})
	require.NoError(t, err)

	_, err = env.svc.UpdateMeta(ctx, "rollout", UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	// Added after make-runnable, which refuses an unvetted import.
	for _, in := range []PlaybookEntryInput{
		{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-002"},
		{Type: board.EntryTypeCard, Project: "project-alpha", Card: "ALPHA-003"},
		{Type: board.EntryTypeManual, Text: "verify"},
	} {
		_, err = env.svc.AddEntry(ctx, "rollout", in, "human:alice")
		require.NoError(t, err)
	}

	// The run's first card has a live worker unless the case finished it.
	first, err := env.cards.GetCard(ctx, "project-alpha", "ALPHA-001")
	require.NoError(t, err)

	first.WorkerStatus = "running"
	require.NoError(t, env.cards.UpdateCard(ctx, "project-alpha", first))

	for _, id := range f.done {
		card, err := env.cards.GetCard(ctx, "project-alpha", id)
		require.NoError(t, err)

		card.State, card.WorkerStatus = "done", ""
		require.NoError(t, env.cards.UpdateCard(ctx, "project-alpha", card))
	}

	if f.run != nil {
		_, err = env.svc.SetRunIf(ctx, "rollout", nil, f.run, "human:alice")
		require.NoError(t, err)
	}

	return env
}

func entryOrder(d *PlaybookDetail) []string {
	ids := make([]string, len(d.Entries))
	for i, e := range d.Entries {
		ids[i] = e.ID
	}

	return ids
}

func TestPlaybookService_MoveDuringRun(t *testing.T) {
	started := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	run := func(status, entry, reason string) *board.PlaybookRun {
		r := &board.PlaybookRun{Status: status, StartedAt: started, UpdatedAt: started, Entry: entry, Reason: reason}
		if status == board.RunStatusStopped || status == board.RunStatusCompleted {
			r.EndedAt = &started
		}

		return r
	}
	unchanged := []string{"e1", "e2", "e3", "e4"}
	live := runMoveFixture{importedE2: true, run: run(board.RunStatusRunning, "e1", "")}
	// The vetting hold: e1 finished and the run waits on the unvetted e2
	// with no entry recorded.
	held := runMoveFixture{
		importedE2: true, done: []string{"ALPHA-001"},
		run: run(board.RunStatusWaiting, "", "ALPHA-002 not vetted"),
	}

	tests := []struct {
		name      string
		fixture   runMoveFixture
		entry     string
		in        UpdateEntryInput
		wantErr   error
		wantOrder []string
	}{
		{
			name: "running: an unvetted import moved ahead of the live entry is refused", fixture: live,
			entry: "e2", in: UpdateEntryInput{Position: new(0)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name:    "running: a vetted card moved ahead of the live entry is refused",
			fixture: runMoveFixture{run: run(board.RunStatusRunning, "e1", "")},
			entry:   "e2", in: UpdateEntryInput{Position: new(0)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "running: the run's own entry moved behind another is refused", fixture: live,
			entry: "e1", in: UpdateEntryInput{Position: new(1)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "running: a queued entry moved behind the live entry is refused", fixture: live,
			entry: "e4", in: UpdateEntryInput{Position: new(1)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "running: a queued entry moved to the end is refused", fixture: live,
			entry: "e2", in: UpdateEntryInput{Position: new(99)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "vetting hold: a card moved ahead of the held import is refused", fixture: held,
			entry: "e3", in: UpdateEntryInput{Position: new(1)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "vetting hold: the held import moved back is refused", fixture: held,
			entry: "e2", in: UpdateEntryInput{Position: new(2)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "vetting hold: a move behind the held import is refused", fixture: held,
			entry: "e4", in: UpdateEntryInput{Position: new(2)},
			wantErr: ErrPlaybookMoveDuringRun, wantOrder: unchanged,
		},
		{
			name: "running: a position equal to the current one is not a move", fixture: live,
			entry: "e1", in: UpdateEntryInput{Position: new(0), Note: new("pinned")},
			wantOrder: unchanged,
		},
		{
			name: "running: a position that clamps to the current one is not a move", fixture: live,
			entry: "e4", in: UpdateEntryInput{Position: new(99)},
			wantOrder: unchanged,
		},
		{
			name: "running: a note on the run's entry is allowed", fixture: live,
			entry: "e1", in: UpdateEntryInput{Note: new("watch the migration")},
			wantOrder: unchanged,
		},
		{
			name: "vetting hold: a note on a queued entry is allowed", fixture: held,
			entry: "e3", in: UpdateEntryInput{Note: new("after the deploy")},
			wantOrder: unchanged,
		},
		{
			name: "running: checking off a manual entry is allowed", fixture: live,
			entry: "e4", in: UpdateEntryInput{Done: new(true)},
			wantOrder: unchanged,
		},
		{
			name:    "stopped run: every move is allowed",
			fixture: runMoveFixture{importedE2: true, run: run(board.RunStatusStopped, "e1", "stopped by human:alice")},
			entry:   "e2", in: UpdateEntryInput{Position: new(0)},
			wantOrder: []string{"e2", "e1", "e3", "e4"},
		},
		{
			name:    "completed run: every move is allowed",
			fixture: runMoveFixture{importedE2: true, run: run(board.RunStatusCompleted, "", "")},
			entry:   "e2", in: UpdateEntryInput{Position: new(0)},
			wantOrder: []string{"e2", "e1", "e3", "e4"},
		},
		{
			name:    "no run: every move is allowed",
			fixture: runMoveFixture{importedE2: true},
			entry:   "e2", in: UpdateEntryInput{Position: new(0)},
			wantOrder: []string{"e2", "e1", "e3", "e4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.fixture.build(t)
			ctx := context.Background()

			before, err := env.svc.Get(ctx, "rollout")
			require.NoError(t, err)

			_, err = env.svc.UpdateEntry(ctx, "rollout", tt.entry, tt.in, "human:alice")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			got, err := env.svc.Get(ctx, "rollout")
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrder, entryOrder(got))
			assert.Equal(t, before.Run, got.Run, "an entry update never touches the run block")

			for _, e := range got.Entries {
				if e.ID != tt.entry {
					continue
				}

				if tt.in.Note != nil {
					assert.Equal(t, *tt.in.Note, e.Note)
				}

				if tt.in.Done != nil {
					assert.Equal(t, *tt.in.Done, e.Done)
				}
			}
		})
	}
}

func TestPlaybookService_SourceSetOnCreateAndFound(t *testing.T) {
	env := newPlaybookTestEnv(t)
	ctx := context.Background()

	src := &board.Source{System: "jira", ExternalID: "PROJ-1", ExternalURL: "https://acme.atlassian.net/browse/PROJ-1"}

	d, err := env.svc.Create(ctx, CreatePlaybookInput{Title: "PROJ-1 Checkout", AgentID: "human:alice", Source: src})
	require.NoError(t, err)
	assert.Equal(t, src, d.Source)

	src.ExternalID = "MUTATED" // the caller's pointer must not alias the stored source

	_, err = env.svc.UpdateMeta(ctx, d.ID, UpdatePlaybookInput{Title: new("Renamed")}, "human:alice")
	require.NoError(t, err)

	got, err := env.svc.FindBySource(ctx, "jira", "PROJ-1")
	require.NoError(t, err)
	assert.Equal(t, d.ID, got.ID)
	assert.Equal(t, "Renamed", got.Title)
	require.NotNil(t, got.Source, "a metadata update keeps the source")

	_, err = env.svc.FindBySource(ctx, "jira", "PROJ-2")
	require.ErrorIs(t, err, storage.ErrPlaybookNotFound)

	_, err = env.svc.FindBySource(ctx, "github", "PROJ-1")
	require.ErrorIs(t, err, storage.ErrPlaybookNotFound)
}
