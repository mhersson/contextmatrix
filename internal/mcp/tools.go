package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// registerTools adds all MCP tools to the server.
func registerTools(server *mcp.Server, svc *service.CardService, workflowSkillsDir string) {
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
	registerCheckAgentHealth(server, svc)
	registerGetReadyTasks(server, svc)
	registerReportUsage(server, svc)
	registerRecalculateCosts(server, svc)
	registerCreateProject(server, svc)
	registerUpdateProject(server, svc)
	registerDeleteProject(server, svc)
	registerStartWorkflow(server, svc, workflowSkillsDir)
	registerGetSkill(server, svc, workflowSkillsDir)
	registerReportPush(server, svc)
	registerIncrementReviewAttempts(server, svc)
	registerPromoteToAutonomous(server, svc)
}

// resolveProject resolves the project for a card ID when project is not provided.
// If project is already set, it returns it unchanged.
// If project is empty, it searches all projects for the card.
func resolveProject(ctx context.Context, svc *service.CardService, project, cardID string) (string, error) {
	if project != "" {
		return project, nil
	}

	_, proj, err := findCard(ctx, svc, cardID)
	if err != nil {
		return "", fmt.Errorf("resolve project for %s: %w", cardID, err)
	}

	return proj, nil
}

// --- Input/Output types ---

type (
	listProjectsInput  struct{}
	listProjectsOutput struct {
		Projects []board.ProjectConfig `json:"projects"`
	}
)

type listCardsInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	State   string `json:"state,omitempty" jsonschema:"filter by state"`
	Type    string `json:"type,omitempty" jsonschema:"filter by card type"`
	Label   string `json:"label,omitempty" jsonschema:"filter by label"`
	Agent   string `json:"agent,omitempty" jsonschema:"filter by assigned agent"`
	Parent  string `json:"parent,omitempty" jsonschema:"filter by parent card ID"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity — unvetted external card bodies are redacted for non-human callers"`
}
type listCardsOutput struct {
	Cards []*board.Card `json:"cards"`
}

type getCardInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID (e.g. ALPHA-001)"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity — unvetted external card bodies are redacted for non-human callers"`
}

type createCardInput struct {
	Project   string    `json:"project" jsonschema:"required,project name"`
	Title     string    `json:"title" jsonschema:"required,card title"`
	Type      string    `json:"type" jsonschema:"required,card type (task/bug/feature). Overridden to 'subtask' when parent is set."`
	Priority  string    `json:"priority" jsonschema:"required,priority (low/medium/high/critical)"`
	Labels    []string  `json:"labels,omitempty" jsonschema:"optional labels"`
	Skills    *[]string `json:"skills,omitempty" jsonschema:"optional task-skill names to mount in the runner container; nil inherits from parent or project default, [] means none, [list] constrains"`
	Body      string    `json:"body,omitempty" jsonschema:"optional markdown body"`
	Parent    string    `json:"parent,omitempty" jsonschema:"parent card ID for subtasks"`
	DependsOn []string  `json:"depends_on,omitempty" jsonschema:"card IDs this depends on"`
}

// NOTE: vetted, autonomous, feature_branch, create_pr are intentionally
// excluded — they are human-only fields.
type updateCardInput struct {
	Project  string    `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string    `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string    `json:"agent_id,omitempty" jsonschema:"agent performing the update — if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	Title    *string   `json:"title,omitempty" jsonschema:"new title"`
	Priority *string   `json:"priority,omitempty" jsonschema:"new priority"`
	Labels   []string  `json:"labels,omitempty" jsonschema:"new labels (replaces all)"`
	Skills   *[]string `json:"skills,omitempty" jsonschema:"new task skills (replaces all); [] means none, omit to leave unchanged"`
	Body     *string   `json:"body,omitempty" jsonschema:"new markdown body"`
}

type transitionCardInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string `json:"agent_id,omitempty" jsonschema:"agent performing the transition — if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	NewState string `json:"new_state" jsonschema:"required,target state"`
}

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

type getTaskContextInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity — unvetted external card bodies are redacted for non-human callers"`
}
type getTaskContextOutput struct {
	Card     *board.Card          `json:"card"`
	Parent   *board.Card          `json:"parent,omitempty"`
	Siblings []*board.Card        `json:"siblings,omitempty"`
	Config   *board.ProjectConfig `json:"config"`
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

type getSubtaskSummaryInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from parent ID if omitted)"`
	ParentID string `json:"parent_id" jsonschema:"required,parent card ID"`
}
type getSubtaskSummaryOutput struct {
	ParentID string         `json:"parent_id"`
	Total    int            `json:"total"`
	Counts   map[string]int `json:"counts"`
}

type checkAgentHealthInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from parent ID if omitted)"`
	ParentID string `json:"parent_id" jsonschema:"required,parent card ID whose subtasks to check"`
}

// AgentHealthStatus represents the computed health of a single subtask's agent.
type AgentHealthStatus struct {
	CardID            string `json:"card_id"`
	Title             string `json:"title"`
	State             string `json:"state"`
	AssignedAgent     string `json:"assigned_agent,omitempty"`
	LastHeartbeat     string `json:"last_heartbeat,omitempty"`
	SecondsSinceHbeat *int64 `json:"seconds_since_heartbeat,omitempty"`
	Status            string `json:"status"` // active, warning, stalled, unassigned, completed
}

type checkAgentHealthOutput struct {
	ParentID       string              `json:"parent_id"`
	TimeoutSeconds int64               `json:"timeout_seconds"`
	WarningSeconds int64               `json:"warning_seconds"`
	Subtasks       []AgentHealthStatus `json:"subtasks"`
	Summary        string              `json:"summary"`
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

		// Redact unvetted card bodies for non-human callers so prompt-injection
		// payloads from imported external cards cannot reach agent context.
		cards = redactCardsForAgent(cards, input.AgentID)

		return nil, listCardsOutput{Cards: cards}, nil
	})
}

func registerGetCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_card",
		Description: "Get a single card by ID, including its full body and metadata.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCardInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card %s: %w", input.CardID, err)
		}

		// Redact unvetted card body for non-human callers so prompt-injection
		// payloads from imported external cards cannot reach agent context.
		card = redactCardForAgent(card, input.AgentID)

		return nil, card, nil
	})
}

func registerCreateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a new card in a project. Returns the created card with its generated ID. The card starts in the project's first state (usually 'todo'). IMPORTANT: After creation, the card must be claimed with claim_card before any work begins. Never start working on a card without claiming it first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createCardInput) (*mcp.CallToolResult, *board.Card, error) {
		svcInput := service.CreateCardInput{
			Title:    input.Title,
			Type:     input.Type,
			Priority: input.Priority,
			Labels:   input.Labels,
			Skills:   input.Skills,
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
				Skills:    card.Skills,
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
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		patchInput := service.PatchCardInput{
			AgentID:  input.AgentID,
			Title:    input.Title,
			Priority: input.Priority,
			Labels:   input.Labels,
			Skills:   input.Skills,
			Body:     input.Body,
		}

		card, err := svc.PatchCard(ctx, project, input.CardID, patchInput)
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
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		// Agent-ownership check is now enforced inside PatchCard via AgentID.
		patchInput := service.PatchCardInput{
			AgentID: input.AgentID,
			State:   &input.NewState,
		}

		card, err := svc.PatchCard(ctx, project, input.CardID, patchInput)
		if err != nil {
			return nil, nil, fmt.Errorf("transition card %s to %s: %w", input.CardID, input.NewState, err)
		}

		return nil, card, nil
	})
}

func registerClaimCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_card",
		Description: "Claim a card for an agent and auto-transition to 'in_progress' if possible. Only one agent can claim a card at a time. Returns 'already claimed' error if another agent holds it. Claiming sets last_heartbeat — you must call heartbeat periodically to avoid being marked stalled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input agentCardInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.ClaimCard(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("claim card %s: %w", input.CardID, err)
		}
		// Auto-transition to in_progress only from todo — claiming a card
		// in review/done/blocked should not change its state.
		var transitionErr error

		if card.State == board.StateTodo {
			if transitioned, err := svc.TransitionTo(ctx, project, input.CardID, board.StateInProgress); err != nil {
				slog.Warn("claim_card: auto-transition to in_progress failed", "card_id", input.CardID, "error", err)
				transitionErr = err
				// Continue — claim succeeded, transition did not
			} else {
				card = transitioned
			}
		}

		if transitionErr != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Card claimed successfully (note: auto-transition to in_progress failed: %v)", transitionErr)},
				},
			}, card, nil
		}

		return nil, card, nil
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
		Description: "Append an activity log entry to a card. The log is capped at 50 entries (oldest dropped). Use action types like 'status_update', 'note', 'blocker', 'decision'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addLogInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  input.Action,
			Message: input.Message,
		}
		if err := svc.AddLogEntry(ctx, project, input.CardID, entry); err != nil {
			return nil, nil, fmt.Errorf("add log to %s: %w", input.CardID, err)
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
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
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, getTaskContextOutput{}, err
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get card %s: %w", input.CardID, err)
		}

		cfg, err := svc.GetProject(ctx, project)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get project config: %w", err)
		}

		out := getTaskContextOutput{
			// Redact unvetted card body for non-human callers — get_task_context
			// is the primary prompt-injection vector because its response is fed
			// straight into agent context.
			Card:   redactCardForAgent(card, input.AgentID),
			Config: cfg,
		}

		// Load parent if set
		if card.Parent != "" {
			parent, err := svc.GetCard(ctx, project, card.Parent)
			if err == nil {
				out.Parent = redactCardForAgent(parent, input.AgentID)
			}
		}

		// Load siblings (cards with same parent)
		if card.Parent != "" {
			siblings, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.Parent})
			if err == nil {
				// Exclude self from siblings
				filtered := make([]*board.Card, 0, len(siblings))
				for _, s := range siblings {
					if s.ID != card.ID {
						filtered = append(filtered, s)
					}
				}

				out.Siblings = redactCardsForAgent(filtered, input.AgentID)
			}
		}

		return nil, out, nil
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
		// Add completion log entry
		entry := board.ActivityEntry{
			Agent:   input.AgentID,
			Action:  "completed",
			Message: input.Summary,
		}
		if err := svc.AddLogEntry(ctx, project, input.CardID, entry); err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("add completion log: %w", err)
		}

		// Determine target state: subtasks go to done, main tasks go to review
		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("get card: %w", err)
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

func registerGetSubtaskSummary(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_subtask_summary",
		Description: "Get counts of subtasks by state for a parent card. Returns {todo: N, in_progress: N, done: N, ...}. Use this to check if all subtasks are done before transitioning the parent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSubtaskSummaryInput) (*mcp.CallToolResult, getSubtaskSummaryOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.ParentID)
		if err != nil {
			return nil, getSubtaskSummaryOutput{}, err
		}

		cards, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: input.ParentID})
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

