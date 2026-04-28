package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	intmcp "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// TestRunnerFoundationsEndToEnd is a single end-to-end smoke test that wires
// real storage, service, event bus, runner buffer, API router, and MCP server
// against a fresh boards directory. It exercises the runner-orchestration
// foundations introduced by Plan 1: the new ProjectConfig schema (Repos +
// JiraProjectKey), per-card runner event buffer with SSE/poll endpoints,
// log-event fan-out, and the MCP get_project_kb tool reading _kb/ markdown.
func TestRunnerFoundationsEndToEnd(t *testing.T) {
	// 1. Set up a fresh boards dir with a multi-repo project + KB files.
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "p1", "tasks"), 0o755))

	cfg := &board.ProjectConfig{
		Name:           "p1",
		Prefix:         "P",
		NextID:         1,
		JiraProjectKey: "PAY",
		Repos: []board.RepoSpec{
			{Slug: "r1", URL: "https://example.com/r1.git", Description: "primary repo"},
			{Slug: "r2", URL: "https://example.com/r2.git"},
		},
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"medium"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
	require.NoError(t, board.SaveProjectConfig(filepath.Join(boardsDir, "p1"), cfg))

	// KB tree under the boards root: per-repo + per-jira-project + per-project.
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "_kb", "repos"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "_kb", "jira-projects"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "p1", "kb"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(boardsDir, "_kb", "repos", "r1.md"),
		[]byte("# r1 kb\nrepo notes for r1"), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(boardsDir, "_kb", "jira-projects", "PAY.md"),
		[]byte("# PAY jira context\npayments domain notes"), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(boardsDir, "p1", "kb", "project.md"),
		[]byte("# p1 project notes\nephemeral project notes"), 0o644,
	))

	// 2. Wire real services.
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	// Seed an initial commit so HEAD exists and CommitFile() succeeds.
	require.NoError(t, gitMgr.CommitFile(context.Background(), "p1/.board.yaml", "init: seed boards repo"))

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	runnerBuf := events.NewRunnerEventBuffer(100, time.Hour)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	// 3. Build the API router with the runner buffer wired in.
	router := NewRouter(RouterConfig{
		Service:           svc,
		Bus:               bus,
		RunnerEventBuffer: runnerBuf,
	})

	apiSrv := httptest.NewServer(router)
	defer apiSrv.Close()

	// 4. Build the MCP server (in-memory transport).
	mcpServer := intmcp.NewServer(svc, "")

	// t.Context() is auto-cancelled when the test completes, replacing the
	// previous explicit context.WithCancel/defer cancel pattern.
	ctx := t.Context()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = mcpServer.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.1.0"}, nil)

	mcpSession, err := mcpClient.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	defer func() { _ = mcpSession.Close() }()

	// 5. Create a card via the REST API.
	createBody, err := json.Marshal(createCardRequest{
		Title:    "E2E test card",
		Type:     "task",
		Priority: "medium",
		Labels:   []string{"smoke"},
	})
	require.NoError(t, err)

	resp, err := http.Post(apiSrv.URL+"/api/projects/p1/cards", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)

	var created board.Card

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	closeBody(t, resp.Body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	cardID := created.ID
	require.NotEmpty(t, cardID)
	require.Equal(t, "p1", created.Project)
	require.Equal(t, []string{"smoke"}, created.Labels)

	// Round-trip the card via GET to confirm it persisted.
	getResp, err := http.Get(apiSrv.URL + "/api/projects/p1/cards/" + cardID)
	require.NoError(t, err)

	var fetched board.Card

	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&fetched))
	closeBody(t, getResp.Body)
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	require.Equal(t, cardID, fetched.ID)
	require.Equal(t, "E2E test card", fetched.Title)

	// And confirm the project itself round-trips with the new schema fields.
	projResp, err := http.Get(apiSrv.URL + "/api/projects/p1")
	require.NoError(t, err)

	var projOut board.ProjectConfig

	require.NoError(t, json.NewDecoder(projResp.Body).Decode(&projOut))
	closeBody(t, projResp.Body)
	require.Equal(t, http.StatusOK, projResp.StatusCode)
	require.Equal(t, "PAY", projOut.JiraProjectKey)
	require.Len(t, projOut.Repos, 2)
	require.Equal(t, "r1", projOut.Repos[0].Slug)
	require.Equal(t, "https://example.com/r1.git", projOut.Repos[0].URL)
	require.Equal(t, "primary repo", projOut.Repos[0].Description)
	require.Equal(t, "r2", projOut.Repos[1].Slug)

	// 6. Subscribe to /api/runner/events SSE BEFORE appending so we see live fan-out.
	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiSrv.URL+"/api/runner/events?card_id="+cardID, nil)
	require.NoError(t, err)

	sseResp, err := http.DefaultClient.Do(sseReq)
	require.NoError(t, err)

	defer closeBody(t, sseResp.Body)

	require.Equal(t, http.StatusOK, sseResp.StatusCode)

	// Drain in a goroutine so we don't block on bufio.Scanner reads forever.
	sawData := make(chan string, 1)
	sseErr := make(chan error, 1)

	go func() {
		sc := bufio.NewScanner(sseResp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				sawData <- line

				return
			}
		}

		if scanErr := sc.Err(); scanErr != nil {
			sseErr <- scanErr
		}
	}()

	// Give the SSE handler a beat to register the subscription. The handler
	// subscribes synchronously on the request goroutine before returning to
	// the read loop, but the client-side body reader doesn't get scheduled
	// instantly.
	time.Sleep(100 * time.Millisecond)

	// 7. POST a runner log event. /api/runner/log-event publishes to the
	// global event Bus (web-UI fan-out); it does NOT go through the per-card
	// RunnerEventBuffer. Verifying it returns 204 confirms the endpoint is
	// wired and accepting runner-side posts.
	logBody, err := json.Marshal(map[string]any{
		"card_id": cardID,
		"kind":    "tool_call",
		"text":    "Read /workspace/README.md",
	})
	require.NoError(t, err)

	logResp, err := http.Post(apiSrv.URL+"/api/runner/log-event", "application/json", bytes.NewReader(logBody))
	require.NoError(t, err)
	closeBody(t, logResp.Body)
	require.Equal(t, http.StatusNoContent, logResp.StatusCode)

	// 8. Append directly to the runner buffer to drive the SSE endpoint that
	// the runner control channel reads from. The buffer is the live wire for
	// /api/runner/events; writes here fan out to the SSE subscriber above.
	runnerBuf.Append(cardID, events.RunnerEvent{Type: "chat_input", Data: "from e2e"})

	select {
	case line := <-sawData:
		require.Contains(t, line, "from e2e")
		require.Contains(t, line, "chat_input")
	case err := <-sseErr:
		t.Fatalf("SSE scan error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive SSE data within 2s")
	}

	// 9. Poll fallback round-trip on the same buffer.
	pollResp, err := http.Get(apiSrv.URL + "/api/runner/events?card_id=" + cardID + "&since=0")
	require.NoError(t, err)

	pollBody, err := io.ReadAll(pollResp.Body)
	require.NoError(t, err)
	closeBody(t, pollResp.Body)
	require.Equal(t, http.StatusOK, pollResp.StatusCode)
	require.Equal(t, "application/json", pollResp.Header.Get("Content-Type"))
	require.Contains(t, string(pollBody), "from e2e")
	require.Contains(t, string(pollBody), `"type":"chat_input"`)

	// 10. MCP get_project_kb returns the merged KB content.
	kbResult, err := mcpSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_project_kb",
		Arguments: map[string]any{
			"project": "p1",
		},
	})
	require.NoError(t, err)
	require.False(t, kbResult.IsError, "MCP get_project_kb should succeed")
	require.NotEmpty(t, kbResult.Content)

	textContent, ok := kbResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", kbResult.Content[0])

	var kbOut struct {
		Repos       map[string]string `json:"repos"`
		JiraProject string            `json:"jira_project"`
		Project     string            `json:"project"`
	}

	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &kbOut))
	require.Contains(t, kbOut.Repos, "r1")
	require.Contains(t, kbOut.Repos["r1"], "repo notes for r1")
	require.Contains(t, kbOut.JiraProject, "payments domain notes")
	require.Contains(t, kbOut.Project, "ephemeral project notes")

	// Filtered MCP get_project_kb narrows the per-repo content to one slug.
	kbFiltered, err := mcpSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_project_kb",
		Arguments: map[string]any{
			"project":   "p1",
			"repo_slug": "r1",
		},
	})
	require.NoError(t, err)
	require.False(t, kbFiltered.IsError)

	textFiltered, ok := kbFiltered.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var kbFilteredOut struct {
		Repos map[string]string `json:"repos"`
	}

	require.NoError(t, json.Unmarshal([]byte(textFiltered.Text), &kbFilteredOut))
	require.Contains(t, kbFilteredOut.Repos, "r1")
	require.NotContains(t, kbFilteredOut.Repos, "r2", "filter should narrow to r1 only")
}
