package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// workflowPreamble is prepended to every skill prompt to enforce card lifecycle
// discipline regardless of which skill is active.
const workflowPreamble = `## ContextMatrix Workflow Rules

IMPORTANT: These rules apply to ALL interactions with the ContextMatrix board.
Violating these rules leaves cards orphaned with no tracking. Follow them exactly.

- **Never work on a card without claiming it first.** Before writing any code or
  making any changes for a card, you MUST call claim_card (or use the execute-task
  skill which handles this automatically). Working without claiming leaves the card
  orphaned — no tracking, no heartbeats, no completion record.
- **Follow the full lifecycle to completion: claim → work → heartbeat → complete.**
  Every card you work on must go through this entire sequence. Call heartbeat
  periodically during work. Call complete_task when done. Do NOT stop after making
  code changes — the lifecycle ends when complete_task is called, not when the code
  is written.
- **Never stop mid-lifecycle.** Do NOT ask the user to commit, review your diff,
  or approve your changes instead of completing the card lifecycle. Do NOT abandon
  a card after coding. If you claimed it, you must either complete it or report it
  as blocked.
- **Heartbeat during idle waits.** If you are waiting for user input, waiting for
  a sub-agent to complete, or otherwise idle for more than a few minutes, call
  heartbeat every 5 minutes to prevent your claim from going stale. Idle waits
  are the most common cause of stalled cards.
- **Always use MCP tools for ContextMatrix interactions.** For all board
  operations (claiming cards, sending heartbeats, updating cards, completing
  tasks, etc.), ALWAYS use the provided MCP tools. NEVER use curl, wget, REST
  API calls, or any direct HTTP approach. The MCP tools are the only supported
  interface for agent operations.
- **When in doubt, use /contextmatrix:execute-task <card_id>.** It handles the
  entire lifecycle for you.

---

`

// skillResult holds the assembled skill content and parsed metadata.
type skillResult struct {
	Content string
	Model   string
}

// modelPattern matches "**Model:** claude-<family>-<version>" in skill files.
var modelPattern = regexp.MustCompile(`\*\*Model:\*\*\s+claude-(\w+)-`)


// agentConfigPattern matches the full "## Agent Configuration" section
// (from the heading through the "---" separator) so it can be stripped
// from the content delivered to agents.
var agentConfigPattern = regexp.MustCompile(`(?s)## Agent Configuration\n.*?---\n+`)

// parseSkillModel extracts the short model name (sonnet, opus, haiku) from
// a skill file's "## Agent Configuration" section.
func parseSkillModel(content string) string {
	m := modelPattern.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}


// stripAgentConfig removes the "## Agent Configuration" section from a skill
// file. This section is metadata for the orchestrator, not instructions for
// the agent executing the skill.
func stripAgentConfig(content string) string {
	return agentConfigPattern.ReplaceAllString(content, "")
}

// callerModelPattern matches "claude-<family>-<version>" in caller_model input.
var callerModelPattern = regexp.MustCompile(`(?i)^claude-(\w+)-`)

// normalizeModelFamily extracts the short model family (opus, sonnet, haiku)
// from either a full model ID like "claude-opus-4-6" or returns the input
// unchanged if it is already a short family name like "opus".
func normalizeModelFamily(model string) string {
	m := callerModelPattern.FindStringSubmatch(model)
	if len(m) >= 2 {
		return strings.ToLower(m[1])
	}
	return model
}

// inlineEligibleSkills lists skills that the server may return with inline
// execution enabled when the caller's model matches the skill's model.
var inlineEligibleSkills = map[string]bool{
	"review-task": true,
	"create-plan": true,
}

// isInlineEligible reports whether a skill supports inline execution.
func isInlineEligible(skillName string) bool {
	return inlineEligibleSkills[skillName]
}

