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

// buildDelegationPrompt returns a short wrapper prompt that instructs the
// receiving agent to call get_skill with the given arguments and then spawn
// a sub-agent via the Agent tool with the returned model and content.
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
	fmt.Fprintln(&b, "2. Use the **Agent tool** with the returned `model` and `content` to spawn the sub-agent.")
	fmt.Fprintln(&b, "3. Wait for the sub-agent to complete and relay its structured output back.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Do NOT read the skill content yourself and execute it — you MUST use the Agent tool with model `%s`.\n", model)
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
}

// createTaskPromptHandler returns the handler for create-task prompt.
func createTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		result, err := buildSkillContent(ctx, svc, skillsDir, "create-task", skillArgs{
			Description: req.Params.Arguments["description"],
		})
		if err != nil {
			return nil, err
		}
		description := req.Params.Arguments["description"]
		getSkillArgs := "skill_name='create-task'"
		if description != "" {
			getSkillArgs += fmt.Sprintf(", description='%s'", description)
		}
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "create-task", getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Create a new task on the board",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	}
}

// createPlanPromptHandler returns the handler for create-plan prompt.
func createPlanPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "create-plan", skillArgs{
			CardID: cardID,
		})
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='create-plan', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "create-plan", getSkillArgs)
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
		})
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

// reviewTaskPromptHandler returns the handler for review-task prompt.
func reviewTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cardID := req.Params.Arguments["card_id"]
		result, err := buildSkillContent(ctx, svc, skillsDir, "review-task", skillArgs{
			CardID: cardID,
		})
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='review-task', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "review-task", getSkillArgs)
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
		})
		if err != nil {
			return nil, err
		}
		getSkillArgs := fmt.Sprintf("skill_name='document-task', card_id='%s'", cardID)
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "document-task", getSkillArgs)
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
		})
		if err != nil {
			return nil, err
		}
		getSkillArgs := "skill_name='init-project'"
		if name != "" {
			getSkillArgs += fmt.Sprintf(", name='%s'", name)
		}
		model := result.Model
		if model == "" {
			model = "sonnet"
		}
		text := buildDelegationPrompt(model, "init-project", getSkillArgs)
		return &mcp.GetPromptResult{
			Description: "Initialize a new project board",
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
}

// buildSkillContent reads the skill file and assembles the full prompt text
// with injected card/project context. Used by both prompt handlers and the
// get_skill tool. Returns a skillResult with the content and parsed model.
func buildSkillContent(ctx context.Context, svc *service.CardService, skillsDir, skillName string, args skillArgs) (skillResult, error) {
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
	default:
		return skillResult{}, fmt.Errorf("unknown skill %q; valid skills: %v", skillName, validSkillNames)
	}
	if err != nil {
		return skillResult{}, err
	}

	return skillResult{
		Content: workflowPreamble + content,
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
			parts = append(parts, "\n## Parent Card\n"+formatCardBrief(parent))
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
	if c.Body != "" {
		fmt.Fprintf(&b, "\n### Body\n\n%s\n", c.Body)
	}
	return b.String()
}

// formatCardBrief formats a card as a brief summary.
func formatCardBrief(c *board.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s: %s\n", c.ID, c.Title)
	fmt.Fprintf(&b, "- State: %s | Type: %s | Priority: %s\n", c.State, c.Type, c.Priority)
	if c.AssignedAgent != "" {
		fmt.Fprintf(&b, "- Agent: %s\n", c.AssignedAgent)
	}
	if c.Body != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Body)
	}
	return b.String()
}