func registerCheckAgentHealth(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_agent_health",
		Description: "Check health status of all subtask agents for a parent card. Returns heartbeat age and computed status (active/warning/stalled/unassigned/completed) for each subtask. Use this to detect dead sub-agents that need respawning.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input checkAgentHealthInput) (*mcp.CallToolResult, checkAgentHealthOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.ParentID)
		if err != nil {
			return nil, checkAgentHealthOutput{}, err
		}

		cards, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: input.ParentID})
		if err != nil {
			return nil, checkAgentHealthOutput{}, fmt.Errorf("list subtasks: %w", err)
		}

		timeout := svc.HeartbeatTimeout()
		warningThreshold := timeout / 2
		now := time.Now()

		var (
			subtasks                                                []AgentHealthStatus
			stalledCount, warningCount, activeCount, completedCount int
		)

		for _, card := range cards {
			status := AgentHealthStatus{
				CardID:        card.ID,
				Title:         card.Title,
				State:         card.State,
				AssignedAgent: card.AssignedAgent,
			}

			switch {
			case card.State == board.StateDone || card.State == board.StateReview:
				status.Status = "completed"
				completedCount++
			case card.State == board.StateStalled:
				status.Status = "stalled"
				stalledCount++
			case card.AssignedAgent == "":
				status.Status = "unassigned"
			default:
				if card.LastHeartbeat != nil {
					status.LastHeartbeat = card.LastHeartbeat.Format(time.RFC3339)
					elapsed := int64(now.Sub(*card.LastHeartbeat).Seconds())
					status.SecondsSinceHbeat = &elapsed

					switch {
					case now.Sub(*card.LastHeartbeat) >= timeout:
						status.Status = "stalled"
						stalledCount++
					case now.Sub(*card.LastHeartbeat) >= warningThreshold:
						status.Status = "warning"
						warningCount++
					default:
						status.Status = "active"
						activeCount++
					}
				} else {
					status.Status = "warning"
					warningCount++
				}
			}

			subtasks = append(subtasks, status)
		}

		summary := fmt.Sprintf("%d active, %d warning, %d stalled, %d completed, %d total",
			activeCount, warningCount, stalledCount, completedCount, len(cards))

		return nil, checkAgentHealthOutput{
			ParentID:       input.ParentID,
			TimeoutSeconds: int64(timeout.Seconds()),
			WarningSeconds: int64(warningThreshold.Seconds()),
			Subtasks:       subtasks,
			Summary:        summary,
		}, nil
	})
}

func registerGetReadyTasks(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ready_tasks",
		Description: "Get unclaimed 'todo' cards that are ready to start — all depends_on cards are in 'done' state. Optionally scoped to a parent card's subtasks. Use this to find which tasks can be started in parallel.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getReadyTasksInput) (*mcp.CallToolResult, getReadyTasksOutput, error) {
		filter := storage.CardFilter{State: board.StateTodo}
		if input.ParentID != "" {
			filter.Parent = input.ParentID
		}

		cards, err := svc.ListCards(ctx, input.Project, filter)
		if err != nil {
			return nil, getReadyTasksOutput{}, fmt.Errorf("list todo cards: %w", err)
		}

		// Filter to unclaimed cards with all dependencies met
		// ListCards already computes DependenciesMet on each card
		ready := make([]*board.Card, 0)

		for _, card := range cards {
			if card.AssignedAgent != "" {
				continue // already claimed
			}

			if card.DependenciesMet != nil && !*card.DependenciesMet {
				continue
			}

			if card.Source != nil && !card.Vetted {
				continue // unvetted external cards cannot be claimed by agents
			}

			ready = append(ready, card)
		}

		return nil, getReadyTasksOutput{Cards: ready}, nil
	})
}

type reportUsageInput struct {
	Project          string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID           string `json:"card_id" jsonschema:"required,card ID"`
	AgentID          string `json:"agent_id" jsonschema:"required,agent ID reporting usage"`
	Model            string `json:"model,omitempty" jsonschema:"model name for cost calculation (e.g. claude-sonnet-4)"`
	PromptTokens     int64  `json:"prompt_tokens" jsonschema:"required,number of prompt tokens used"`
	CompletionTokens int64  `json:"completion_tokens" jsonschema:"required,number of completion tokens used"`
}

func registerReportUsage(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_usage",
		Description: "Report token usage for a card. Increments running totals of prompt and completion tokens, and recalculates estimated cost based on the model's configured rates. Call this on heartbeat and when completing a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportUsageInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.ReportUsage(ctx, project, input.CardID, service.ReportUsageInput{
			AgentID:          input.AgentID,
			Model:            input.Model,
			PromptTokens:     input.PromptTokens,
			CompletionTokens: input.CompletionTokens,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("report usage for %s: %w", input.CardID, err)
		}

		return nil, card, nil
	})
}