// buildInlineExecutionPrompt wraps skill content in a lifecycle-enforcing
// envelope for inline execution. This is structurally different from raw
// content delivery — the lifecycle gates are entry/exit conditions that
// frame the skill instructions, not a prepended suggestion.
func buildInlineExecutionPrompt(content, cardID, skillName string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## INLINE EXECUTION — Lifecycle Checkpoints Required")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "You are executing **%s** for card **%s** inline.\n", skillName, cardID)
	fmt.Fprintln(&b, "YOU are responsible for the full card lifecycle. These steps are MANDATORY:")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **BEFORE** any work: `claim_card(card_id='%s', agent_id=<your_agent_id>)`\n", cardID)
	fmt.Fprintln(&b, "- **DURING** work: call `heartbeat` every 5 minutes")
	fmt.Fprintf(&b, "- **AFTER** work: `release_card` or `complete_task` as instructed below, then call `report_usage`\n")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Skipping lifecycle steps leaves cards orphaned. The board detects this")
	fmt.Fprintln(&b, "and marks the card as stalled after 30 minutes.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "--- BEGIN SKILL INSTRUCTIONS ---")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, content)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "--- END SKILL INSTRUCTIONS ---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "MANDATORY: After completing the skill instructions above, verify the")
	fmt.Fprintf(&b, "card state is correct by calling `get_card(card_id='%s')`.\n", cardID)
	return b.String()
}

