package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// fakeOutcomeWriter is an in-test double for OutcomeWriter that records every
// row it receives without validation, so tests can assert exactly what the
// handler passed down.
type fakeOutcomeWriter struct{ rows []sqlite.ModelOutcome }

func (f *fakeOutcomeWriter) RecordModelOutcomes(_ context.Context, rows []sqlite.ModelOutcome) error {
	f.rows = append(f.rows, rows...)

	return nil
}

// setupOutcomeMCP mirrors setupMCP/setupMCPWithCosts (server_test.go) but wires
// a caller-supplied OutcomeWriter into ServerConfig — setupMCP has no Outcomes
// hook, and this file must not modify server_test.go.
func setupOutcomeMCP(t *testing.T, w OutcomeWriter) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectConfig()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	server := NewServer(ServerConfig{
		Service:  svc,
		Outcomes: w,
	})

	ctx, cancel := context.WithCancel(context.Background())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = session.Close()

		cancel()
	})

	return &testEnv{
		session:   session,
		svc:       svc,
		store:     store,
		boardsDir: boardsDir,
		cancel:    cancel,
	}
}

func TestReportModelOutcomeTool(t *testing.T) {
	t.Run("happy path records rows with project and card from args", func(t *testing.T) {
		w := &fakeOutcomeWriter{}
		env := setupOutcomeMCP(t, w)

		createTestCard(t, env, "Best-of-N run", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "cmx-agent-cm-1",
		})

		result := callTool(t, env, "report_model_outcome", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "cmx-agent-cm-1",
			"outcomes": []map[string]any{
				{
					"model":        "vendor/coder-a",
					"result":       "win",
					"verify_pass":  true,
					"cost_usd":     1.25,
					"n_candidates": 2,
					"judge_model":  "vendor/judge",
				},
				{
					"model":        "vendor/coder-b",
					"result":       "loss",
					"verify_pass":  true,
					"cost_usd":     0.9,
					"n_candidates": 2,
					"judge_model":  "vendor/judge",
				},
			},
		})
		require.False(t, result.IsError, "report_model_outcome should not error")

		require.Len(t, w.rows, 2, "store should receive one row per candidate")

		assert.Equal(t, "test-project", w.rows[0].Project)
		assert.Equal(t, "TEST-001", w.rows[0].CardID)
		assert.Equal(t, "vendor/coder-a", w.rows[0].Model)
		assert.Equal(t, "coder", w.rows[0].Role)
		assert.Equal(t, "win", w.rows[0].Result)
		assert.True(t, w.rows[0].VerifyPass)
		assert.InDelta(t, 1.25, w.rows[0].CostUSD, 1e-9)
		assert.Equal(t, 2, w.rows[0].NCandidates)
		assert.Equal(t, "vendor/judge", w.rows[0].JudgeModel)

		assert.Equal(t, "test-project", w.rows[1].Project)
		assert.Equal(t, "TEST-001", w.rows[1].CardID)
		assert.Equal(t, "vendor/coder-b", w.rows[1].Model)
		assert.Equal(t, "loss", w.rows[1].Result)
	})

	t.Run("claimed by someone else is rejected", func(t *testing.T) {
		w := &fakeOutcomeWriter{}
		env := setupOutcomeMCP(t, w)

		createTestCard(t, env, "Best-of-N run", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "cmx-agent-cm-1",
		})

		result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "report_model_outcome",
			Arguments: map[string]any{
				"project":  "test-project",
				"card_id":  "TEST-001",
				"agent_id": "attacker",
				"outcomes": []map[string]any{
					{"model": "vendor/coder-a", "result": "win", "verify_pass": true, "cost_usd": 1.0, "n_candidates": 2},
				},
			},
		})
		if err != nil {
			assert.Contains(t, err.Error(), "claimed by")

			return
		}

		require.True(t, result.IsError, "mismatched agent should error")
		textContent, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "claimed by")
		assert.Empty(t, w.rows, "store must not be written when ownership check fails")
	})

	t.Run("unclaimed card is rejected", func(t *testing.T) {
		w := &fakeOutcomeWriter{}
		env := setupOutcomeMCP(t, w)

		createTestCard(t, env, "Best-of-N run", "task", "medium")

		result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "report_model_outcome",
			Arguments: map[string]any{
				"project":  "test-project",
				"card_id":  "TEST-001",
				"agent_id": "cmx-agent-cm-1",
				"outcomes": []map[string]any{
					{"model": "vendor/coder-a", "result": "win", "verify_pass": true, "cost_usd": 1.0, "n_candidates": 2},
				},
			},
		})
		if err != nil {
			assert.Contains(t, err.Error(), "requires an active claim")

			return
		}

		require.True(t, result.IsError, "unclaimed card should error")
		textContent, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "requires an active claim")
		assert.Empty(t, w.rows, "store must not be written for an unclaimed card")
	})

	// Case 4 needs real store validation (RecordModelOutcomes rejects unknown
	// result values) — fakeOutcomeWriter accepts everything, so this uses the
	// real *sqlite.Store against a t.TempDir ops.db instead of the fake.
	t.Run("invalid result value surfaces store validation", func(t *testing.T) {
		st, err := sqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
		require.NoError(t, err)

		defer st.Close()

		env := setupOutcomeMCP(t, st)

		createTestCard(t, env, "Best-of-N run", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "cmx-agent-cm-1",
		})

		result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "report_model_outcome",
			Arguments: map[string]any{
				"project":  "test-project",
				"card_id":  "TEST-001",
				"agent_id": "cmx-agent-cm-1",
				"outcomes": []map[string]any{
					{"model": "vendor/coder-a", "result": "maybe", "verify_pass": true, "cost_usd": 1.0, "n_candidates": 2},
				},
			},
		})
		if err != nil {
			assert.Contains(t, err.Error(), "invalid result")

			return
		}

		require.True(t, result.IsError, "invalid result value should error")
		textContent, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "invalid result")
	})
}
