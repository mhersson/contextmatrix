package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
)

// permissionPromptInput is the request shape Claude Code's SDK sends when an
// agent calls a tool whose checkPermissions returns behavior:"ask". The SDK
// forwards (tool_name, input, tool_use_id); this MCP tool replies with
// allow/deny via the result's text content. Wired in via the runner's
// `claude --permission-prompt-tool mcp__contextmatrix__permission_prompt`
// invocation in docker/entrypoint.sh.
//
// Phase 1: always denies. AskUserQuestion gets a context-aware instruction
// (chat-mode caller is told to re-ask as plain text; card-mode caller is
// told to choose a different approach or report via add_log). Without this
// gate Claude Code auto-denies in headless mode and the model has been
// observed to silently make decisions the operator never sees.
//
// Phase 2 (deferred): AskUserQuestion will block waiting for the operator's
// answer through the chat UI and return behavior:"allow" with answers
// pre-filled into updatedInput so AskUserQuestion.call() can emit a proper
// structured tool_result. Tracked separately.
type permissionPromptInput struct {
	ToolName  string         `json:"tool_name" jsonschema:"required,name of the tool the agent is requesting permission for"`
	Input     map[string]any `json:"input" jsonschema:"input the agent passed to the tool"`
	ToolUseID string         `json:"tool_use_id" jsonschema:"tool_use id of the pending tool call"`
}

// permissionDecision is the JSON shape Claude Code's SDK expects in the
// result's text content block. omitempty omits the inactive branch field.
type permissionDecision struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Message      string         `json:"message,omitempty"`
}

const (
	// denyMsgChatAskUserQuestion is returned when AskUserQuestion is called
	// from a chat-mode container (X-CM-Chat-Session header present). The
	// operator is reading the chat live, so the model can recover by
	// re-asking as plain text.
	denyMsgChatAskUserQuestion = "Reply with your question as plain text to the user. " +
		"Do NOT proceed, retry this tool, or make assumptions — " +
		"the user is reading the chat and will answer."

	// denyMsgCardFallback is returned when the caller is not in a chat
	// session (card mode, autonomous, knowledge-refresh, or any unknown
	// tool that hit the ask gate). No human chat surface to redirect to.
	denyMsgCardFallback = "Tool not permitted in this environment. " +
		"Choose a different approach or stop and report the blocker via add_log."
)

func registerPermissionPrompt(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "permission_prompt",
		Description: "Permission gate for tool calls that hit a behavior:\"ask\" branch " +
			"in headless Claude Code (wired via --permission-prompt-tool). Phase 1 always " +
			"denies; AskUserQuestion is denied with an explicit instruction telling the " +
			"model what to do instead. Not intended for direct agent invocation.",
	}, buildPermissionPromptHandler())
}

// buildPermissionPromptHandler returns the handler closure. Extracted for
// direct unit testing without standing up the MCP server.
func buildPermissionPromptHandler() func(context.Context, *mcp.CallToolRequest, permissionPromptInput) (*mcp.CallToolResult, permissionDecision, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in permissionPromptInput) (*mcp.CallToolResult, permissionDecision, error) {
		session := mcpcontext.ChatSession(ctx)

		slog.Info("mcp: permission_prompt",
			"tool_name", in.ToolName,
			"tool_use_id", in.ToolUseID,
			"session_id", session,
		)

		message := denyMsgCardFallback
		if in.ToolName == "AskUserQuestion" && session != "" {
			message = denyMsgChatAskUserQuestion
		}

		decision := permissionDecision{Behavior: "deny", Message: message}

		// Marshal explicitly so the wire bytes are exactly the SDK contract
		// (no field-ordering surprises from the SDK's auto-marshal path).
		payload, err := json.Marshal(decision)
		if err != nil {
			return nil, permissionDecision{}, fmt.Errorf("permission_prompt: marshal decision: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, decision, nil
	}
}