// buildDelegationPrompt returns a short wrapper prompt that instructs the
// receiving agent to call get_skill with the given arguments and then spawn
// a sub-agent via the `Agent` tool with the returned model and content.
//
// This is what MCP prompt handlers return — NOT the raw skill content.
// The delegation wrapper ensures the work runs as a sub-agent on the correct
// model, not inline in the calling agent's context.
func buildDelegationPrompt(model, skillName, getSkillArgs string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Subagent Required: %s\n\n", skillName)
	fmt.Fprintf(&b, "This workflow step must run as a sub-agent on the **%s** model.\n", model)
	fmt.Fprintln(&b, "Do NOT execute it inline — delegation is required.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "**Steps:**")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "1. Call `get_skill(%s)` to retrieve the full skill prompt and the required model.\n", getSkillArgs)
	fmt.Fprintln(&b, "2. Use the **`Agent`** tool to spawn the sub-agent with:")
	fmt.Fprintf(&b, "   - `model`: `\"%s\"` — this is **CRITICAL**, using the wrong model breaks cost/quality\n", model)
	fmt.Fprintf(&b, "   - `description`: `\"%s <card_id>\"`\n", skillName)
	fmt.Fprintln(&b, "   - `prompt`: the full `content` returned by `get_skill`")
	fmt.Fprintln(&b, "3. Wait for the sub-agent to complete and relay its structured output back.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Do NOT read the skill content yourself and execute it — you MUST use the `Agent` tool with model `%s`.\n", model)
	fmt.Fprintf(&b, "Do NOT use SendMessage to spawn sub-agents — use the `Agent` tool.\n")
	fmt.Fprintf(&b, "After spawning, if the sub-agent asks the user a question and the user responds, relay their response to the sub-agent using SendMessage. Always include the `summary` parameter (a brief description of the message).\n")
	return b.String()
}

// buildCreatePlanDelegationPrompt returns a two-phase delegation prompt for
// the create-plan workflow. Unlike the generic buildDelegationPrompt, this
// encodes the two-phase flow explicitly:
//
//   - Phase 1 (plan-drafting): spawn a sub-agent that drafts the plan, writes
//     it to the card body via update_card, and returns a PLAN_DRAFTED structured
//     output immediately — without asking the user for approval or waiting.
//   - User approval: the orchestrator (main Claude, always alive) presents the
//     plan to the user and collects approval directly.
//   - Phase 2 (subtask-creation): once approved, spawn a second short-lived
//     sub-agent that reads the plan from the card body and creates the subtasks.
//
// This eliminates the idle-wait that kills sub-agents when they wait for user
// input between drafting and subtask creation.
func buildCreatePlanDelegationPrompt(cardID, getSkillArgs string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## Planning Workflow")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Planning for card **%s** uses a plan-then-approve flow.\n", cardID)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Plan Drafting — Always Inline")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Run plan drafting **inline** — do not spawn a sub-agent for planning.\n")
	fmt.Fprintln(&b, "The orchestrator retains the plan context for subtask creation.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "1. Call `get_skill(%s, caller_model='<your_model>')` to retrieve the skill prompt.\n", getSkillArgs)
	fmt.Fprintln(&b, "2. Append `\\n\\nYou are executing **Phase 1: Plan Drafting** only.` to the content.")
	fmt.Fprintln(&b, "3. Execute the skill content directly (inline). Follow its instructions to draft the plan.")
	fmt.Fprintln(&b, "4. When you produce the `PLAN_DRAFTED` output, proceed to **User Approval** below.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### User Approval (YOU handle this directly — no sub-agent)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "5. Present the plan directly from what you just drafted:")
	fmt.Fprintln(&b, "   > Here is the proposed plan for [card title]:")
	fmt.Fprintln(&b, "   > [paste the ## Plan section from the card body]")
	fmt.Fprintln(&b, "   > Does this look good, or would you like adjustments?")
	fmt.Fprintln(&b, "6. If the user requests changes: call `get_skill` again, append the feedback and")
	fmt.Fprintln(&b, "   `\\n\\nYou are executing **Phase 1: Plan Drafting** only. The user requested these changes: <feedback>` to the prompt,")
	fmt.Fprintln(&b, "   and execute inline again. Repeat until the user approves.")
	fmt.Fprintln(&b, "7. Once the user approves, proceed to Subtask Creation.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Subtask Creation (inline — you do this directly)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "8. Read the `## Plan` section from the card body (you already have it from drafting).")
	fmt.Fprintln(&b, "9. For each subtask in the plan, call `create_card` with:")
	fmt.Fprintln(&b, "   - `parent`: the parent card ID")
	fmt.Fprintln(&b, "   - `title`, `body`, `priority`, `depends_on` as specified in the plan")
	fmt.Fprintln(&b, "10. After all subtasks are created, release the parent card claim: `release_card(card_id)`.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### After Subtasks Are Created")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "11. Ask the user whether to start executing the subtasks now.")
	fmt.Fprintln(&b, "    - If **yes**: use `get_ready_tasks` and spawn execute-task sub-agents.")
	fmt.Fprintln(&b, "      Call `get_skill(skill_name='execute-task', card_id='<subtask_id>')` for each.")
	fmt.Fprintln(&b, "      Spawn with the returned `model` and `content`. Always spawn as sub-agents")
	fmt.Fprintln(&b, "      for context isolation and parallel execution.")
	fmt.Fprintln(&b, "    - If **no**: let the user know they can run `/contextmatrix:execute-task <card_id>` later.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Do NOT use SendMessage to spawn sub-agents — use the `Agent` tool.")
	return b.String()
}

// buildDocumentTaskDelegationPrompt returns a single-phase "fire-and-report"
// delegation prompt for the document-task workflow. Unlike create-plan's
// two-phase flow, documentation writing requires no human approval gate:
// the sub-agent writes docs directly to disk and returns a DOCS_WRITTEN
// structured output immediately, then the orchestrator presents the summary
// to the user.
//
// Flow:
//   - Spawn one sub-agent that writes docs and returns DOCS_WRITTEN.
//   - Orchestrator parses the output and shows the user what was written.
//   - No Phase 2 needed.
func buildDocumentTaskDelegationPrompt(model, cardID, getSkillArgs string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## Documentation Workflow")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Documenting card **%s** uses a single-phase fire-and-report flow.\n", cardID)
	fmt.Fprintln(&b, "Do NOT execute this inline — delegation to a sub-agent is required.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Step 1: Spawn the documentation sub-agent")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "1. Call `get_skill(%s)` to retrieve the full skill prompt and the required model.\n", getSkillArgs)
	fmt.Fprintln(&b, "2. Spawn a sub-agent using the **`Agent`** tool with:")
	fmt.Fprintf(&b, "   - `model`: `\"%s\"` — **CRITICAL**, do not omit\n", model)
	fmt.Fprintf(&b, "   - `description`: `\"document-task %s\"`\n", cardID)
	fmt.Fprintln(&b, "   - `prompt`: the full `content` returned by `get_skill`")
	fmt.Fprintln(&b, "3. Wait for the sub-agent to complete.")
	fmt.Fprintln(&b, "   The sub-agent writes documentation directly to disk — no approval step needed.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Step 2: Parse structured output and report to user")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "4. Parse the sub-agent's structured output. It will be in this format:")
	fmt.Fprintln(&b, "   ```")
	fmt.Fprintln(&b, "   DOCS_WRITTEN")
	fmt.Fprintf(&b, "   card_id: %s\n", cardID)
	fmt.Fprintln(&b, "   status: written")
	fmt.Fprintln(&b, "   files_written: <list of files written or updated>")
	fmt.Fprintln(&b, "   ```")
	fmt.Fprintln(&b, "5. Present a summary to the user:")
	fmt.Fprintln(&b, "   > Documentation for [card title] has been written.")
	fmt.Fprintln(&b, "   > Files written/updated: [list from files_written]")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Do NOT use SendMessage to spawn sub-agents — use the `Agent` tool with model `%s`.\n", model)
	fmt.Fprintf(&b, "Do NOT execute the skill inline — the sub-agent writes docs and reports back immediately.\n")
	return b.String()
}


// registerPrompts adds all MCP prompts (slash commands) to the server.
func registerPrompts(server *mcp.Server, svc *service.CardService, skillsDir string) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "create-task",
		Description: "Start a guided task creation workflow. Optionally provide a description to seed the conversation.",
		Arguments: []*mcp.PromptArgument{
			{Name: "description", Description: "Optional free-text description of the task to create"},
		},
	}, createTaskPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "create-plan",
		Description: "Create a plan and subtasks for an existing card. Breaks work into subtasks suitable for parallel agent execution.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Card ID to plan (e.g. ALPHA-001)", Required: true},
		},
	}, createPlanPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "execute-task",
		Description: "Claim and execute a task. Used by sub-agents spawned to work on individual subtasks.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Card ID to execute (e.g. ALPHA-003)", Required: true},
		},
	}, executeTaskPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "review-task",
		Description: "Review a completed task and its subtasks. Devils-advocate assessment of work done.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Parent card ID to review (e.g. ALPHA-001)", Required: true},
		},
	}, reviewTaskPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "document-task",
		Description: "Write external documentation for a completed task. Produces README updates, API docs, architecture notes as needed.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Parent card ID to document (e.g. ALPHA-001)", Required: true},
		},
	}, documentTaskPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "init-project",
		Description: "Initialize a new ContextMatrix project board for the current repository. Auto-detects repo URL and derives project name.",
		Arguments: []*mcp.PromptArgument{
			{Name: "name", Description: "Optional project name (auto-detected from repo if omitted)"},
		},
	}, initProjectPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "run-autonomous",
		Description: "Run a card through its full lifecycle autonomously. Picks up from current state and chains all remaining phases.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Card ID to run autonomously (e.g. ALPHA-001)", Required: true},
		},
	}, runAutonomousPromptHandler(svc, skillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "start-workflow",
		Description: "Start the workflow for a card. Automatically routes to run-autonomous for autonomous cards or create-plan for human-in-the-loop cards.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Card ID to start the workflow for (e.g. ALPHA-001)", Required: true},
		},
	}, startWorkflowPromptHandler(svc, skillsDir))
}

