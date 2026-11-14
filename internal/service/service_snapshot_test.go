package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

type fakeEditSnapshots struct {
	mu    sync.Mutex
	snaps []board.EditSnapshot
	err   error
}

func (f *fakeEditSnapshots) RefreshEditSnapshot(_ context.Context, _, _ string, snap board.EditSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.snaps = append(f.snaps, snap)

	return f.err
}

func (f *fakeEditSnapshots) calls() []board.EditSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]board.EditSnapshot(nil), f.snaps...)
}

func snapshotCard(t *testing.T, svc *CardService, claimant string) string {
	t.Helper()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "snap", Type: "task", Priority: "low"})
	require.NoError(t, err)

	if claimant != "" {
		_, err = svc.ClaimCard(ctx, "test-project", card.ID, claimant)
		require.NoError(t, err)
	}

	return card.ID
}

func planPatch(agentID string) PatchCardInput {
	return PatchCardInput{AgentID: agentID, UpsertSection: &SectionPatch{Heading: "Plan", Content: "step one"}}
}

func TestWorkerWriteRefreshesEditSnapshot(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	fake := &fakeEditSnapshots{}
	svc.SetEditSnapshotStore(fake)

	id := snapshotCard(t, svc, "agent:w1")

	got, err := svc.PatchCard(context.Background(), "test-project", id, planPatch("agent:w1"))
	require.NoError(t, err)

	calls := fake.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, board.TakeEditSnapshot(got), calls[0])
}

func TestWorkerWriteSurvivesSnapshotStoreError(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	svc.SetEditSnapshotStore(&fakeEditSnapshots{err: errors.New("disk full")})

	id := snapshotCard(t, svc, "agent:w1")

	_, err := svc.PatchCard(context.Background(), "test-project", id, planPatch("agent:w1"))
	require.NoError(t, err)
}

func TestHumanClaimHolderWriteDoesNotRefresh(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	fake := &fakeEditSnapshots{}
	svc.SetEditSnapshotStore(fake)

	id := snapshotCard(t, svc, "human:alice")

	_, err := svc.PatchCard(context.Background(), "test-project", id, planPatch("human:alice"))
	require.NoError(t, err)
	assert.Empty(t, fake.calls())
}

func TestUnclaimedWriteDoesNotRefresh(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	fake := &fakeEditSnapshots{}
	svc.SetEditSnapshotStore(fake)

	id := snapshotCard(t, svc, "")

	_, err := svc.PatchCard(context.Background(), "test-project", id, planPatch("human:web"))
	require.NoError(t, err)
	assert.Empty(t, fake.calls())
}

func TestSystemWriteDoesNotRefresh(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	fake := &fakeEditSnapshots{}
	svc.SetEditSnapshotStore(fake)

	id := snapshotCard(t, svc, "agent:w1")

	_, err := svc.ForcePlaybookSettings(context.Background(), "test-project", id, "pb", "main", "")
	require.NoError(t, err)
	assert.Empty(t, fake.calls())
}
