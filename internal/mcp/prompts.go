package mcp

import (
	"context"
	"fmt"
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
- **When in doubt, use /contextmatrix:start-workflow <card_id>.** It routes
  the card through its full lifecycle for you.

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
//
// brainstorming is inline-eligible because dialogue with the user requires
// the same chat channel as the calling create-plan orchestrator; spawning
// a sub-agent for it would have no channel back to the user.
var inlineEligibleSkills = map[string]bool{
	"review-task":   true,
	"create-plan":   true,
	"brainstorming": true,
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

// registerPrompts adds all MCP prompts (slash commands) to the server.
func registerPrompts(server *mcp.Server, svc *service.CardService, workflowSkillsDir string) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "create-task",
		Description: "Start a guided task creation workflow. Optionally provide a description to seed the conversation.",
		Arguments: []*mcp.PromptArgument{
			{Name: "description", Description: "Optional free-text description of the task to create"},
		},
	}, createTaskPromptHandler(svc, workflowSkillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "init-project",
		Description: "Initialize a new ContextMatrix project board for the current repository. Auto-detects repo URL and derives project name.",
		Arguments: []*mcp.PromptArgument{
			{Name: "name", Description: "Optional project name (auto-detected from repo if omitted)"},
		},
	}, initProjectPromptHandler(svc, workflowSkillsDir))

	server.AddPrompt(&mcp.Prompt{
		Name:        "start-workflow",
		Description: "Start the workflow for a card. Automatically routes to run-autonomous for autonomous cards or create-plan for human-in-the-loop cards.",
		Arguments: []*mcp.PromptArgument{
			{Name: "card_id", Description: "Card ID to start the workflow for (e.g. ALPHA-001)", Required: true},
		},
	}, startWorkflowPromptHandler(svc, workflowSkillsDir))
}