// createTaskPromptHandler returns the handler for create-task prompt.
func createTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		result, err := buildSkillContent(ctx, svc, skillsDir, "create-task", skillArgs{
			Description: req.Params.Arguments["description"],
		}, true)
		if err != nil {
			return nil, err
		}
		text := stripAgentConfig(result.Content)
		return &mcp.GetPromptResult{
			Description: "Create a new task on the board",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// createPlanPromptHandler returns the handler for create-plan prompt.
// If the card has autonomous: true, returns a redirect to run-autonomous.
// Otherwise, Phase 1 drafts the plan, the orchestrator handles user approval,
// then creates subtasks inline.
func createPlanPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		// Validate that the skill and card exist; content is not used directly —
		// the delegation prompt instructs the agent to call get_skill at runtime.
		if _, err := buildSkillContent(ctx, svc, skillsDir, "create-plan", skillArgs{
			CardID: cardID,
		}, true); err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='create-plan', card_id='%s'", cardID)

		// Check if card is autonomous — redirect to run-autonomous.
		card, _, findErr := findCard(ctx, svc, cardID)
		if findErr != nil {
			slog.Warn("create-plan: could not look up card for autonomous check, falling back to HITL",
				"card_id", cardID, "error", findErr)
		}
		isAutonomous := card != nil && card.Autonomous

		if isAutonomous {
			text := fmt.Sprintf(
				"**Stop — wrong entry point.** Card **%s** has `autonomous: true` set, "+
					"but you invoked the HITL `create-plan` prompt which is designed for "+
					"human-in-the-loop workflows with approval gates.\n\n"+
					"To run this card autonomously, use the `run-autonomous` prompt instead:\n\n"+
					"```\n/contextmatrix:run-autonomous %s\n```\n\n"+
					"Alternatively, if you intend to run this card interactively with human "+
					"approval at each phase, remove `autonomous: true` from the card first.",
				cardID, cardID,
			)
			return &mcp.GetPromptResult{
				Description: "Create plan and subtasks for a card",
				Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
			}, nil
		}

		text := buildCreatePlanDelegationPrompt(cardID, getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Create plan and subtasks for a card",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// executeTaskPromptHandler returns the handler for execute-task prompt.
func executeTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "execute-task", skillArgs{
			CardID: cardID,
		}, true)
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='execute-task', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "execute-task", getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Claim and execute a task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// buildReviewTaskDelegationPrompt returns a delegation prompt for the
// review-task workflow. Unlike the generic buildDelegationPrompt, this
// encodes the two-step flow explicitly:
//
//   - Step 1 (review sub-agent): spawn a sub-agent that evaluates the work,
//     writes a "## Review Findings" section to the card body via update_card,
//     releases the card, and returns a REVIEW_FINDINGS structured output
//     immediately — without asking the user or waiting for a decision.
//   - User decision: the orchestrator (main Claude, always alive) reads the
//     card body, presents the findings to the user, and collects the
//     approve/reject decision directly.
//   - The orchestrator prints REVIEW_APPROVED or REVIEW_REJECTED based on
//     the user's answer — no second sub-agent needed.
//
// This eliminates the idle-wait that kills sub-agents when they wait for user
// input between writing findings and collecting the human's decision.
func buildReviewTaskDelegationPrompt(model, cardID, getSkillArgs string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## Review Task Workflow")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Reviewing card **%s** uses a two-step flow to avoid agent timeouts.\n", cardID)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Model-Aware Routing")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "When calling `get_skill` below, include your model family as `caller_model`.\n")
	fmt.Fprintln(&b, "Check your system context for \"You are powered by the model named...\" and")
	fmt.Fprintln(&b, "extract the family: **opus**, **sonnet**, or **haiku**.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "If the `get_skill` response has `inline: true`:")
	fmt.Fprintln(&b, "  → Execute the returned content directly (it includes lifecycle instructions).")
	fmt.Fprintln(&b, "  → When you reach the structured output (`REVIEW_FINDINGS`), continue to")
	fmt.Fprintln(&b, "    **User Approval** below. Since you wrote the findings inline, present them")
	fmt.Fprintln(&b, "    directly — no need to call `get_card`.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "If `inline` is false or absent:")
	fmt.Fprintln(&b, "  → Spawn a sub-agent as described in the delegation steps below.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Step 1: Review Sub-Agent (delegation path)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "1. Call `get_skill(%s, caller_model='<your_model>')` to retrieve the full skill prompt and the required model.\n", getSkillArgs)
	fmt.Fprintln(&b, "2. If `inline` is true, execute the content directly and skip to **User Approval**.")
	fmt.Fprintln(&b, "3. Otherwise, spawn a sub-agent using the **`Agent`** tool with:")
	fmt.Fprintf(&b, "   - `model`: `\"%s\"` — **CRITICAL**, do not omit\n", model)
	fmt.Fprintf(&b, "   - `description`: `\"review-task %s\"`\n", cardID)
	fmt.Fprintln(&b, "   - `prompt`: the full skill content returned by `get_skill`")
	fmt.Fprintln(&b, "4. Wait for the review sub-agent to complete.")
	fmt.Fprintln(&b, "5. Parse its structured output. It will be in this format:")
	fmt.Fprintln(&b, "   ```")
	fmt.Fprintln(&b, "   REVIEW_FINDINGS")
	fmt.Fprintf(&b, "   card_id: %s\n", cardID)
	fmt.Fprintln(&b, "   recommendation: approve | approve_with_notes | revise")
	fmt.Fprintln(&b, "   summary: <one-line summary>")
	fmt.Fprintln(&b, "   ```")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### User Approval (YOU handle this directly — no sub-agent)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "6. If you executed the review inline, present the findings you just wrote directly.")
	fmt.Fprintln(&b, "   If you delegated, call `get_card` to read the `## Review Findings` section from the card body.")
	fmt.Fprintln(&b, "7. Present the findings to the user directly:")
	fmt.Fprintln(&b, "   > Here are the review findings for [card title]:")
	fmt.Fprintln(&b, "   > [paste the ## Review Findings section]")
	fmt.Fprintln(&b, "   > Do you approve this work, or should it be sent back for revision?")
	fmt.Fprintln(&b, "8. Based on the user's answer, YOU (the orchestrator) print one of:")
	fmt.Fprintln(&b, "   - `REVIEW_APPROVED` — if the user approves the work")
	fmt.Fprintln(&b, "   - `REVIEW_REJECTED` — if the user wants the work sent back for revision")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Do NOT use SendMessage to spawn sub-agents — use the `Agent` tool with model `%s`.\n", model)
	return b.String()
}

// reviewTaskPromptHandler returns the handler for review-task prompt.
// It uses a two-step delegation prompt to avoid sub-agent timeouts during
// user approval: the review sub-agent writes findings to the card body and
// returns immediately, and the orchestrator handles user approve/reject directly.
func reviewTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "review-task", skillArgs{
			CardID: cardID,
		}, true)
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='review-task', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "opus"
		}

		// Always use the HITL review prompt — even for autonomous cards.
		// The only caller that hits this prompt handler is a human invoking
		// the slash command; automated autonomous workflows use get_skill
		// directly and never reach here.
		text := buildReviewTaskDelegationPrompt(model, cardID, getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Review a completed task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// documentTaskPromptHandler returns the handler for document-task prompt.
func documentTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "document-task", skillArgs{
			CardID: cardID,
		}, true)
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='document-task', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDocumentTaskDelegationPrompt(model, cardID, getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Write documentation for a completed task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// initProjectPromptHandler returns the handler for init-project prompt.
func initProjectPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		name := req.Params.Arguments["name"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "init-project", skillArgs{
			Name: name,
		}, true)
		if err != nil {
			return nil, err
		}
		text := stripAgentConfig(result.Content)
		return &mcp.GetPromptResult{
			Description: "Initialize a new project board",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// startWorkflowPromptHandler returns the handler for start-workflow prompt.
// It inspects the card's autonomous flag and delegates to runAutonomousPromptHandler
// (autonomous: true) or createPlanPromptHandler (autonomous: false / unset).
// No routing logic is duplicated — it calls through to the captured handlers directly.
func startWorkflowPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	autonomousHandler := runAutonomousPromptHandler(svc, skillsDir)
	planHandler := createPlanPromptHandler(svc, skillsDir)

	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]

		card, _, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, fmt.Errorf("start workflow: %w", err)
		}

		if card.Autonomous {
			return autonomousHandler(ctx, req)
		}
		return planHandler(ctx, req)
	}
}

