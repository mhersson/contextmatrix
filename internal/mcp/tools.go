package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// registerTools adds all MCP tools to the server.
func registerTools(server *mcp.Server, svc *service.CardService) {
	registerListProjects(server, svc)
	registerListCards(server, svc)
	registerGetCard(server, svc)
	registerCreateCard(server, svc)
	registerUpdateCard(server, svc)
	registerTransitionCard(server, svc)
	registerClaimCard(server, svc)
	registerReleaseCard(server, svc)
	registerHeartbeat(server, svc)
	registerAddLog(server, svc)
	registerGetTaskContext(server, svc)
	registerCompleteTask(server, svc)
	registerGetSubtaskSummary(server, svc)
	registerGetReadyTasks(server, svc)
}

// --- Input/Output types ---

type listProjectsInput struct{}
type listProjectsOutput struct {
	Projects []board.ProjectConfig `json:"projects"`
}

type listCardsInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	State   string `json:"state,omitempty" jsonschema:"filter by state"`
	Type    string `json:"type,omitempty" jsonschema:"filter by card type"`
	Label   string `json:"label,omitempty" jsonschema:"filter by label"`
	Agent   string `json:"agent,omitempty" jsonschema:"filter by assigned agent"`
	Parent  string `json:"parent,omitempty" jsonschema:"filter by parent card ID"`
}
type listCardsOutput struct {
	Cards []*board.Card `json:"cards"`
}

type getCardInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	CardID  string `json:"card_id" jsonschema:"required,card ID (e.g. ALPHA-001)"`
}

type createCardInput struct {
	Project   string   `json:"project" jsonschema:"required,project name"`
	Title     string   `json:"title" jsonschema:"required,card title"`
	Type      string   `json:"type" jsonschema:"required,card type (task/bug/feature)"`
	Priority  string   `json:"priority" jsonschema:"required,priority (low/medium/high/critical)"`
	Labels    []string `json:"labels,omitempty" jsonschema:"optional labels"`
	Body      string   `json:"body,omitempty" jsonschema:"optional markdown body"`
	Parent    string   `json:"parent,omitempty" jsonschema:"parent card ID for subtasks"`
	DependsOn []string `json:"depends_on,omitempty" jsonschema:"card IDs this depends on"`
}

type updateCardInput struct {
	Project  string   `json:"project" jsonschema:"required,project name"`
	CardID   string   `json:"card_id" jsonschema:"required,card ID"`
	Title    *string  `json:"title,omitempty" jsonschema:"new title"`
	Priority *string  `json:"priority,omitempty" jsonschema:"new priority"`
	Labels   []string `json:"labels,omitempty" jsonschema:"new labels (replaces all)"`
	Body     *string  `json:"body,omitempty" jsonschema:"new markdown body"`
}

