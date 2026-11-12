package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
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

func TestReportParkedTool_RecordsRejectedWindow(t *testing.T) {
	reset := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		args map[string]any
		want [][]protocol.SubscriptionUsage
	}{
		{
			name: "window with resets_at records rejected",
			args: map[string]any{"window": "five_hour", "resets_at": reset.Format(time.RFC3339)},
			want: [][]protocol.SubscriptionUsage{{
				{Window: "five_hour", Utilization: 1, Status: "rejected", ResetsAt: reset},
			}},
		},
		{
			name: "resets_at without window records nothing",
			args: map[string]any{"resets_at": reset.Format(time.RFC3339)},
		},
		{
			name: "window without resets_at records nothing",
			args: map[string]any{"window": "five_hour"},
		},
		{
			name: "window with zero resets_at records nothing",
			args: map[string]any{"window": "five_hour", "resets_at": "0001-01-01T00:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordedUsage{}
			env := setupMCP(t, func(c *ServerConfig) { c.SubscriptionUsage = rec })
			env.svc.SetParkResetStore(&parkResetRecorder{rows: map[string]time.Time{}})

			createTestCard(t, env, "Limit", "task", "medium")
			callTool(t, env, "claim_card", map[string]any{
				"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
			})

			args := map[string]any{
				"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
				"reason": "usage limit reached",
			}
			for k, v := range tt.args {
				args[k] = v
			}

			result := callTool(t, env, "report_parked", args)
			require.False(t, result.IsError)
			assert.Equal(t, tt.want, rec.got)
		})
	}

	t.Run("a failed park records nothing", func(t *testing.T) {
		rec := &recordedUsage{}
		env := setupMCP(t, func(c *ServerConfig) { c.SubscriptionUsage = rec })

		createTestCard(t, env, "Limit", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result := callTool(t, env, "report_parked", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-B",
			"reason": "usage limit reached", "window": "five_hour", "resets_at": reset.Format(time.RFC3339),
		})
		require.True(t, result.IsError)
		assert.Empty(t, rec.got)
	})
}