// runAutonomousPromptHandler returns the handler for run-autonomous prompt.
// It reads the card state and returns the appropriate full-chain delegation prompt.
func runAutonomousPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "run-autonomous", skillArgs{
			CardID: cardID,
		}, true)
		if err != nil {
			return nil, err
		}
		text := stripAgentConfig(result.Content)
		return &mcp.GetPromptResult{
			Description: "Run a card through its full lifecycle autonomously",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// --- Shared skill content builder ---

// skillArgs holds optional arguments for building a skill's content.
type skillArgs struct {
	CardID      string
	Description string
	Name        string
}

// validSkillNames lists all recognized skill names.
var validSkillNames = []string{
	"create-task", "create-plan", "execute-task",
	"review-task", "document-task", "init-project",
	"run-autonomous",
}

// buildSkillContent reads the skill file and assembles the full prompt text
// with injected card/project context. Used by both prompt handlers and the
// get_skill tool. Returns a skillResult with the content and parsed model.
// When includePreamble is false, the workflow rules preamble is omitted to
// avoid re-injecting it into agents that already have it (e.g. orchestrators
// calling get_skill multiple times during an autonomous run).
func buildSkillContent(ctx context.Context, svc *service.CardService, skillsDir, skillName string, args skillArgs, includePreamble bool) (skillResult, error) {
	var content string
	var err error

	switch skillName {
	case "create-task":
		content, err = buildCreateTask(skillsDir, args.Description)
	case "create-plan":
		content, err = buildCardSkill(ctx, svc, skillsDir, "create-plan.md", args.CardID, false)
	case "execute-task":
		content, err = buildCardSkill(ctx, svc, skillsDir, "execute-task.md", args.CardID, true)
	case "review-task":
		content, err = buildSubtaskSkill(ctx, svc, skillsDir, "review-task.md", args.CardID)
	case "document-task":
		content, err = buildSubtaskSkill(ctx, svc, skillsDir, "document-task.md", args.CardID)
	case "init-project":
		content, err = buildInitProject(ctx, svc, skillsDir, args.Name)
	case "run-autonomous":
		content, err = buildRunAutonomous(ctx, svc, skillsDir, args.CardID)
	default:
		return skillResult{}, fmt.Errorf("unknown skill %q; valid skills: %v", skillName, validSkillNames)
	}
	if err != nil {
		return skillResult{}, err
	}

	prefix := ""
	if includePreamble {
		prefix = workflowPreamble
	}
	return skillResult{
		Content: prefix + content,
		Model:   parseSkillModel(content),
	}, nil
}

func buildCreateTask(skillsDir, description string) (string, error) {
	skill, err := readSkillFile(skillsDir, "create-task.md")
	if err != nil {
		return "", err
	}
	if description != "" {
		skill = "User description: " + description + "\n\n" + skill
	}
	return skill, nil
}

// buildCardSkill handles skills that need a single card's context, optionally
// including parent and sibling cards (for execute-task).
func buildCardSkill(ctx context.Context, svc *service.CardService, skillsDir, filename, cardID string, includeFamily bool) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}
	skill, err := readSkillFile(skillsDir, filename)
	if err != nil {
		return "", err
	}
	card, project, err := findCard(ctx, svc, cardID)
	if err != nil {
		return "", err
	}

	var parts []string
	parts = append(parts, formatCardContext(card, project))

	if includeFamily && card.Parent != "" {
		parent, perr := svc.GetCard(ctx, project, card.Parent)
		if perr == nil {
			parts = append(parts, "\n## Parent Card\n"+formatCardBriefWithBody(parent))
		}
		siblings, serr := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.Parent})
		if serr == nil {
			var lines []string
			for _, s := range siblings {
				if s.ID != card.ID {
					lines = append(lines, fmt.Sprintf("- %s [%s] %s", s.ID, s.State, s.Title))
				}
			}
			if len(lines) > 0 {
				parts = append(parts, "\n## Sibling Tasks\n"+strings.Join(lines, "\n"))
			}
		}
	}

	return strings.Join(parts, "\n") + "\n\n" + skill, nil
}

