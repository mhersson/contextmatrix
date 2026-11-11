package backend_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/events"
)

// fakeRunCloser honours the started-at bound: started maps "project/id" to the
// open run's start; a card absent from it is treated as started at the epoch.
type fakeRunCloser struct {
	mu      sync.Mutex
	closed  []string
	started map[string]time.Time
}

func (f *fakeRunCloser) CloseCardRun(_ context.Context, project, cardID string, startedNotAfter, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := project + "/" + cardID
	if st, ok := f.started[key]; ok && st.After(startedNotAfter) {
		return nil
	}

	f.closed = append(f.closed, key)

	return nil
}

func (f *fakeRunCloser) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.closed...)
}

func TestCardRunCloser(t *testing.T) {
	tests := []struct {
		name  string
		event events.Event
		want  bool
	}{
		{"failed", events.Event{Type: events.WorkerFailed}, true},
		{"killed", events.Event{Type: events.WorkerKilled}, true},
		{"parked", events.Event{Type: events.WorkerParked}, true},
		{"completed", events.Event{Type: events.CardUpdated, Data: map[string]any{"worker_status": "completed"}}, true},
		{"started", events.Event{Type: events.WorkerStarted, Data: map[string]any{"worker_status": "running"}}, false},
		{"plain card update", events.Event{Type: events.CardUpdated}, false},
		{"other worker status", events.Event{Type: events.CardUpdated, Data: map[string]any{"worker_status": "queued"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := events.NewBus()
			rc := &fakeRunCloser{}
			backend.StartCardRunCloser(ctx, bus, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))

			ev := tt.event
			ev.Project, ev.CardID = "p", "P-001"
			bus.Publish(ev)
			// A sentinel the closer always acts on proves the earlier event was seen.
			bus.Publish(events.Event{Type: events.WorkerFailed, Project: "p", CardID: "SENTINEL"})

			assert.Eventually(t, func() bool {
				return containsStr(rc.snapshot(), "p/SENTINEL")
			}, 2*time.Second, 5*time.Millisecond)
			assert.Equal(t, tt.want, containsStr(rc.snapshot(), "p/P-001"))
		})
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}

	return false
}

func TestCardRunCloser_EventOlderThanRerunLeavesItOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	bus := events.NewBus()
	rc := &fakeRunCloser{started: map[string]time.Time{
		"p/P-001": now,                 // rerun started after the stale event
		"p/P-002": now.Add(-time.Hour), // run that the event does end
	}}
	backend.StartCardRunCloser(ctx, bus, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	bus.Publish(events.Event{Type: events.WorkerFailed, Project: "p", CardID: "P-001", Timestamp: now.Add(-time.Minute)})
	bus.Publish(events.Event{Type: events.WorkerFailed, Project: "p", CardID: "P-002", Timestamp: now})

	assert.Eventually(t, func() bool {
		return containsStr(rc.snapshot(), "p/P-002")
	}, 2*time.Second, 5*time.Millisecond)
	assert.False(t, containsStr(rc.snapshot(), "p/P-001"))
}
