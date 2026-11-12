package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type fakeParkResets struct {
	mu        sync.Mutex
	rows      map[string]time.Time
	listCalls int
}

func newFakeParkResets() *fakeParkResets {
	return &fakeParkResets{rows: map[string]time.Time{}}
}

func (f *fakeParkResets) PutParkReset(_ context.Context, project, cardID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rows[project+"/"+cardID] = at

	return nil
}

func (f *fakeParkResets) ClearParkReset(_ context.Context, project, cardID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.rows, project+"/"+cardID)

	return nil
}

func (f *fakeParkResets) ParkResets(_ context.Context, project string) (map[string]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.listCalls++

	out := map[string]time.Time{}

	for k, v := range f.rows {
		if len(k) > len(project)+1 && k[:len(project)+1] == project+"/" {
			out[k[len(project)+1:]] = v
		}
	}

	return out, nil
}

func (f *fakeParkResets) has(project, cardID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, ok := f.rows[project+"/"+cardID]

	return ok
}

func claimedCard(t *testing.T, svc *CardService) string {
	t.Helper()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "limit", Type: "task", Priority: "low"})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	return card.ID
}

func TestReportParked_StoresResetTime(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	fake := newFakeParkResets()
	svc.SetParkResetStore(fake)

	id := claimedCard(t, svc)
	resets := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	got, err := svc.ReportParked(ctx, "test-project", id, "agent-1", "usage limit", &resets)
	require.NoError(t, err)
	require.NotNil(t, got.ParkResetsAt)
	assert.True(t, resets.Equal(*got.ParkResetsAt))

	var parked *events.Event

	for len(ch) > 0 {
		if ev := <-ch; ev.Type == events.WorkerParked {
			parked = &ev
		}
	}

	require.NotNil(t, parked)
	assert.Equal(t, "2026-11-11T18:00:00Z", parked.Data["resets_at"])

	fetched, err := svc.GetCard(ctx, "test-project", id)
	require.NoError(t, err)
	require.NotNil(t, fetched.ParkResetsAt)
	assert.True(t, resets.Equal(*fetched.ParkResetsAt))

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NotNil(t, listed[0].ParkResetsAt)
	assert.True(t, resets.Equal(*listed[0].ParkResetsAt))

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Items[0].ParkResetsAt)

	fake.mu.Lock()
	fake.listCalls = 0
	fake.mu.Unlock()

	_, err = svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Equal(t, 1, fake.listCalls, "one lookup per list, not per card")
}

func TestReportParked_NoResetTime(t *testing.T) {
	zero := time.Time{}

	for name, resets := range map[string]*time.Time{"nil": nil, "zero": &zero} {
		t.Run(name, func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			ctx := context.Background()
			fake := newFakeParkResets()
			svc.SetParkResetStore(fake)

			id := claimedCard(t, svc)

			ch, unsub := svc.bus.Subscribe()
			defer unsub()

			got, err := svc.ReportParked(ctx, "test-project", id, "agent-1", "review parked", resets)
			require.NoError(t, err)
			assert.Equal(t, "parked", got.WorkerStatus)
			assert.Nil(t, got.ParkResetsAt)
			assert.False(t, fake.has("test-project", id))

			for len(ch) > 0 {
				if ev := <-ch; ev.Type == events.WorkerParked {
					assert.NotContains(t, ev.Data, "resets_at")
				}
			}
		})
	}
}

func TestMarkQueued_ClearsResetTime(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	fake := newFakeParkResets()
	svc.SetParkResetStore(fake)

	id := claimedCard(t, svc)
	resets := time.Now().Add(time.Hour)

	_, err := svc.ReportParked(ctx, "test-project", id, "agent-1", "usage limit", &resets)
	require.NoError(t, err)
	require.True(t, fake.has("test-project", id))

	queued, err := svc.MarkQueued(ctx, "test-project", id, "resume")
	require.NoError(t, err)
	assert.Nil(t, queued.ParkResetsAt)
	assert.False(t, fake.has("test-project", id))
}

func TestParkResetsAt_HiddenWhenNotParked(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	fake := newFakeParkResets()
	svc.SetParkResetStore(fake)

	id := claimedCard(t, svc)
	require.NoError(t, fake.PutParkReset(ctx, "test-project", id, time.Now().Add(time.Hour)))

	got, err := svc.GetCard(ctx, "test-project", id)
	require.NoError(t, err)
	assert.Nil(t, got.ParkResetsAt)

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].ParkResetsAt)
}

func TestReportParked_NoResetTimeDropsStaleRow(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	fake := newFakeParkResets()
	svc.SetParkResetStore(fake)

	id := claimedCard(t, svc)
	resets := time.Now().Add(time.Hour)

	_, err := svc.ReportParked(ctx, "test-project", id, "agent-1", "usage limit", &resets)
	require.NoError(t, err)

	// Leave parked without queueing: the row survives.
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", id, "failed", "crashed")
	require.NoError(t, err)
	require.True(t, fake.has("test-project", id))

	_, err = svc.ClaimCard(ctx, "test-project", id, "agent-1")
	require.NoError(t, err)

	got, err := svc.ReportParked(ctx, "test-project", id, "agent-1", "review parked", nil)
	require.NoError(t, err)
	assert.Nil(t, got.ParkResetsAt)

	fetched, err := svc.GetCard(ctx, "test-project", id)
	require.NoError(t, err)
	assert.Equal(t, "parked", fetched.WorkerStatus)
	assert.Nil(t, fetched.ParkResetsAt)
}

func TestParkResets_OnlyParkedCardsEnriched(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	fake := newFakeParkResets()
	svc.SetParkResetStore(fake)

	parkedID := claimedCard(t, svc)
	resets := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)

	_, err := svc.ReportParked(ctx, "test-project", parkedID, "agent-1", "usage limit", &resets)
	require.NoError(t, err)

	other, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "other", Type: "task", Priority: "low"})
	require.NoError(t, err)
	require.NoError(t, fake.PutParkReset(ctx, "test-project", other.ID, resets))

	got, err := svc.GetCard(ctx, "test-project", other.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ParkResetsAt)

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 2)

	for _, c := range listed {
		if c.ID == parkedID {
			require.NotNil(t, c.ParkResetsAt)
		} else {
			assert.Nil(t, c.ParkResetsAt)
		}
	}
}
