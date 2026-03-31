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
func registerTools(server *mcp.Server, svc *service.CardService, skillsDir string) {
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
	registerReportUsage(server, svc)
	registerCreateProject(server, svc)
	registerUpdateProject(server, svc)
	registerDeleteProject(server, svc)
	registerGetSkill(server, svc, skillsDir)
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
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
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
	Project  string   `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string   `json:"card_id" jsonschema:"required,card ID"`
	Title    *string  `json:"title,omitempty" jsonschema:"new title"`
	Priority *string  `json:"priority,omitempty" jsonschema:"new priority"`
	Labels   []string `json:"labels,omitempty" jsonschema:"new labels (replaces all)"`
	Body     *string  `json:"body,omitempty" jsonschema:"new markdown body"`
}

type transitionCardInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string `json:"agent_id,omitempty" jsonschema:"agent performing the transition"`
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
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}
		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card %s: %w", input.CardID, err)
		}
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
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}
		patchInput := service.PatchCardInput{
			Title:    input.Title,
			Priority: input.Priority,
			Labels:   input.Labels,
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
		patchInput := service.PatchCardInput{
			State: &input.NewState,
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
		// Auto-transition to in_progress if possible
		if card.State != "in_progress" {
			if transitioned, err := svc.TransitionTo(ctx, project, input.CardID, "in_progress"); err == nil {
				card = transitioned
			}
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
		return nil, nil, nil
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
			Card:   card,
			Config: cfg,
		}

		// Load parent if set
		if card.Parent != "" {
			parent, err := svc.GetCard(ctx, project, card.Parent)
			if err == nil {
				out.Parent = parent
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
				out.Siblings = filtered
			}
		}

		return nil, out, nil
	})
}

func registerCompleteTask(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Atomically complete a task: adds a completion log entry, walks through required state transitions, and releases the claim. Subtasks (cards with a parent) transition to 'done'. Main tasks (no parent) transition to 'review' for the review workflow. When a main task transitions to review, the response includes a next_step field with instructions to invoke the review-task skill — follow it. Use this instead of separate add_log + transition_card calls.",
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
		targetState := "review"
		if card.Parent != "" {
			targetState = "done"
		}

		// Walk through intermediate transitions to reach target state
		if _, err := svc.TransitionTo(ctx, project, input.CardID, targetState); err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("transition to %s: %w", targetState, err)
		}

		// Release the claim
		card, err = svc.ReleaseCard(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, completeTaskOutput{}, fmt.Errorf("release card: %w", err)
		}

		out := completeTaskOutput{Card: card}
		if targetState == "review" {
			out.NextStep = fmt.Sprintf(
				"LIFECYCLE: Card %s is now in 'review'. You MUST spawn a sub-agent for review. "+
					"Call get_skill(skill_name='review-task', card_id='%s') — it returns a 'model' field (e.g. 'opus'). "+
					"Use the Agent tool with that model and the returned content as the prompt. Do NOT stop here.",
				input.CardID, input.CardID,
			)
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
		// ListCards already computes DependenciesMet on each card
		ready := make([]*board.Card, 0)
		for _, card := range cards {
			if card.AssignedAgent != "" {
				continue // already claimed
			}
			if card.DependenciesMet != nil && !*card.DependenciesMet {
				continue
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

type getSkillInput struct {
	SkillName   string `json:"skill_name" jsonschema:"required,skill name: create-task, create-plan, execute-task, review-task, document-task, init-project"`
	CardID      string `json:"card_id,omitempty" jsonschema:"card ID (required for create-plan, execute-task, review-task, document-task)"`
	Description string `json:"description,omitempty" jsonschema:"free-text description (used by create-task)"`
	Name        string `json:"name,omitempty" jsonschema:"project name (used by init-project)"`
}
type getSkillOutput struct {
	SkillName string `json:"skill_name"`
	Model     string `json:"model,omitempty"`
	Content   string `json:"content"`
}

func registerGetSkill(server *mcp.Server, svc *service.CardService, skillsDir string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_skill",
		Description: "Get a skill prompt with injected card/project context. Returns the full skill instructions, plus a 'model' field indicating which model to use when spawning a sub-agent (e.g. 'sonnet', 'opus'). Use the Agent tool with the returned model and content to spawn the right agent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSkillInput) (*mcp.CallToolResult, getSkillOutput, error) {
		result, err := buildSkillContent(ctx, svc, skillsDir, input.SkillName, skillArgs{
			CardID:      input.CardID,
			Description: input.Description,
			Name:        input.Name,
		})
		if err != nil {
			return nil, getSkillOutput{}, fmt.Errorf("get skill %s: %w", input.SkillName, err)
		}
		return nil, getSkillOutput{SkillName: input.SkillName, Model: result.Model, Content: result.Content}, nil
	})
}
