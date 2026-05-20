package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type agentCardInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
}

type addLogInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Action  string `json:"action" jsonschema:"required,action type (e.g. status_update/note/blocker)"`
	Message string `json:"message" jsonschema:"required,log message"`
}

type completeTaskInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Summary string `json:"summary" jsonschema:"required,one-line summary of what was done"`
}
type completeTaskOutput struct {
	Card     *board.Card `json:"card"`
	NextStep string      `json:"next_step,omitempty"`
}

// claimCardOutput surfaces auto-transition failures via typed fields so MCP
// callers can inspect them programmatically instead of parsing free-form
// text from CallToolResult.Content. The embedded *board.Card inlines the
// usual card fields at the JSON root so existing callers that decoded the
// claim_card response as board.Card keep working — the new fields just
// appear alongside.
//
// When AutoTransitionFailed is true, the embedded card reflects the
// post-claim, pre-transition state (todo, agent set) because the
// transition that would have moved it to in_progress did not succeed.
type claimCardOutput struct {
	*board.Card
	AutoTransitionFailed bool   `json:"auto_transition_failed,omitempty"`
	AutoTransitionError  string `json:"auto_transition_error,omitempty"`
}

func registerClaimCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_card",
		Description: "Claim a card for an agent and auto-transition to 'in_progress' if possible. Only one agent can claim a card at a time. Returns 'already claimed' error if another agent holds it. Claiming sets last_heartbeat — you must call heartbeat periodically to avoid being marked stalled. If the auto-transition to in_progress fails (e.g. config forbids it), the claim still succeeds and auto_transition_failed=true plus auto_transition_error are set on the response.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, claimCardOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, claimCardOutput{}, err
		}

		card, err := svc.ClaimCard(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, claimCardOutput{}, fmt.Errorf("claim card %s: %w", input.CardID, err)
		}

		out := claimCardOutput{Card: card}

		// Auto-transition to in_progress only from todo — claiming a card
		// in review/done/blocked should not change its state.
		if card.State == board.StateTodo {
			transitioned, terr := svc.TransitionTo(ctx, project, input.CardID, board.StateInProgress)
			if terr != nil {
				slog.Warn("claim_card: auto-transition to in_progress failed", "card_id", input.CardID, "error", terr)

				out.AutoTransitionFailed = true
				out.AutoTransitionError = terr.Error()
				// Continue — claim succeeded, transition did not. The embedded
				// card stays at the post-claim state so callers can see the
				// claim landed even though the state did not move.
			} else {
				out.Card = transitioned
			}
		}

		return nil, out, nil
	})
}

func registerReleaseCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "release_card",
		Description: "Release an agent's claim on a card. The agent_id must match the current owner. After release, any agent can claim the card.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.ReleaseCard(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("release card %s: %w", input.CardID, err)
		}

		return nil, card, nil
	})
}

func registerHeartbeat(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "heartbeat",
		Description: "Update the heartbeat timestamp for a claimed card. MUST be called periodically (at least every 30 minutes) while working on a task, or the card will be marked stalled and your claim released.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, any, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		if err := svc.HeartbeatCard(ctx, project, input.CardID, input.AgentID); err != nil {
			return nil, nil, fmt.Errorf("heartbeat card %s: %w", input.CardID, err)
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card after heartbeat: %w", err)
		}

		return nil, card, nil
	})
}

func registerAddLog(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_log",
		Description: "Append an activity log entry to a card. The log is capped at 50 entries (oldest dropped). Use action types like 'status_update', 'note', 'blocker', 'decision'. Requires an active claim by the caller: unclaimed cards are rejected so attacker-supplied agent_ids cannot land in the activity log.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addLogInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		// Ownership gate: AddLogEntry only verifies ownership when
		// AssignedAgent is non-empty. For unclaimed cards it would happily
		// write the caller-supplied agent_id verbatim into the activity log
		// (audit trail forgery + impersonation surface). Require an active
		// claim by this caller at the handler boundary so the audit trail
		// stays trustworthy.
		if err := requireActiveClaim(ctx, svc, project, input.CardID, input.AgentID, "add_log"); err != nil {
			return nil, nil, err
		}

		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  input.Action,
			Message: input.Message,
		}

		card, err := svc.AddLogEntry(ctx, project, input.CardID, entry)
		if err != nil {
			return nil, nil, fmt.Errorf("add log to %s: %w", input.CardID, err)
		}

		return nil, card, nil
	})
}

func registerCompleteTask(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Atomically complete a task: adds a completion log entry, walks through required state transitions, and releases the claim. Subtasks (cards with a parent) transition to 'done'. Main tasks (no parent) transition to 'review' for the review workflow. Use this instead of separate add_log + transition_card calls.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input completeTaskInput) (*mcp.CallToolResult, completeTaskOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, completeTaskOutput{}, err
		}

		// Ownership gate: complete_task drives state transitions and releases
		// the claim, so the caller must currently own the card. AddLogEntry +
		// TransitionTo skip ownership checks when AssignedAgent is empty, which
		// would let any caller drive an unclaimed card to done. Reject up front.
		if err := requireActiveClaim(ctx, svc, project, input.CardID, input.AgentID, "complete_task"); err != nil {
			return nil, completeTaskOutput{}, err
		}

		// Add completion log entry
		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  "completed",
			Message: input.Summary,
		}

		card, err := svc.AddLogEntry(ctx, project, input.CardID, entry)
		if err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("add completion log: %w", err)
		}

		parentID := card.Parent

		targetState := board.StateReview
		if parentID != "" {
			targetState = board.StateDone
		}

		// Walk through intermediate transitions to reach target state
		if _, err := svc.TransitionTo(ctx, project, input.CardID, targetState); err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("transition to %s failed (log entry already written): %w", targetState, err)
		}

		// Release the claim — if this fails, the transition already committed,
		// so log the error and include a warning rather than failing the whole operation.
		var releaseWarning string

		card, err = svc.ReleaseCard(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			slog.Warn("complete_task: release failed after transition", "card_id", input.CardID, "error", err)
			releaseWarning = fmt.Sprintf("warning: release failed after transition: %v", err)
			// Re-read card to return current state
			card, err = svc.GetCard(ctx, project, input.CardID)
			if err != nil {
				return nil, completeTaskOutput{}, fmt.Errorf("get card after release failure: %w", err)
			}
		}

		out := completeTaskOutput{Card: card}

		// Build informational next_step, preserving release warning if present.
		var parts []string
		if releaseWarning != "" {
			parts = append(parts, releaseWarning)
		}

		if targetState == board.StateReview {
			parts = append(parts, fmt.Sprintf("Card %s transitioned to review.", input.CardID))
		} else if parentID != "" {
			// Check if all sibling subtasks are now done. The parent stays in
			// in_progress — the orchestrator spawns a documentation sub-agent
			// first, then manually transitions the parent to review.
			siblings, serr := svc.ListCards(ctx, project, storage.CardFilter{Parent: parentID})
			if serr == nil {
				allDone := true

				for _, sib := range siblings {
					if sib.State != board.StateDone {
						allDone = false

						break
					}
				}

				if allDone {
					parts = append(parts, fmt.Sprintf("All subtasks done. Parent %s stays in in_progress for documentation.", parentID))
				}
			}
		}

		if len(parts) > 0 {
			out.NextStep = strings.Join(parts, " ")
		}

		return nil, out, nil
	})
}
