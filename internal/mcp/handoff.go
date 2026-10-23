package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
)

// handOffInput is the agent-facing argument shape.
type handOffInput struct {
	Title    string   `json:"title"    jsonschema:"required,short name for the blueprint (at most 256 bytes)"`
	Need     string   `json:"need"     jsonschema:"required,markdown brief for the architect (at most 65536 bytes)"`
	Projects []string `json:"projects" jsonschema:"required,ContextMatrix projects that need changes (at least one)"`
}

// handOffOutput acknowledges the call; Seq is the hand-off row's sequence number.
type handOffOutput struct {
	OK  bool  `json:"ok"`
	Seq int64 `json:"seq"`
}

// registerHandOffToArchitect adds the hand_off_to_architect MCP tool. It acts
// on the calling chat only, named by X-CM-Chat-Session.
func registerHandOffToArchitect(server *mcp.Server, mgr *chat.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "hand_off_to_architect",
		Description: "Hand the need worked out in this chat over to the ContextMatrix architect. " +
			"Call this only when the operator asks you to hand over, hand off, or start a blueprint from this chat. " +
			"ContextMatrix posts a hand-off block into this chat; nothing is created and nothing is spent until " +
			"the operator reviews it and presses Create. Calling again posts a newer hand-off that replaces the earlier one. " +
			"`title`: a short name for the blueprint. " +
			"`projects`: the ContextMatrix projects that need changes, at least one; never a project read only for context. " +
			"`need`: markdown of at most 65536 bytes, written for an architect who has not seen this chat. " +
			"Cover, in the operator's words where possible: what the operator wants; the decisions made here and why; " +
			"for each project that will not change, what you learned from it (files, interfaces, facts), because the " +
			"architect will not check those out; each project to change and why; what is still open. " +
			"Leave out ideas the chat tried and dropped.",
	}, buildHandOffToArchitectTool(mgr))
}

// buildHandOffToArchitectTool returns the handler closure for the
// hand_off_to_architect tool. Extracted for direct unit testing.
func buildHandOffToArchitectTool(mgr *chat.Manager) func(context.Context, *mcp.CallToolRequest, handOffInput) (*mcp.CallToolResult, handOffOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in handOffInput) (*mcp.CallToolResult, handOffOutput, error) {
		caller := mcpcontext.ChatSession(ctx)
		if caller == "" {
			return nil, handOffOutput{}, errors.New("hand_off_to_architect: works only inside a ContextMatrix chat")
		}

		msg, err := mgr.PostHandoff(ctx, caller, chat.Handoff{Title: in.Title, Need: in.Need, Projects: in.Projects})
		if err != nil {
			return nil, handOffOutput{}, fmt.Errorf("hand_off_to_architect: %w", err)
		}

		return nil, handOffOutput{OK: true, Seq: msg.Seq}, nil
	}
}