// buildSubtaskSkill handles skills that need a card plus all its subtasks
// (review-task, document-task).
func buildSubtaskSkill(ctx context.Context, svc *service.CardService, skillsDir, filename, cardID string) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}
	skill, err := readSkillFile(skillsDir, filename)
	if err != nil {
		return "", err
	}
	card, project, err := findCard(ctx, svc, cardID)
	if err != nil {
		return "", err
	}

	var parts []string
	parts = append(parts, formatCardContext(card, project))

	subtasks, serr := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.ID})
	if serr == nil && len(subtasks) > 0 {
		parts = append(parts, "\n## Subtasks")
		for _, sub := range subtasks {
			parts = append(parts, formatCardBrief(sub))
		}
	}

	return strings.Join(parts, "\n") + "\n\n" + skill, nil
}

func buildRunAutonomous(ctx context.Context, svc *service.CardService, skillsDir, cardID string) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}
	skill, err := readSkillFile(skillsDir, "run-autonomous.md")
	if err != nil {
		return "", err
	}
	card, project, err := findCard(ctx, svc, cardID)
	if err != nil {
		return "", err
	}

	if !card.Autonomous {
		return "", fmt.Errorf("card %s does not have autonomous mode enabled", cardID)
	}

	var parts []string
	parts = append(parts, formatCardContext(card, project))

	// Inject server-side complexity classification so the skill can route
	// simple tasks to the fast path (skip planning/review/docs).
	subtasks, serr := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.ID})
	complexity := classifyComplexity(card, subtasks, serr)
	parts = append(parts, fmt.Sprintf("- **Complexity:** %s", complexity))

	// Include subtasks if any
	if serr == nil && len(subtasks) > 0 {
		parts = append(parts, "\n## Subtasks")
		for _, sub := range subtasks {
			parts = append(parts, formatCardBrief(sub))
		}
	}

	return strings.Join(parts, "\n") + "\n\n" + skill, nil
}

