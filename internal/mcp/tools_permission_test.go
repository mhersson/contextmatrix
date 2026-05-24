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

func TestPermissionPrompt_AskUserQuestion_ChatMode_DenyTellsModelToReReplyInChat(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()
	// Simulate a chat-mode caller by stashing a session ID into context via
	// the same helper the middleware uses in production.
	ctx := mcpcontext.WithChatSession(context.Background(), "AAAAAAAAAAAAAAAAAAAAAAAAAA")

	result, _, err := handler(ctx, nil, permissionPromptInput{
		ToolName:  "AskUserQuestion",
		Input:     map[string]any{"questions": []any{}},
		ToolUseID: "toolu_chat_test",
	})
	require.NoError(t, err)

	got := decodeDecision(t, result)
	assert.Equal(t, "deny", got.Behavior)
	assert.Equal(t, denyMsgChatAskUserQuestion, got.Message)
	assert.Empty(t, got.UpdatedInput, "deny must not set updatedInput")
}

func TestPermissionPrompt_AskUserQuestion_NoChatSession_DenyTellsModelToReportBlocker(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()

	// No chat session header → card / autonomous / KB mode. No chat surface
	// to redirect to, so the message points the agent at add_log instead.
	result, _, err := handler(context.Background(), nil, permissionPromptInput{
		ToolName:  "AskUserQuestion",
		Input:     map[string]any{"questions": []any{}},
		ToolUseID: "toolu_card_test",
	})
	require.NoError(t, err)

	got := decodeDecision(t, result)
	assert.Equal(t, "deny", got.Behavior)
	assert.Equal(t, denyMsgCardFallback, got.Message)
	assert.Empty(t, got.UpdatedInput, "deny must not set updatedInput")
}

func TestPermissionPrompt_UnknownTool_FailsClosedWithCardFallback(t *testing.T) {
	t.Parallel()

	handler := buildPermissionPromptHandler()
	// Even in chat mode, unknown tools that reach the ask gate get the
	// card-flavored message — the chat-flavored "ask via plain text"
	// instruction is meaningless for arbitrary tools.
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
