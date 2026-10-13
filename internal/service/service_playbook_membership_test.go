package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type fakePlaybookLister struct {
	playbooks []*board.Playbook
	err       error
}

func (f *fakePlaybookLister) List(_ context.Context) ([]*board.Playbook, error) {
	return f.playbooks, f.err
}

func cardEntry(project, card string) board.PlaybookEntry {
	return board.PlaybookEntry{ID: "e-" + card, Type: "card", Project: project, Card: card}
}

func testPlaybook(id string, entries ...board.PlaybookEntry) *board.Playbook {
	return &board.Playbook{
		ID:      id,
		Title:   id,
		Created: time.Now(),
		Updated: time.Now(),
		Entries: entries,
	}
}

func TestPlaybookMembership_GetCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	member, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "member", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	loner, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "loner", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	svc.SetPlaybookLister(&fakePlaybookLister{playbooks: []*board.Playbook{
		testPlaybook("rollout", cardEntry("test-project", member.ID)),
		testPlaybook("cleanup",
			board.PlaybookEntry{ID: "m1", Type: "manual", Text: "flip the switch"},
			cardEntry("test-project", member.ID),
			cardEntry("other-project", loner.ID),
		),
	}})

	got, err := svc.GetCard(ctx, "test-project", member.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"rollout", "cleanup"}, got.InPlaybooks,
		"member card lists its playbooks in lister order")

	got, err = svc.GetCard(ctx, "test-project", loner.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks,
		"same card ID in another project must not count as membership")
}

func TestPlaybookMembership_ListCards(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	member, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "member", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "loner", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	svc.SetPlaybookLister(&fakePlaybookLister{playbooks: []*board.Playbook{
		testPlaybook("rollout", cardEntry("test-project", member.ID)),
	}})

	cards, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)

	byID := map[string][]string{}
	for _, c := range cards {
		byID[c.ID] = c.InPlaybooks
	}

	assert.Equal(t, []string{"rollout"}, byID[member.ID])

	for id, pbs := range byID {
		if id != member.ID {
			assert.Empty(t, pbs, "non-member %s must have no playbooks", id)
		}
	}
}

func TestPlaybookMembership_BestEffort(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "card", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	// No lister wired: reads still work.
	got, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks)

	// Lister failure: reads still work, membership just stays empty.
	svc.SetPlaybookLister(&fakePlaybookLister{err: errors.New("boom")})
	got, err = svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks)
}

// TestCardReopenDuringPlaybookRun drives every CardService path that takes a
// caller's state: a card leaving done or not_planned while a playbook holding
// it has a running or waiting run would give the runner a new frontier
// behind its live worker.
func TestCardReopenDuringPlaybookRun(t *testing.T) {
	writers := []struct {
		name  string
		write func(ctx context.Context, svc *CardService, id, state string) error
	}{
		{"TransitionTo", func(ctx context.Context, svc *CardService, id, state string) error {
			_, err := svc.TransitionTo(ctx, "test-project", id, state)

			return err
		}},
		{"PatchCard", func(ctx context.Context, svc *CardService, id, state string) error {
			_, err := svc.PatchCard(ctx, "test-project", id, PatchCardInput{State: &state})

			return err
		}},
	}

	run := func(status string) *board.PlaybookRun { return &board.PlaybookRun{Status: status, Entry: "e-other"} }
	errList := errors.New("playbook store unavailable")

	tests := []struct {
		name         string
		from, to     string
		run          *board.PlaybookRun
		entryProject string   // project of the playbook's entry for the card's id
		allow        []string // extra transition from -> allow[0], when set
		noLister     bool
		listErr      error
		wantErr      error
	}{
		{name: "done to todo while running is refused", from: "done", to: "todo", run: run(board.RunStatusRunning), wantErr: ErrCardReopenDuringRun},
		{name: "done to todo while waiting is refused", from: "done", to: "todo", run: run(board.RunStatusWaiting), wantErr: ErrCardReopenDuringRun},
		{name: "not_planned to todo while running is refused", from: "not_planned", to: "todo", run: run(board.RunStatusRunning), wantErr: ErrCardReopenDuringRun},
		{name: "not_planned to todo while waiting is refused", from: "not_planned", to: "todo", run: run(board.RunStatusWaiting), wantErr: ErrCardReopenDuringRun},
		{name: "done to todo after a stop is allowed", from: "done", to: "todo", run: run(board.RunStatusStopped)},
		{name: "done to todo after completion is allowed", from: "done", to: "todo", run: run(board.RunStatusCompleted)},
		{name: "done to todo with no run is allowed", from: "done", to: "todo"},
		{
			name: "done to todo for a card in no playbook is allowed", from: "done", to: "todo",
			run: run(board.RunStatusRunning), entryProject: "other-project",
		},
		{name: "in_progress to done while running is allowed", from: "in_progress", to: "done", run: run(board.RunStatusRunning)},
		{name: "todo to in_progress while running is allowed", from: "todo", to: "in_progress", run: run(board.RunStatusRunning)},
		{name: "todo to not_planned while running is allowed", from: "todo", to: "not_planned", run: run(board.RunStatusRunning)},
		{
			name: "done to not_planned while running is allowed", from: "done", to: "not_planned",
			run: run(board.RunStatusRunning), allow: []string{"not_planned"},
		},
		{name: "done to todo with no lister is allowed", from: "done", to: "todo", run: run(board.RunStatusRunning), noLister: true},
		{
			name: "done to todo when the playbooks cannot be listed is refused", from: "done", to: "todo",
			run: run(board.RunStatusRunning), listErr: errList, wantErr: errList,
		},
	}

	for _, tt := range tests {
		for _, w := range writers {
			t.Run(tt.name+"/"+w.name, func(t *testing.T) {
				svc, _, cleanup := setupTest(t)
				defer cleanup()

				ctx := context.Background()

				card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "entry", Type: "task", Priority: "low"})
				require.NoError(t, err)

				seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
				require.NoError(t, err)

				seeded.State = tt.from
				require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

				if tt.allow != nil {
					cfg, err := svc.store.GetProject(ctx, "test-project")
					require.NoError(t, err)

					cfg.Transitions[tt.from] = append(cfg.Transitions[tt.from], tt.allow...)
					require.NoError(t, svc.store.SaveProject(ctx, cfg))
					svc.ClearCaches()
				}

				if !tt.noLister {
					project := tt.entryProject
					if project == "" {
						project = "test-project"
					}

					p := testPlaybook("rollout", cardEntry(project, card.ID))
					p.Title = "Rollout"
					p.Run = tt.run
					svc.SetPlaybookLister(&fakePlaybookLister{playbooks: []*board.Playbook{p}, err: tt.listErr})
				}

				err = w.write(ctx, svc, card.ID, tt.to)

				got, getErr := svc.store.GetCard(ctx, "test-project", card.ID)
				require.NoError(t, getErr)

				if tt.wantErr == nil {
					require.NoError(t, err)
					assert.Equal(t, tt.to, got.State)

					return
				}

				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.from, got.State, "a refused write leaves the card where it was")

				if errors.Is(tt.wantErr, ErrCardReopenDuringRun) {
					reopen, ok := errors.AsType[*CardReopenError](err)
					require.True(t, ok)
					assert.Equal(t, "Rollout", reopen.Playbook)
				}
			})
		}
	}
}
