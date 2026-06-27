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
// This gate denies every tool call. AskUserQuestion gets a plain-text redirect —
// the runner does not render it as a clickable card, so the model is told to
// ask its question as an ordinary message (with options listed inline)
// instead. Every other tool that reaches this gate gets a generic deny.
// Without this gate Claude Code auto-denies in headless mode with no message,
// and the model silently makes decisions the operator never sees.
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
	// denyMsgAskUserQuestion is returned when AskUserQuestion reaches the gate.
	// The runner suppresses AskUserQuestion (it never renders), so the model
	// must ask in plain text instead — a bare deny with no message would
	// silently swallow the question. Asking as a normal message naturally ends
	// the turn and waits for the reply, so no "wait" framing is needed.
	// Autonomous runs are unlikely to reach here since they avoid interactive
	// questions; if one does, the plain-text redirect is harmless.
	denyMsgAskUserQuestion = "AskUserQuestion isn't available here. " +
		"Ask your question as a plain-text message, listing any options inline."

	// denyMsgCardFallback is returned for any other tool that reaches the gate
	// (a tool not on the runner's --allowed-tools list). Bash is allowlisted
	// unrestricted, so the common case here is an intentionally-excluded tool.
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

		// AskUserQuestion gets the plain-text redirect; every other tool that
		// reaches the gate gets the generic deny.
		message := denyMsgCardFallback

		if in.ToolName == "AskUserQuestion" {
			message = denyMsgAskUserQuestion
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