type recalculateCostsInput struct {
	Project      string `json:"project" jsonschema:"required,project name"`
	DefaultModel string `json:"default_model" jsonschema:"required,model name used when card has no stored model (e.g. claude-sonnet-4-6)"`
}

type recalculateCostsOutput struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

func registerRecalculateCosts(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recalculate_costs",
		Description: "Recompute estimated costs for cards that have non-zero token counts but $0 cost (e.g. because model was not specified when usage was reported). Only updates cards that qualify; cards with an existing cost are not modified.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input recalculateCostsInput) (*mcp.CallToolResult, recalculateCostsOutput, error) {
		result, err := svc.RecalculateCosts(ctx, input.Project, input.DefaultModel)
		if err != nil {
			return nil, recalculateCostsOutput{}, fmt.Errorf("recalculate costs: %w", err)
		}

		return nil, recalculateCostsOutput{
			CardsUpdated:          result.CardsUpdated,
			TotalCostRecalculated: result.TotalCostRecalculated,
		}, nil
	})
}

// --- Project management tools ---

type createProjectToolInput struct {
	Name        string              `json:"name" jsonschema:"required,project name (alphanumeric with hyphens/underscores)"`
	Prefix      string              `json:"prefix" jsonschema:"required,card ID prefix (e.g. ALPHA)"`
	Repo        string              `json:"repo,omitempty" jsonschema:"git repository URL for the project code"`
	States      []string            `json:"states" jsonschema:"required,workflow states (must include stalled)"`
	Types       []string            `json:"types" jsonschema:"required,card types (e.g. task bug feature)"`
	Priorities  []string            `json:"priorities" jsonschema:"required,priority levels (e.g. low medium high)"`
	Transitions map[string][]string `json:"transitions" jsonschema:"required,state transition rules mapping each state to allowed target states"`
}

type updateProjectToolInput struct {
	Project     string              `json:"project" jsonschema:"required,project name to update"`
	Repo        string              `json:"repo,omitempty" jsonschema:"git repository URL"`
	States      []string            `json:"states" jsonschema:"required,workflow states (must include stalled)"`
	Types       []string            `json:"types" jsonschema:"required,card types"`
	Priorities  []string            `json:"priorities" jsonschema:"required,priority levels"`
	Transitions map[string][]string `json:"transitions" jsonschema:"required,state transition rules"`
}

type deleteProjectToolInput struct {
	Project string `json:"project" jsonschema:"required,project name to delete (must have zero cards)"`
}

type deleteProjectOutput struct {
	Deleted bool `json:"deleted"`
}

func registerCreateProject(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_project",
		Description: "Create a new project board with directory structure and configuration. The project name becomes the directory name. States must include 'stalled'. All states must have transition entries.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createProjectToolInput) (*mcp.CallToolResult, *board.ProjectConfig, error) {
		cfg, err := svc.CreateProject(ctx, service.CreateProjectInput{
			Name:        input.Name,
			Prefix:      input.Prefix,
			Repo:        input.Repo,
			States:      input.States,
			Types:       input.Types,
			Priorities:  input.Priorities,
			Transitions: input.Transitions,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create project %s: %w", input.Name, err)
		}

		return nil, cfg, nil
	})
}

func registerUpdateProject(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_project",
		Description: "Update a project's configuration. Cannot change name or prefix. Cannot remove states, types, or priorities that are currently in use by cards.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updateProjectToolInput) (*mcp.CallToolResult, *board.ProjectConfig, error) {
		cfg, err := svc.UpdateProject(ctx, input.Project, service.UpdateProjectInput{
			Repo:        input.Repo,
			States:      input.States,
			Types:       input.Types,
			Priorities:  input.Priorities,
			Transitions: input.Transitions,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("update project %s: %w", input.Project, err)
		}

		return nil, cfg, nil
	})
}