// classifyComplexity determines whether a task is simple enough for the fast
// path (skip planning/review/docs). A task is simple only if it has the
// "simple" label AND has no existing subtasks.
func classifyComplexity(card *board.Card, subtasks []*board.Card, subtaskErr error) string {
	// Already has subtasks — standard pipeline needed.
	if subtaskErr == nil && len(subtasks) > 0 {
		return "standard"
	}
	for _, l := range card.Labels {
		if l == "simple" {
			return "simple"
		}
	}
	return "standard"
}

func buildInitProject(ctx context.Context, svc *service.CardService, skillsDir, name string) (string, error) {
	skill, err := readSkillFile(skillsDir, "init-project.md")
	if err != nil {
		return "", err
	}
	projects, perr := svc.ListProjects(ctx)
	if perr == nil && len(projects) > 0 {
		var names []string
		for _, p := range projects {
			names = append(names, p.Name)
		}
		skill = "Existing projects on this board: " + strings.Join(names, ", ") + "\n\n" + skill
	}
	if name != "" {
		skill = "Suggested project name: " + name + "\n\n" + skill
	}
	return skill, nil
}

// --- Helpers ---

// readSkillFile reads a skill file from the skills directory.
func readSkillFile(skillsDir, filename string) (string, error) {
	data, err := os.ReadFile(filepath.Join(skillsDir, filename))
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", filename, err)
	}
	return string(data), nil
}

