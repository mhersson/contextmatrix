package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
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
	Project       string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID        string `json:"card_id" jsonschema:"required,card ID (e.g. ALPHA-001)"`
	AgentID       string `json:"agent_id,omitempty" jsonschema:"caller identity — unvetted external card bodies are redacted for non-human callers"`
	IncludeImages *bool  `json:"include_images,omitempty" jsonschema:"attach inline image bytes for cm-server-hosted markdown image references in the body (default true; capped at 10 images per call and ~20 MiB cumulative bytes, with later references in body order omitted when over budget)"`
}

type createCardInput struct {
	Project   string    `json:"project" jsonschema:"required,project name"`
	Title     string    `json:"title" jsonschema:"required,card title"`
	Type      string    `json:"type" jsonschema:"required,card type (task/bug/feature). Overridden to 'subtask' when parent is set."`
	Priority  string    `json:"priority" jsonschema:"required,priority (low/medium/high/critical)"`
	Labels    []string  `json:"labels,omitempty" jsonschema:"optional labels"`
	Skills    *[]string `json:"skills,omitempty" jsonschema:"optional task-skill names to mount in the worker container; nil inherits from parent or project default, [] means none, [list] constrains"`
	Body      string    `json:"body,omitempty" jsonschema:"optional markdown body"`
	Parent    string    `json:"parent,omitempty" jsonschema:"parent card ID for subtasks"`
	DependsOn []string  `json:"depends_on,omitempty" jsonschema:"card IDs this depends on"`
	// AgentID is accepted for parity with the other card tools: the agent MCP
	// client injects agent_id into every call, so create_card must declare it or
	// the strict (additionalProperties:false) schema rejects the orchestrator's
	// subtask creation. Not threaded to attribution today (the service has no
	// author param); present so the call validates.
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity (accepted for client parity; not used for attribution)"`
}

// NOTE: vetted, autonomous, feature_branch, create_pr, base_branch, best_of_n,
// the mob session fields (mob_participants, mob_phases, mob_guests), and model pin
// fields (model_orchestrator, model_coder, model_reviewer) are intentionally
// excluded — they are human-only fields. Model pins are excluded for the same
// reason: they express human intent about which model to use and must not be
// overridden by the agent that is itself subject to the pin.
type updateCardInput struct {
	Project  string    `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string    `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string    `json:"agent_id,omitempty" jsonschema:"agent performing the update — if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	Title    *string   `json:"title,omitempty" jsonschema:"new title"`
	Priority *string   `json:"priority,omitempty" jsonschema:"new priority"`
	Labels   []string  `json:"labels,omitempty" jsonschema:"new labels (replaces all)"`
	Skills   *[]string `json:"skills,omitempty" jsonschema:"new task skills (replaces all); [] means none, omit to leave unchanged"`
	Body     *string   `json:"body,omitempty" jsonschema:"new markdown body"`
	Phase    *string   `json:"phase,omitempty" jsonschema:"orchestrator phase: plan|execute|judge|document|review|integrate|done; empty clears"`
}

type transitionCardInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string `json:"agent_id,omitempty" jsonschema:"agent performing the transition — if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	NewState string `json:"new_state" jsonschema:"required,target state"`
}

type getTaskContextInput struct {
	Project       string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID        string `json:"card_id" jsonschema:"required,card ID"`
	AgentID       string `json:"agent_id,omitempty" jsonschema:"caller identity — unvetted external card bodies are redacted for non-human callers"`
	IncludeImages *bool  `json:"include_images,omitempty" jsonschema:"attach inline image bytes for cm-server-hosted markdown image references in the primary card body (default true; capped at 10 images per call and ~20 MiB cumulative bytes, with later references in body order omitted when over budget; siblings stay text-only)"`
}
type getTaskContextOutput struct {
	Card     *board.Card          `json:"card"`
	Parent   *board.Card          `json:"parent,omitempty"`
	Siblings []*board.Card        `json:"siblings,omitempty"`
	Config   *board.ProjectConfig `json:"config"`
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

type reportUsageInput struct {
	Project             string   `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID              string   `json:"card_id" jsonschema:"required,card ID"`
	AgentID             string   `json:"agent_id" jsonschema:"required,agent ID reporting usage"`
	Model               string   `json:"model,omitempty" jsonschema:"model name for cost calculation (e.g. claude-sonnet-4)"`
	PromptTokens        int64    `json:"prompt_tokens" jsonschema:"required,number of prompt tokens used"`
	CompletionTokens    int64    `json:"completion_tokens" jsonschema:"required,number of completion tokens used"`
	CacheReadTokens     int64    `json:"cache_read_tokens,omitempty" jsonschema:"number of cache-read tokens (billed at 0.10× base input rate)"`
	CacheCreationTokens int64    `json:"cache_creation_tokens,omitempty" jsonschema:"number of cache-creation tokens (billed at 1.25× base input rate)"`
	ActualCostUSD       *float64 `json:"actual_cost_usd,omitempty" jsonschema:"authoritative provider-reported cost in USD for this delta; omit to use the server rate table"`
}

type recalculateCostsInput struct {
	Project      string `json:"project" jsonschema:"required,project name"`
	DefaultModel string `json:"default_model" jsonschema:"required,model name used when card has no stored model (e.g. claude-sonnet-4-6)"`
}

type recalculateCostsOutput struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

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

type promoteToAutonomousInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID performing the promotion"`
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

func registerGetCard(server *mcp.Server, svc *service.CardService, imageStore images.Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_card",
		Description: "Get a single card by ID, including its full body and metadata. By default, attaches inline image bytes for any cm-server-hosted markdown images in the body (capped at 10); pass include_images=false to skip. Cumulative attached image bytes are capped at ~20 MiB; later references in body order are omitted when over budget.",
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

		result := attachImagesToResult(ctx, imageStore,
			attachContext{Tool: "get_card", CardID: card.ID},
			card, card.Body, input.IncludeImages, 0,
		)

		return result, card, nil
	})
}

func registerCreateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a new card in a project. Returns the created card with its generated ID. The card starts in the project's first state (usually 'todo'). IMPORTANT: After creation, the card must be claimed with claim_card before any work begins. Never start working on a card without claiming it first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createCardInput) (*mcp.CallToolResult, *board.Card, error) {
		// depends_on is part of CreateCardInput so create + dependency wiring
		// happen as a single atomic operation (one git commit, no race window
		// between create and follow-up update).
		svcInput := service.CreateCardInput{
			Title:     input.Title,
			Type:      input.Type,
			Priority:  input.Priority,
			Labels:    input.Labels,
			Skills:    input.Skills,
			Body:      input.Body,
			Parent:    input.Parent,
			DependsOn: input.DependsOn,
		}

		card, err := svc.CreateCard(ctx, input.Project, svcInput)
		if err != nil {
			return nil, nil, fmt.Errorf("create card: %w", err)
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
			Phase:    input.Phase,
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

		// Agent-ownership is enforced inside PatchCard via AgentID.
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

func registerGetTaskContext(server *mcp.Server, svc *service.CardService, imageStore images.Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_context",
		Description: "Get a card with its parent card, sibling cards (same parent), and project config in a single call. Sub-agents should call this first before touching anything — it eliminates multiple round-trips. By default, attaches inline image bytes for any cm-server-hosted markdown images in the primary card body (capped at 10); pass include_images=false to skip. Sibling card bodies stay text-only. Cumulative attached image bytes are capped at ~20 MiB; later references in body order are omitted when over budget.",
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

		// Redact unvetted card body for non-human callers — get_task_context
		// is the primary prompt-injection vector because its response is fed
		// straight into agent context.
		primary := redactCardForAgent(card, input.AgentID)

		out := getTaskContextOutput{
			Card:   primary,
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

		result := attachImagesToResult(ctx, imageStore,
			attachContext{Tool: "get_task_context", CardID: primary.ID},
			out, primary.Body, input.IncludeImages, 0,
		)

		return result, out, nil
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
		now := svc.Now()

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

func registerReportUsage(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_usage",
		Description: "Report token usage for a card. Increments running totals of prompt and completion tokens, " +
			"and recalculates estimated cost based on the model's configured rates. " +
			"Accepts optional cache_read_tokens (billed at 0.10× base input rate) and " +
			"cache_creation_tokens (billed at 1.25× base input rate) for prompt-cache cost accounting. " +
			"Call this on heartbeat and when completing a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportUsageInput) (*mcp.CallToolResult, *board.Card, error) {
		// Reject negative token counts at the handler boundary. The service
		// layer uses += on the running totals, so a negative value would
		// silently decrement counters and produce nonsensical totals.
		if input.PromptTokens < 0 || input.CompletionTokens < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: tokens must be non-negative (prompt_tokens=%d, completion_tokens=%d)",
				input.CardID, input.PromptTokens, input.CompletionTokens)
		}

		if input.CacheReadTokens < 0 || input.CacheCreationTokens < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: cache tokens must be non-negative (cache_read_tokens=%d, cache_creation_tokens=%d)",
				input.CardID, input.CacheReadTokens, input.CacheCreationTokens)
		}

		if input.ActualCostUSD != nil && *input.ActualCostUSD < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: actual cost must be non-negative (actual_cost_usd=%v)",
				input.CardID, *input.ActualCostUSD)
		}

		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.ReportUsage(ctx, project, input.CardID, service.ReportUsageInput{
			AgentID:             input.AgentID,
			Model:               input.Model,
			PromptTokens:        input.PromptTokens,
			CompletionTokens:    input.CompletionTokens,
			CacheReadTokens:     input.CacheReadTokens,
			CacheCreationTokens: input.CacheCreationTokens,
			ActualCostUSD:       input.ActualCostUSD,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("report usage for %s: %w", input.CardID, err)
		}

		return nil, card, nil
	})
}

func registerRecalculateCosts(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recalculate_costs",
		Description: "Recompute estimated costs from the current rate table. Cards with a usage breakdown: every estimated bucket is re-priced (stale prices corrected); actual provider-reported costs are never modified. Legacy cards without a breakdown: fill-missing-only — cards with non-zero tokens but $0 cost get a cost, cards with an existing cost are not modified.",
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

// --- report_push tool ---

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

func registerPromoteToAutonomous(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "promote_to_autonomous",
		Description: "Promote a card to autonomous mode by flipping its autonomous flag to true. " +
			"Human-only: agent_id must start with \"human:\" or the call is rejected. " +
			"Idempotent: calling on an already-autonomous card is a no-op. " +
			"Returns an error if the card is in a terminal state (done/not_planned). " +
			"Appends an activity log entry and fires an SSE event so the UI updates live.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input promoteToAutonomousInput) (*mcp.CallToolResult, *board.Card, error) {
		// Defence in depth: the service layer rejects non-human callers, but
		// gate at the handler boundary too so the rejection style matches the
		// other human-only tools and the error never depends on project
		// resolution succeeding first.
		if err := requireHumanAgent(input.AgentID, "promote_to_autonomous"); err != nil {
			return nil, nil, err
		}

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