func registerDeleteProject(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_project",
		Description: "Delete a project. The project must have zero cards — delete all cards first. Removes the project directory and configuration.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input deleteProjectToolInput) (*mcp.CallToolResult, deleteProjectOutput, error) {
		if err := svc.DeleteProject(ctx, input.Project); err != nil {
			return nil, deleteProjectOutput{}, fmt.Errorf("delete project %s: %w", input.Project, err)
		}

		return nil, deleteProjectOutput{Deleted: true}, nil
	})
}

// --- start_workflow tool ---

type startWorkflowInput struct {
	CardID          string `json:"card_id" jsonschema:"required,card ID to start the workflow for (e.g. ALPHA-001)"`
	IncludePreamble *bool  `json:"include_preamble,omitempty" jsonschema:"include workflow rules preamble (default true, pass false to skip on subsequent calls when you already have it)"`
}
type startWorkflowOutput struct {
	SkillName string `json:"skill_name"`
	Model     string `json:"model,omitempty"`
	Content   string `json:"content"`
	Inline    bool   `json:"inline,omitempty"`
}

func registerStartWorkflow(server *mcp.Server, svc *service.CardService, workflowSkillsDir string) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "start_workflow",
		Description: "Start the workflow for a card. Call this when a user asks to " +
			"'start workflow', 'start', 'plan', 'work on', 'begin', or 'run' a card. " +
			"Inspects the card's autonomous flag and returns the full workflow skill content: " +
			"run-autonomous (for autonomous cards) or create-plan (for human-in-the-loop cards). " +
			"Always returns inline: true — execute the content directly.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input startWorkflowInput) (*mcp.CallToolResult, startWorkflowOutput, error) {
		card, _, err := findCard(ctx, svc, input.CardID)
		if err != nil {
			return nil, startWorkflowOutput{}, fmt.Errorf("start workflow: %w", err)
		}

		skill := "create-plan"
		if card.Autonomous {
			skill = "run-autonomous"
		}

		includePreamble := input.IncludePreamble == nil || *input.IncludePreamble

		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, skill, skillArgs{
			CardID: input.CardID,
		}, includePreamble)
		if err != nil {
			return nil, startWorkflowOutput{}, fmt.Errorf("start workflow: %w", err)
		}

		content := stripAgentConfig(result.Content)

		// start_workflow always returns inline content — both create-plan
		// and run-autonomous are executed directly by the orchestrator.
		content = buildInlineExecutionPrompt(content, input.CardID, skill)

		return nil, startWorkflowOutput{
			SkillName: skill,
			Content:   content,
			Inline:    true,
		}, nil
	})
}

type getSkillInput struct {
	SkillName       string `json:"skill_name" jsonschema:"required,skill name: create-task, create-plan, execute-task, review-task, document-task, init-project, brainstorming"`
	CardID          string `json:"card_id,omitempty" jsonschema:"card ID (required for create-plan, execute-task, review-task, document-task, brainstorming)"`
	Description     string `json:"description,omitempty" jsonschema:"free-text description (used by create-task)"`
	Name            string `json:"name,omitempty" jsonschema:"project name (used by init-project)"`
	CallerModel     string `json:"caller_model,omitempty" jsonschema:"your model family (opus, sonnet, haiku) — enables inline execution when matching the skill model"`
	IncludePreamble *bool  `json:"include_preamble,omitempty" jsonschema:"include workflow rules preamble (default true, pass false to skip on subsequent calls when you already have it)"`
}
type getSkillOutput struct {
	SkillName string `json:"skill_name"`
	Model     string `json:"model,omitempty"`
	Content   string `json:"content"`
	Inline    bool   `json:"inline,omitempty"`
}

