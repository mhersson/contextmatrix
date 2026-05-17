package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
)

// httptestNewRouterServer is a small helper that wires svc and bus into a
// test HTTP server using the default RouterConfig.
func httptestNewRouterServer(svc *service.CardService, bus *events.Bus) *httptest.Server {
	return httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
}

// appendActivity adds an ActivityEntry to a card via AddLogEntry. Because
// AddLogEntry only sets entry.Timestamp when it is zero, supplying a non-zero
// Timestamp preserves the caller's value exactly.
func appendActivity(t *testing.T, ctx context.Context, svc *service.CardService, cardID string, entry board.ActivityEntry) {
	t.Helper()
	require.NoError(t, svc.AddLogEntry(ctx, "test-project", cardID, entry))
}

func TestGetActivity_ReturnsChronologicalFlatten(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	server := httptestNewRouterServer(svc, bus)
	defer server.Close()

	// Create two cards with overlapping activity logs.
	a, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "first", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	b, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "second", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	// AddLogEntry preserves a non-zero Timestamp, so we can control order.
	appendActivity(t, ctx, svc, a.ID, board.ActivityEntry{
		Agent:     "claude-1",
		Action:    "claim",
		Timestamp: time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC),
		Message:   "claimed",
	})
	appendActivity(t, ctx, svc, b.ID, board.ActivityEntry{
		Agent:     "claude-2",
		Action:    "claim",
		Timestamp: time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC),
		Message:   "claimed",
	})
	appendActivity(t, ctx, svc, a.ID, board.ActivityEntry{
		Agent:     "claude-1",
		Action:    "progress",
		Timestamp: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		Message:   "wip",
	})

	resp, err := http.Get(server.URL + "/api/projects/test-project/activity?limit=10")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Entries []struct {
			Agent  string    `json:"agent"`
			Action string    `json:"action"`
			CardID string    `json:"card_id"`
			TS     time.Time `json:"ts"`
		} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Entries, 3)
	// Newest-first: progress (12:00) > claude-2 claim (11:00) > claude-1 claim (10:00).
	assert.Equal(t, "progress", body.Entries[0].Action)
	assert.Equal(t, "claim", body.Entries[1].Action)
	assert.Equal(t, "claude-2", body.Entries[1].Agent)
	assert.Equal(t, "claim", body.Entries[2].Action)
	assert.Equal(t, "claude-1", body.Entries[2].Agent)
}

func TestGetActivity_Limit(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	server := httptestNewRouterServer(svc, bus)
	defer server.Close()

	a, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "card", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		appendActivity(t, ctx, svc, a.ID, board.ActivityEntry{
			Agent:     "x",
			Action:    "progress",
			Timestamp: time.Date(2026, 5, 17, 10+i, 0, 0, 0, time.UTC),
			Message:   "step",
		})
	}

	resp, err := http.Get(server.URL + "/api/projects/test-project/activity?limit=3")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body.Entries, 3)
}
