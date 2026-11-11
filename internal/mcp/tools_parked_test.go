package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestReportParked(t *testing.T) {
	t.Run("owning agent parks the card", func(t *testing.T) {
		env := setupMCP(t)
		createTestCard(t, env, "Park me", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result := callTool(t, env, "report_parked", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
			"reason":   "review parked: attempts cap exhausted without approval",
		})
		require.False(t, result.IsError)

		getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
		require.False(t, getResult.IsError)

		var card board.Card
		unmarshalResult(t, getResult, &card)
		assert.Equal(t, "parked", card.WorkerStatus)
	})

	t.Run("non-owning agent is rejected", func(t *testing.T) {
		env := setupMCP(t)
		createTestCard(t, env, "Park me not", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result, err := callToolRaw(t, env, "report_parked", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-B",
			"reason":   "review parked: attempts cap exhausted without approval",
		})
		require.True(t, resultIsError(result, err))
		assert.Contains(t, errorText(result, err), "agent")
	})
}

type parkResetRecorder struct {
	rows map[string]time.Time
}

func (r *parkResetRecorder) PutParkReset(_ context.Context, project, cardID string, at time.Time) error {
	r.rows[project+"/"+cardID] = at

	return nil
}

func (r *parkResetRecorder) ClearParkReset(_ context.Context, project, cardID string) error {
	delete(r.rows, project+"/"+cardID)

	return nil
}

func (r *parkResetRecorder) ParkResets(_ context.Context, project string) (map[string]time.Time, error) {
	out := map[string]time.Time{}

	for k, v := range r.rows {
		if id, ok := strings.CutPrefix(k, project+"/"); ok {
			out[id] = v
		}
	}

	return out, nil
}

func TestReportParkedTool_ResetsAt(t *testing.T) {
	park := func(t *testing.T, resetsAt string) (*testEnv, *parkResetRecorder, *mcp.CallToolResult) {
		t.Helper()

		env := setupMCP(t)
		rec := &parkResetRecorder{rows: map[string]time.Time{}}
		env.svc.SetParkResetStore(rec)

		createTestCard(t, env, "Limit", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result := callTool(t, env, "report_parked", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
			"reason": "usage limit reached", "resets_at": resetsAt,
		})

		return env, rec, result
	}

	t.Run("rejects a value that is not RFC 3339", func(t *testing.T) {
		env, rec, result := park(t, "tomorrow")
		require.True(t, result.IsError)
		assert.Contains(t, resultText(result), "resets_at")
		assert.Empty(t, rec.rows)

		card, err := env.svc.GetCard(context.Background(), "test-project", "TEST-001")
		require.NoError(t, err)
		assert.NotEqual(t, "parked", card.WorkerStatus)
	})

	t.Run("accepts a past time", func(t *testing.T) {
		_, rec, result := park(t, "2020-01-02T03:04:05Z")
		require.False(t, result.IsError)
		require.Len(t, rec.rows, 1)
		assert.True(t, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC).Equal(rec.rows["test-project/TEST-001"]))
	})

	t.Run("a zero time parks without a reset row", func(t *testing.T) {
		env, rec, result := park(t, "0001-01-01T00:00:00Z")
		require.False(t, result.IsError)
		assert.Empty(t, rec.rows)

		card, err := env.svc.GetCard(context.Background(), "test-project", "TEST-001")
		require.NoError(t, err)
		assert.Equal(t, "parked", card.WorkerStatus)
		assert.Nil(t, card.ParkResetsAt)
	})
}
