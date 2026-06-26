package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

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

// --- start_review tool ---

type startReviewInput struct {
	Project         string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID          string `json:"card_id" jsonschema:"required,parent card ID to enter review (e.g. ALPHA-001)"`
	AgentID         string `json:"agent_id" jsonschema:"required,agent performing the transition — must own the card claim"`
	CallerModel     string `json:"caller_model,omitempty" jsonschema:"your model family (opus, sonnet, haiku) — enables inline execution when matching the skill model"`
	IncludePreamble *bool  `json:"include_preamble,omitempty" jsonschema:"include workflow rules preamble (default true, pass false to skip on subsequent calls when you already have it)"`
}

func registerStartReview(server *mcp.Server, svc *service.CardService, workflowSkillsDir string) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "start_review",
		Description: "Atomically transition a parent card to 'review' and return the review-task skill. " +
			"Replaces the two-call pattern transition_card + get_skill('review-task') — there is no way to " +
			"load the review skill without committing the transition. Caller must own the card claim " +
			"(agent_id is required and is verified against the assigned agent). Returns the same shape as " +
			"get_skill (skill_name, model, content, inline). If the transition fails (forbidden state, " +
			"agent ownership mismatch, card not found), the skill is not loaded and the call returns an error.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input startReviewInput) (*mcp.CallToolResult, getSkillOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, getSkillOutput{}, err
		}

		newState := board.StateReview

		patchInput := service.PatchCardInput{
			AgentID: input.AgentID,
			State:   &newState,
		}

		if _, err := svc.PatchCard(ctx, project, input.CardID, patchInput); err != nil {
			return nil, getSkillOutput{}, fmt.Errorf("start review for %s: %w", input.CardID, err)
		}

		includePreamble := input.IncludePreamble == nil || *input.IncludePreamble

		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, "review-task", skillArgs{
			CardID: input.CardID,
		}, includePreamble)
		if err != nil {
			return nil, getSkillOutput{}, fmt.Errorf("start review %s: load skill: %w", input.CardID, err)
		}

		content := stripAgentConfig(result.Content)

		// review-task always runs inline regardless of caller_model. The
		// review skill spawns three specialist sub-agents in parallel via
		// the Agent tool, which is only available to the top-level (calling)
		// session. Running review-task as a spawned sub-agent silently
		// degrades to a single-perspective review because spawned sub-agents
		// lack Agent. Keep this gate-free — do not reintroduce model match.
		content = buildInlineExecutionPrompt(content, input.CardID, "review-task")

		return nil, getSkillOutput{
			SkillName: "review-task",
			Model:     result.Model,
			Content:   content,
			Inline:    true,
		}, nil
	})
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