// createTaskPromptHandler returns the handler for create-task prompt.
func createTaskPromptHandler(svc *service.CardService, workflowSkillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, "create-task", skillArgs{
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

// initProjectPromptHandler returns the handler for init-project prompt.
func initProjectPromptHandler(svc *service.CardService, workflowSkillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		name := req.Params.Arguments["name"]

		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, "init-project", skillArgs{
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
// It inspects the card's autonomous flag and returns the appropriate workflow
// skill content directly (run-autonomous for autonomous cards, create-plan
// for HITL). Both paths return raw skill content with the Agent Configuration
// section stripped, so the invoking agent drives the workflow inline.
func startWorkflowPromptHandler(svc *service.CardService, workflowSkillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]

		card, _, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, fmt.Errorf("start workflow: %w", err)
		}

		skill := "create-plan"
		description := "Create plan and subtasks for a card"

		if card.Autonomous {
			skill = "run-autonomous"
			description = "Run a card through its full lifecycle autonomously"
		}

		result, err := buildSkillContent(ctx, svc, workflowSkillsDir, skill, skillArgs{
			CardID: cardID,
		}, true)
		if err != nil {
			return nil, err
		}

		text := stripAgentConfig(result.Content)

		return &mcp.GetPromptResult{
			Description: description,
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
	"run-autonomous", "brainstorming", "systematic-debugging",
}

// buildSkillContent reads the skill file and assembles the full prompt text
// with injected card/project context. Used by both prompt handlers and the
// get_skill tool. Returns a skillResult with the content and parsed model.
// When includePreamble is false, the workflow rules preamble is omitted to
// avoid re-injecting it into agents that already have it (e.g. orchestrators
// calling get_skill multiple times during an autonomous run).
func buildSkillContent(ctx context.Context, svc *service.CardService, workflowSkillsDir, skillName string, args skillArgs, includePreamble bool) (skillResult, error) {
	var (
		content string
		err     error
	)

	switch skillName {
	case "create-task":
		content, err = buildCreateTask(workflowSkillsDir, args.Description)
	case "create-plan":
		content, err = buildCardSkill(ctx, svc, workflowSkillsDir, "create-plan.md", args.CardID, false)
	case "execute-task":
		content, err = buildCardSkill(ctx, svc, workflowSkillsDir, "execute-task.md", args.CardID, true)
	case "review-task":
		content, err = buildSubtaskSkill(ctx, svc, workflowSkillsDir, "review-task.md", args.CardID)
	case "document-task":
		content, err = buildSubtaskSkill(ctx, svc, workflowSkillsDir, "document-task.md", args.CardID)
	case "init-project":
		content, err = buildInitProject(ctx, svc, workflowSkillsDir, args.Name)
	case "run-autonomous":
		content, err = buildRunAutonomous(ctx, svc, workflowSkillsDir, args.CardID)
	case "brainstorming":
		content, err = buildCardSkill(ctx, svc, workflowSkillsDir, "brainstorming.md", args.CardID, false)
	case "systematic-debugging":
		content, err = buildCardSkill(ctx, svc, workflowSkillsDir, "systematic-debugging.md", args.CardID, false)
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

func buildCreateTask(workflowSkillsDir, description string) (string, error) {
	skill, err := readSkillFile(workflowSkillsDir, "create-task.md")
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
func buildCardSkill(ctx context.Context, svc *service.CardService, workflowSkillsDir, filename, cardID string, includeFamily bool) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}

	skill, err := readSkillFile(workflowSkillsDir, filename)
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
					safe := redactCardForPrompt(s)
					lines = append(lines, fmt.Sprintf("- %s [%s] %s", safe.ID, safe.State, safe.Title))
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
func buildSubtaskSkill(ctx context.Context, svc *service.CardService, workflowSkillsDir, filename, cardID string) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}

	skill, err := readSkillFile(workflowSkillsDir, filename)
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

func buildRunAutonomous(ctx context.Context, svc *service.CardService, workflowSkillsDir, cardID string) (string, error) {
	if cardID == "" {
		return "", fmt.Errorf("card_id argument is required")
	}

	skill, err := readSkillFile(workflowSkillsDir, "run-autonomous.md")
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

func buildInitProject(ctx context.Context, svc *service.CardService, workflowSkillsDir, name string) (string, error) {
	skill, err := readSkillFile(workflowSkillsDir, "init-project.md")
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
func readSkillFile(workflowSkillsDir, filename string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workflowSkillsDir, filename))
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
// Unvetted external cards are redacted first so untrusted title/body/activity
// entries can never influence the rendered skill prompt.
func formatCardContext(c *board.Card, project string) string {
	c = redactCardForPrompt(c)

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

	// Skill prompts always flow into agent (non-human) context, so redact
	// unvetted bodies to block prompt-injection payloads from externally
	// imported cards. The empty agent ID is non-human by definition —
	// redactUnvettedBody substitutes the placeholder for unvetted cards.
	body := redactUnvettedBody(c, "")
	if body != "" {
		fmt.Fprintf(&b, "\n### Body\n\n%s\n", body)
	}

	return b.String()
}

// formatCardBrief formats a card as a brief summary without the body.
// Use formatCardBriefWithBody when the caller genuinely needs body content.
// Unvetted external cards are redacted first so untrusted titles cannot reach
// the agent via parent/sibling summaries.
func formatCardBrief(c *board.Card) string {
	c = redactCardForPrompt(c)

	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s: %s\n", c.ID, c.Title)
	fmt.Fprintf(&b, "- State: %s | Type: %s | Priority: %s\n", c.State, c.Type, c.Priority)

	if c.AssignedAgent != "" {
		fmt.Fprintf(&b, "- Agent: %s\n", c.AssignedAgent)
	}

	if c.Autonomous {
		fmt.Fprintf(&b, "- Autonomous: true\n")
	}

	if c.FeatureBranch {
		fmt.Fprintf(&b, "- Feature Branch: enabled\n")
	}

	if c.BranchName != "" {
		fmt.Fprintf(&b, "- Branch: %s\n", c.BranchName)
	}

	if c.CreatePR {
		fmt.Fprintf(&b, "- Create PR: enabled\n")
	}

	if c.BaseBranch != "" {
		fmt.Fprintf(&b, "- Base Branch: %s\n", c.BaseBranch)
	}

	return b.String()
}

// formatCardBriefWithBody formats a card as a brief summary including the full body.
// Unvetted external cards are redacted first via redactCardForPrompt so both
// the brief header (title, source, activity log) and the appended body are
// replaced with safe placeholders, blocking prompt injection from externally
// imported cards — skill prompts always flow into agent context.
func formatCardBriefWithBody(c *board.Card) string {
	c = redactCardForPrompt(c)

	s := formatCardBrief(c)

	body := redactUnvettedBody(c, "")
	if body != "" {
		s += fmt.Sprintf("\n%s\n", body)
	}

	return s
}
