package mcp

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
)

// maxSummaryBytes is the upper bound on the rehydration summary payload.
// A 5 MiB summary would persist in SQLite and be dispatched to every
// connected SSE subscriber; 16 KiB is generous for a one-paragraph anchor.
const maxSummaryBytes = 16 * 1024

// sanitizeLogField strips non-printable runes (including newlines and ANSI
// escape sequences) from s so user-controlled values are safe to interpolate
// into structured log messages.
func sanitizeLogField(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}

		return -1
	}, s)
}

// chatRehydrationCompleteInput is the agent-facing argument shape.
type chatRehydrationCompleteInput struct {
	SessionID string `json:"session_id" jsonschema:"required,id of the chat session being resumed"`
	Summary   string `json:"summary"    jsonschema:"required,one-paragraph summary of where the conversation left off (becomes the first visible message of the resumed chat)"`
}

// chatRehydrationCompleteOutput acknowledges the call.
type chatRehydrationCompleteOutput struct {
	OK bool `json:"ok"`
}

// registerChatRehydrationComplete adds the chat_rehydration_complete MCP tool.
// The agent calls this once it has read /run/cm-chat/resume.jsonl and
// re-established workspace state. The call:
//
//   - flips chat_sessions.rehydration_active off so the UI un-collapses the
//     restoration block and stops showing the "Restoring workspace…" spinner;
//   - persists `summary` as a normal (non-phase) assistant_text message so
//     the operator sees an anchor point ("ready to continue") in the thread.
//
// Idempotent: a second call with the flag already off is a successful no-op.
func registerChatRehydrationComplete(server *mcp.Server, mgr *chat.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "chat_rehydration_complete",
		Description: "Signal that the chat-mode rehydration phase is complete. " +
			"Call this exactly once per resumed chat session after reading " +
			"/run/cm-chat/resume.jsonl and re-establishing any workspace state " +
			"(re-cloning repos, restoring branches, etc.). The summary argument " +
			"becomes the first visible message of the resumed chat - keep it " +
			"to one short paragraph.",
	}, buildChatRehydrationCompleteTool(mgr))
}

// buildChatRehydrationCompleteTool returns the handler closure for the
// chat_rehydration_complete tool. Extracted for direct unit testing.
func buildChatRehydrationCompleteTool(mgr *chat.Manager) func(context.Context, *mcp.CallToolRequest, chatRehydrationCompleteInput) (*mcp.CallToolResult, chatRehydrationCompleteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in chatRehydrationCompleteInput) (*mcp.CallToolResult, chatRehydrationCompleteOutput, error) {
		if strings.TrimSpace(in.SessionID) == "" {
			return nil, chatRehydrationCompleteOutput{}, fmt.Errorf("chat_rehydration_complete: session_id is required")
		}

		if strings.TrimSpace(in.Summary) == "" {
			return nil, chatRehydrationCompleteOutput{}, fmt.Errorf("chat_rehydration_complete: summary is required")
		}

		if len(in.Summary) > maxSummaryBytes {
			return nil, chatRehydrationCompleteOutput{}, fmt.Errorf("chat_rehydration_complete: summary exceeds %d-byte limit (%d bytes)", maxSummaryBytes, len(in.Summary))
		}

		// Gate the call to the caller's own session. Chat-container callers
		// forward CM_CHAT_SESSION via X-CM-Chat-Session; the middleware stashes
		// it into ctx. Empty caller means the header was absent (card-mode
		// worker, human curl) so we skip the check.
		if caller := mcpcontext.ChatSession(ctx); caller != "" && caller != in.SessionID {
			return nil, chatRehydrationCompleteOutput{}, fmt.Errorf("chat_rehydration_complete: session mismatch: caller=%s session_id=%s",
				sanitizeLogField(caller), sanitizeLogField(in.SessionID))
		}

		if err := mgr.CompleteRehydration(ctx, in.SessionID, in.Summary); err != nil {
			return nil, chatRehydrationCompleteOutput{}, fmt.Errorf("chat_rehydration_complete: %w", err)
		}

		return nil, chatRehydrationCompleteOutput{OK: true}, nil
	}
}