// findCard searches for a card by ID across all projects.
func findCard(ctx context.Context, svc *service.CardService, cardID string) (*board.Card, string, error) {
	projects, err := svc.ListProjects(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list projects: %w", err)
	}

	for _, p := range projects {
		c, err := svc.GetCard(ctx, p.Name, cardID)
		if err == nil {
			return c, p.Name, nil
		}
	}

	return nil, "", fmt.Errorf("card %s not found in any project", cardID)
}

// formatCardContext formats a card with full details for prompt injection.
func formatCardContext(c *board.Card, project string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Card: %s\n", c.ID)
	fmt.Fprintf(&b, "- **Title:** %s\n", c.Title)
	fmt.Fprintf(&b, "- **Project:** %s\n", project)
	fmt.Fprintf(&b, "- **Type:** %s\n", c.Type)
	fmt.Fprintf(&b, "- **State:** %s\n", c.State)
	fmt.Fprintf(&b, "- **Priority:** %s\n", c.Priority)
	if c.AssignedAgent != "" {
		fmt.Fprintf(&b, "- **Assigned Agent:** %s\n", c.AssignedAgent)
	}
	if c.Parent != "" {
		fmt.Fprintf(&b, "- **Parent:** %s\n", c.Parent)
	}
	if len(c.DependsOn) > 0 {
		fmt.Fprintf(&b, "- **Depends On:** %s\n", strings.Join(c.DependsOn, ", "))
	}
	if len(c.Labels) > 0 {
		fmt.Fprintf(&b, "- **Labels:** %s\n", strings.Join(c.Labels, ", "))
	}
	if c.Autonomous {
		fmt.Fprintf(&b, "- **Autonomous:** true\n")
	}
	if c.FeatureBranch {
		fmt.Fprintf(&b, "- **Feature Branch:** enabled\n")
	}
	if c.BranchName != "" {
		fmt.Fprintf(&b, "- **Branch:** %s\n", c.BranchName)
	}
	if c.CreatePR {
		fmt.Fprintf(&b, "- **Create PR:** enabled\n")
	}
	if c.PRUrl != "" {
		fmt.Fprintf(&b, "- **PR URL:** %s\n", c.PRUrl)
	}
	if c.ReviewAttempts > 0 {
		fmt.Fprintf(&b, "- **Review Attempts:** %d\n", c.ReviewAttempts)
	}
	if c.Body != "" {
		fmt.Fprintf(&b, "\n### Body\n\n%s\n", c.Body)
	}
	return b.String()
}

// formatCardBrief formats a card as a brief summary without the body.
// Use formatCardBriefWithBody when the caller genuinely needs body content.
func formatCardBrief(c *board.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s: %s\n", c.ID, c.Title)
	fmt.Fprintf(&b, "- State: %s | Type: %s | Priority: %s\n", c.State, c.Type, c.Priority)
	if c.AssignedAgent != "" {
		fmt.Fprintf(&b, "- Agent: %s\n", c.AssignedAgent)
	}
	return b.String()
}

// formatCardBriefWithBody formats a card as a brief summary including the full body.
func formatCardBriefWithBody(c *board.Card) string {
	s := formatCardBrief(c)
	if c.Body != "" {
		s += fmt.Sprintf("\n%s\n", c.Body)
	}
	return s
}
