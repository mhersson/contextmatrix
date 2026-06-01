package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
)

// newPermissionPromptSession spins up an MCP server with only the
// permission_prompt tool registered and returns a connected client session.
// Used to exercise the full SDK call path (handler → wire JSON → client) so
// tests can assert on what Claude Code actually sees, not just what the
// handler returns in-process.
func newPermissionPromptSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.1.0"}, nil)
	registerPermissionPrompt(server)

	ctx, cancel := context.WithCancel(context.Background())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = session.Close()

		cancel()
	})

	return session
}

// decodeDecision extracts the Claude Code SDK-facing decision JSON from the
// result's first text content block. This is the byte sequence the SDK
// parses to choose allow/deny, so it must round-trip via the explicit
// marshal path (not the SDK auto-marshal of the structured output).
func decodeDecision(t *testing.T, result *mcp.CallToolResult) permissionDecision {
	t.Helper()

	require.NotNil(t, result, "handler must return a non-nil CallToolResult so the wire JSON is deterministic")
	require.Len(t, result.Content, 1, "exactly one text content block expected")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content block must be *mcp.TextContent (got %T)", result.Content[0])

	var got permissionDecision
	require.NoError(t, json.Unmarshal([]byte(text.Text), &got))

	return got
}

func TestPermissionPrompt_AskUserQuestion_RedirectsToPlainText(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()

	// AskUserQuestion is denied everywhere with a plain-text redirect. The
	// runner no longer renders it as a clickable card, so the model must ask
	// its question as an ordinary message instead — the deny message must say
	// so and must not reference clickable cards or waiting.
	result, _, err := handler(context.Background(), nil, permissionPromptInput{
		ToolName:  "AskUserQuestion",
		Input:     map[string]any{"questions": []any{}},
		ToolUseID: "toolu_ask",
	})
	require.NoError(t, err)

	got := decodeDecision(t, result)
	assert.Equal(t, "deny", got.Behavior)
	assert.Equal(t, denyMsgAskUserQuestion, got.Message)
	assert.Empty(t, got.UpdatedInput, "deny must not set updatedInput")
}

func TestPermissionPrompt_UnknownTool_FailsClosedWithCardFallback(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()
	// A tool that is not AskUserQuestion and not on the allowlist gets the
	// generic fallback. A chat session in context must not change that — the
	// plain-text redirect is AskUserQuestion-specific.
	ctx := mcpcontext.WithChatSession(context.Background(), "AAAAAAAAAAAAAAAAAAAAAAAAAA")

	result, _, err := handler(ctx, nil, permissionPromptInput{
		ToolName:  "SomeFutureTool",
		Input:     map[string]any{"foo": "bar"},
		ToolUseID: "toolu_unknown",
	})
	require.NoError(t, err)

	got := decodeDecision(t, result)
	assert.Equal(t, "deny", got.Behavior)
	assert.Equal(t, denyMsgCardFallback, got.Message)
}

func TestPermissionPrompt_DecisionJSONShape(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()

	result, _, err := handler(context.Background(), nil, permissionPromptInput{
		ToolName:  "AskUserQuestion",
		ToolUseID: "toolu_shape",
	})
	require.NoError(t, err)

	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	// The wire payload must be exactly the documented permission-prompt
	// contract: top-level keys {behavior, message}. updatedInput must be
	// absent on deny so the SDK does not try to merge a nil map into the
	// downstream tool's input.
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &raw))
	assert.Equal(t, "deny", raw["behavior"])
	assert.Contains(t, raw, "message")
	assert.NotContains(t, raw, "updatedInput")
}

// TestPermissionPrompt_SDKCallPath_NoStructuredContent guards against the
// Go MCP SDK auto-populating CallToolResult.StructuredContent. Claude Code's
// --permission-prompt-tool validator expects a single text content block and
// rejects responses that carry extra fields ("Expected a single text block
// param with type='text' and a string text value"). The SDK does the
// auto-population whenever the handler's Out type parameter is concrete, so
// the handler must declare Out as `any` and return nil.
func TestPermissionPrompt_SDKCallPath_NoStructuredContent(t *testing.T) {
	t.Parallel()

	session := newPermissionPromptSession(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "permission_prompt",
		Arguments: map[string]any{
			"tool_name":   "AskUserQuestion",
			"input":       map[string]any{"questions": []any{}},
			"tool_use_id": "toolu_sdk_test",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "permission_prompt must not surface as a tool error")

	assert.Nil(t, result.StructuredContent,
		"Claude Code rejects permission-prompt responses with structuredContent; "+
			"the handler's Out type must be `any` so the SDK does not auto-populate it")

	require.Len(t, result.Content, 1, "exactly one text content block expected")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content block must be *mcp.TextContent (got %T)", result.Content[0])

	var got permissionDecision
	require.NoError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.Equal(t, "deny", got.Behavior)
}

// TestPermissionPrompt_SDKCallPath_NoOutputSchema guards against the Go MCP
// SDK auto-generating an output schema when the handler declares a typed Out.
// Claude Code may dispatch to a different (stricter) response-validation path
// when the tool definition advertises an outputSchema, so the permission
// prompt tool must NOT have one.
func TestPermissionPrompt_SDKCallPath_NoOutputSchema(t *testing.T) {
	t.Parallel()

	session := newPermissionPromptSession(t)

	tools, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)

	var permTool *mcp.Tool

	for _, tool := range tools.Tools {
		if tool.Name == "permission_prompt" {
			permTool = tool

			break
		}
	}

	require.NotNil(t, permTool, "permission_prompt tool must be registered")

	assert.Nil(t, permTool.OutputSchema,
		"permission_prompt must not advertise an output schema; "+
			"the handler's Out type must be `any` so the SDK does not auto-generate one")
}