func registerGetSkill(server *mcp.Server, svc *service.CardService, workflowSkillsDir string) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_skill",
		Description: "Get a skill prompt with injected card/project context. Returns the full skill instructions, " +
			"plus a 'model' field indicating which model to use when spawning a sub-agent (e.g. 'sonnet', 'opus'). " +
			"When the response has 'inline: true', you MAY execute the content directly instead of spawning a sub-agent — " +
			"the content already includes lifecycle enforcement instructions. " +
			"When 'inline' is false or absent, you MUST spawn a sub-agent via the Agent tool with the returned model.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSkillInput) (*mcp.CallToolResult, getSkillOutput, error) {
		includePreamble := input.IncludePreamble == nil || *input.IncludePreamble

		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, input.SkillName, skillArgs{
			CardID:      input.CardID,
			Description: input.Description,
			Name:        input.Name,
		}, includePreamble)
		if err != nil {
			return nil, getSkillOutput{}, fmt.Errorf("get skill %s: %w", input.SkillName, err)
		}

		content := stripAgentConfig(result.Content)

		// Server-side inline decision: caller model must match skill model
		// AND the skill must be on the inline-eligible whitelist.
		// normalizeModelFamily handles both short names ("opus") and full
		// model IDs ("claude-opus-4-6") that agents may pass.
		callerFamily := normalizeModelFamily(input.CallerModel)
		canInline := callerFamily != "" &&
			strings.EqualFold(callerFamily, result.Model) &&
			isInlineEligible(input.SkillName)

		if canInline {
			content = buildInlineExecutionPrompt(content, input.CardID, input.SkillName)
		}

		return nil, getSkillOutput{
			SkillName: input.SkillName,
			Model:     result.Model,
			Content:   content,
			Inline:    canInline,
		}, nil
	})
}

// --- report_push tool ---

type reportPushInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Branch  string `json:"branch" jsonschema:"required,git branch that was pushed to"`
	PRUrl   string `json:"pr_url,omitempty" jsonschema:"pull request URL if created"`
}

type reportPushOutput struct {
	Card *board.Card `json:"card"`
}

func registerIncrementReviewAttempts(server *mcp.Server, svc *service.CardService) {
	type input struct {
		Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
		CardID  string `json:"card_id" jsonschema:"required,card ID"`
		AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	}

	type output struct {
		Card *board.Card `json:"card"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "increment_review_attempts",
		Description: "Increment the review_attempts counter on a card. Used during autonomous review cycles " +
			"to track how many times a card has been reviewed. The counter determines when to halt " +
			"autonomous processing and escalate to a human (typically at 2 attempts).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, output, error) {
		project, err := resolveProject(ctx, svc, in.Project, in.CardID)
		if err != nil {
			return nil, output{}, err
		}

		card, err := svc.IncrementReviewAttempts(ctx, project, in.CardID, in.AgentID)
		if err != nil {
			return nil, output{}, fmt.Errorf("increment review attempts: %w", err)
		}

		return nil, output{Card: card}, nil
	})
}

func registerReportPush(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_push",
		Description: "Report a completed git push. Call this AFTER pushing to record the branch and " +
			"optional PR URL on the card. Returns a hard error if the branch is main or master.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportPushInput) (*mcp.CallToolResult, reportPushOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, reportPushOutput{}, err
		}

		branch := strings.TrimSpace(input.Branch)

		card, err := svc.RecordPush(ctx, project, input.CardID, input.AgentID, branch, input.PRUrl)
		if err != nil {
			return nil, reportPushOutput{}, fmt.Errorf("report push: %w", err)
		}

		return nil, reportPushOutput{Card: card}, nil
	})
}

// --- promote_to_autonomous tool ---

type promoteToAutonomousInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID performing the promotion"`
}

func registerPromoteToAutonomous(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "promote_to_autonomous",
		Description: "Promote a card to autonomous mode by flipping its autonomous flag to true. " +
			"Human-only: agent_id must start with \"human:\" or the call is rejected. " +
			"Idempotent: calling on an already-autonomous card is a no-op. " +
			"Returns an error if the card is in a terminal state (done/not_planned). " +
			"Appends an activity log entry and fires an SSE event so the UI updates live.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input promoteToAutonomousInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.PromoteToAutonomous(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("promote card %s to autonomous: %w", input.CardID, err)
		}

		return nil, card, nil
	})
}