type transitionCardInput struct {
	Project  string `json:"project" jsonschema:"required,project name"`
	CardID   string `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string `json:"agent_id,omitempty" jsonschema:"agent performing the transition"`
	NewState string `json:"new_state" jsonschema:"required,target state"`
}

type agentCardInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
}

type addLogInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Action  string `json:"action" jsonschema:"required,action type (e.g. status_update/note/blocker)"`
	Message string `json:"message" jsonschema:"required,log message"`
}

type getTaskContextInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
}
type getTaskContextOutput struct {
	Card     *board.Card          `json:"card"`
	Parent   *board.Card          `json:"parent,omitempty"`
	Siblings []*board.Card        `json:"siblings,omitempty"`
	Config   *board.ProjectConfig `json:"config"`
}

type completeTaskInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Summary string `json:"summary" jsonschema:"required,one-line summary of what was done"`
}

type getSubtaskSummaryInput struct {
	Project  string `json:"project" jsonschema:"required,project name"`
	ParentID string `json:"parent_id" jsonschema:"required,parent card ID"`
}
type getSubtaskSummaryOutput struct {
	ParentID string         `json:"parent_id"`
	Total    int            `json:"total"`
	Counts   map[string]int `json:"counts"`
}

type getReadyTasksInput struct {
	Project  string `json:"project" jsonschema:"required,project name"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"optional parent card ID to scope search"`
}
type getReadyTasksOutput struct {
	Cards []*board.Card `json:"cards"`
}

// --- Tool registrations ---

func registerListProjects(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_projects",
		Description: "List all projects on the board with their configurations (states, types, priorities, transitions).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listProjectsInput) (*mcp.CallToolResult, listProjectsOutput, error) {
		projects, err := svc.ListProjects(ctx)
		if err != nil {
			return nil, listProjectsOutput{}, fmt.Errorf("list projects: %w", err)
		}
		return nil, listProjectsOutput{Projects: projects}, nil
	})
}

func registerListCards(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_cards",
		Description: "List cards in a project, optionally filtered by state, type, label, agent, or parent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listCardsInput) (*mcp.CallToolResult, listCardsOutput, error) {
		filter := storage.CardFilter{
			State:         input.State,
			Type:          input.Type,
			Label:         input.Label,
			AssignedAgent: input.Agent,
			Parent:        input.Parent,
		}
		cards, err := svc.ListCards(ctx, input.Project, filter)
		if err != nil {
			return nil, listCardsOutput{}, fmt.Errorf("list cards: %w", err)
		}
		if cards == nil {
			cards = []*board.Card{}
		}
		return nil, listCardsOutput{Cards: cards}, nil
	})
}

func registerGetCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_card",
		Description: "Get a single card by ID, including its full body and metadata.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCardInput) (*mcp.CallToolResult, *board.Card, error) {
		card, err := svc.GetCard(ctx, input.Project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card %s: %w", input.CardID, err)
		}
		return nil, card, nil
	})
}

func registerCreateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a new card in a project. Returns the created card with its generated ID. The card starts in the project's first state (usually 'todo').",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createCardInput) (*mcp.CallToolResult, *board.Card, error) {
		svcInput := service.CreateCardInput{
			Title:    input.Title,
			Type:     input.Type,
			Priority: input.Priority,
			Labels:   input.Labels,
			Body:     input.Body,
			Parent:   input.Parent,
		}
		card, err := svc.CreateCard(ctx, input.Project, svcInput)
		if err != nil {
			return nil, nil, fmt.Errorf("create card: %w", err)
		}

		// If depends_on was provided, update the card to set them
		if len(input.DependsOn) > 0 {
			card, err = svc.UpdateCard(ctx, input.Project, card.ID, service.UpdateCardInput{
				Title:     card.Title,
				Type:      card.Type,
				State:     card.State,
				Priority:  card.Priority,
				Labels:    card.Labels,
				Parent:    card.Parent,
				DependsOn: input.DependsOn,
				Body:      card.Body,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("set depends_on: %w", err)
			}
		}

		return nil, card, nil
	})
}

func registerUpdateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_card",
		Description: "Update a card's mutable fields. Only provided fields are changed; omitted fields keep their current values. Does NOT change state — use transition_card for state changes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updateCardInput) (*mcp.CallToolResult, *board.Card, error) {
		patchInput := service.PatchCardInput{
			Title:    input.Title,
			Priority: input.Priority,
			Labels:   input.Labels,
			Body:     input.Body,
		}
		card, err := svc.PatchCard(ctx, input.Project, input.CardID, patchInput)
		if err != nil {
			return nil, nil, fmt.Errorf("update card %s: %w", input.CardID, err)
		}
		return nil, card, nil
	})
}

func registerTransitionCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "transition_card",
		Description: "Change a card's state. Validates that the transition is allowed by the project's state machine. Returns 'invalid state transition' error with valid targets if not allowed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input transitionCardInput) (*mcp.CallToolResult, *board.Card, error) {
		patchInput := service.PatchCardInput{
			State: &input.NewState,
		}
		card, err := svc.PatchCard(ctx, input.Project, input.CardID, patchInput)
		if err != nil {
			return nil, nil, fmt.Errorf("transition card %s to %s: %w", input.CardID, input.NewState, err)
		}
		return nil, card, nil
	})
}

func registerClaimCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_card",
		Description: "Claim a card for an agent. Only one agent can claim a card at a time. Returns 'already claimed' error if another agent holds it. Claiming sets last_heartbeat — you must call heartbeat periodically to avoid being marked stalled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, *board.Card, error) {
		card, err := svc.ClaimCard(ctx, input.Project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("claim card %s: %w", input.CardID, err)
		}
		return nil, card, nil
	})
}

func registerReleaseCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "release_card",
		Description: "Release an agent's claim on a card. The agent_id must match the current owner. After release, any agent can claim the card.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, *board.Card, error) {
		card, err := svc.ReleaseCard(ctx, input.Project, input.CardID, input.AgentID)
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
		if err := svc.HeartbeatCard(ctx, input.Project, input.CardID, input.AgentID); err != nil {
			return nil, nil, fmt.Errorf("heartbeat card %s: %w", input.CardID, err)
		}
		return nil, nil, nil
	})
}

func registerAddLog(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_log",
		Description: "Append an activity log entry to a card. The log is capped at 50 entries (oldest dropped). Use action types like 'status_update', 'note', 'blocker', 'decision'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addLogInput) (*mcp.CallToolResult, *board.Card, error) {
		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  input.Action,
			Message: input.Message,
		}
		if err := svc.AddLogEntry(ctx, input.Project, input.CardID, entry); err != nil {
			return nil, nil, fmt.Errorf("add log to %s: %w", input.CardID, err)
		}
		card, err := svc.GetCard(ctx, input.Project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card after log: %w", err)
		}
		return nil, card, nil
	})
}

func registerGetTaskContext(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_context",
		Description: "Get a card with its parent card, sibling cards (same parent), and project config in a single call. Sub-agents should call this first before touching anything — it eliminates multiple round-trips.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskContextInput) (*mcp.CallToolResult, getTaskContextOutput, error) {
		card, err := svc.GetCard(ctx, input.Project, input.CardID)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get card %s: %w", input.CardID, err)
		}

		cfg, err := svc.GetProject(ctx, input.Project)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get project config: %w", err)
		}

		out := getTaskContextOutput{
			Card:   card,
			Config: cfg,
		}

		// Load parent if set
		if card.Parent != "" {
			parent, err := svc.GetCard(ctx, input.Project, card.Parent)
			if err == nil {
				out.Parent = parent
			}
		}

		// Load siblings (cards with same parent)
		if card.Parent != "" {
			siblings, err := svc.ListCards(ctx, input.Project, storage.CardFilter{Parent: card.Parent})
			if err == nil {
				// Exclude self from siblings
				filtered := make([]*board.Card, 0, len(siblings))
				for _, s := range siblings {
					if s.ID != card.ID {
						filtered = append(filtered, s)
					}
				}
				out.Siblings = filtered
			}
		}

		return nil, out, nil
	})
}

func registerCompleteTask(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Atomically complete a task: adds a completion log entry and transitions the card to 'done'. Fails if the card is not in a state that can transition to 'done'. Use this instead of separate add_log + transition_card calls.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input completeTaskInput) (*mcp.CallToolResult, *board.Card, error) {
		// Add completion log entry
		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  "completed",
			Message: input.Summary,
		}
		if err := svc.AddLogEntry(ctx, input.Project, input.CardID, entry); err != nil {
			return nil, nil, fmt.Errorf("add completion log: %w", err)
		}

		// Transition to done
		done := "done"
		if _, err := svc.PatchCard(ctx, input.Project, input.CardID, service.PatchCardInput{
			State: &done,
		}); err != nil {
			return nil, nil, fmt.Errorf("transition to done: %w", err)
		}

		// Release the claim
		card, err := svc.ReleaseCard(ctx, input.Project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("release card: %w", err)
		}

		return nil, card, nil
	})
}

func registerGetSubtaskSummary(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_subtask_summary",
		Description: "Get counts of subtasks by state for a parent card. Returns {todo: N, in_progress: N, done: N, ...}. Use this to check if all subtasks are done before transitioning the parent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSubtaskSummaryInput) (*mcp.CallToolResult, getSubtaskSummaryOutput, error) {
		cards, err := svc.ListCards(ctx, input.Project, storage.CardFilter{Parent: input.ParentID})
		if err != nil {
			return nil, getSubtaskSummaryOutput{}, fmt.Errorf("list subtasks: %w", err)
		}

		counts := make(map[string]int)
		for _, card := range cards {
			counts[card.State]++
		}

		return nil, getSubtaskSummaryOutput{
			ParentID: input.ParentID,
			Total:    len(cards),
			Counts:   counts,
		}, nil
	})
}

func registerGetReadyTasks(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ready_tasks",
		Description: "Get unclaimed 'todo' cards that are ready to start — all depends_on cards are in 'done' state. Optionally scoped to a parent card's subtasks. Use this to find which tasks can be started in parallel.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getReadyTasksInput) (*mcp.CallToolResult, getReadyTasksOutput, error) {
		filter := storage.CardFilter{State: "todo"}
		if input.ParentID != "" {
			filter.Parent = input.ParentID
		}

		cards, err := svc.ListCards(ctx, input.Project, filter)
		if err != nil {
			return nil, getReadyTasksOutput{}, fmt.Errorf("list todo cards: %w", err)
		}

		// Filter to unclaimed cards with all dependencies met
		ready := make([]*board.Card, 0)
		for _, card := range cards {
			if card.AssignedAgent != "" {
				continue // already claimed
			}
			if !allDepsDone(ctx, svc, input.Project, card.DependsOn) {
				continue
			}
			ready = append(ready, card)
		}

		return nil, getReadyTasksOutput{Cards: ready}, nil
	})
}

// allDepsDone checks if all dependency cards are in "done" state.
func allDepsDone(ctx context.Context, svc *service.CardService, project string, deps []string) bool {
	for _, depID := range deps {
		dep, err := svc.GetCard(ctx, project, depID)
		if err != nil {
			return false // can't verify, treat as not done
		}
		if dep.State != "done" {
			return false
		}
	}
	return true
}
