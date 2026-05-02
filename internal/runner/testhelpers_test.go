package runner_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/runner"
)

type fakeCardGetter struct {
	mu    sync.RWMutex
	cards map[string]*board.Card
}

func (f *fakeCardGetter) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	c, ok := f.cards[project+"/"+id]
	if !ok {
		return nil, errors.New("not found")
	}

	cp := *c

	return &cp, nil
}

// setAgent updates AssignedAgent under the fake's lock, so tests can safely
// mutate card state while the subscriber may be reading it concurrently.
func (f *fakeCardGetter) setAgent(project, id, agent string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if c, ok := f.cards[project+"/"+id]; ok {
		c.AssignedAgent = agent
	}
}

type fakeClient struct {
	mu         sync.Mutex
	killCalls  []runner.KillPayload
	killErr    error
	listResult []runner.ContainerInfo
	listErr    error
}

func (f *fakeClient) Kill(_ context.Context, p runner.KillPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.killCalls = append(f.killCalls, p)

	return f.killErr
}

// ListContainers is the authoritative runner-side input the sweep consults
// on every tick. Tests set listResult to configure the container snapshot
// the sweep sees; listErr covers the runner-unreachable path so the sweep
// can skip ticks without crashing.
func (f *fakeClient) ListContainers(_ context.Context) ([]runner.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	out := make([]runner.ContainerInfo, len(f.listResult))
	copy(out, f.listResult)

	return out, nil
}

func (f *fakeClient) KillCalls() []runner.KillPayload {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]runner.KillPayload, len(f.killCalls))
	copy(out, f.killCalls)

	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForKillCalls(t *testing.T, fc *fakeClient, want int) {
	t.Helper()

	require.Eventually(t, func() bool {
		return len(fc.KillCalls()) >= want
	}, 2*time.Second, 5*time.Millisecond, "expected >= %d kill calls", want)
}

func assertNoKillCall(t *testing.T, fc *fakeClient) {
	t.Helper()

	// Genuine wall-clock wait: the subscriber dispatches asynchronously via
	// the events bus, so we need a short window to confirm no call *would*
	// have fired.
	time.Sleep(100 * time.Millisecond)

	assert.Empty(t, fc.KillCalls(), "expected no kill calls")
}
