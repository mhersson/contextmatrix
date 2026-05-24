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
// Phase 1: always denies. AskUserQuestion gets a context-aware instruction:
// chat-mode caller is told to stop and wait silently (the runner's logparser
// has already surfaced the AskUserQuestion as a UserQuestionCard with
// clickable option buttons; the user's click sends the chosen label back
// through the normal chat send path, so the model only needs to wait for
// its next user turn). Card-mode caller is told to choose a different
// approach or report via add_log. Without this gate Claude Code auto-denies
// in headless mode and the model has been observed to silently make
// decisions the operator never sees.
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
	// runner's logparser already surfaces the tool_use as a
	// UserQuestionCard rendered with clickable option buttons; the user's
	// click is sent through the normal chat send path as a fresh user
	// message and arrives at the runner as the model's next user turn.
	// So the model just needs to stop. The strong "end your turn,
	// no filler text" framing is load-bearing: without it, the model's
	// first instinct is to acknowledge ("OK, waiting for your answer.")
	// or re-ask in plain text, both of which duplicate the rendered card.
	denyMsgChatAskUserQuestion = "The user has already been shown your question " +
		"with clickable option buttons in the chat UI. Stop now and end your " +
		"turn — do NOT re-ask, paraphrase, retry this tool, add filler text " +
		"(\"OK\", \"Waiting for your answer\", etc.), or make assumptions. " +
		"The user's answer will arrive as the next user message in this chat."

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
//
// The Out type parameter is `any` (not permissionDecision) on purpose:
// declaring a typed Out triggers two SDK behaviors that Claude Code's
// --permission-prompt-tool validator rejects with "Expected a single text
// block param with type='text' and a string text value":
//
//  1. The SDK auto-generates an outputSchema in the tool listing.
//  2. The SDK auto-populates CallToolResult.StructuredContent from the
//     returned Out value.
//
// Either one alone causes the wire response to carry more than the single
// text block Claude Code expects. Returning `nil` as Out skips both code
// paths in the SDK (see toolForErr in go-sdk/mcp/server.go).
func buildPermissionPromptHandler() func(context.Context, *mcp.CallToolRequest, permissionPromptInput) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in permissionPromptInput) (*mcp.CallToolResult, any, error) {
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
			return nil, nil, fmt.Errorf("permission_prompt: marshal decision: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}
