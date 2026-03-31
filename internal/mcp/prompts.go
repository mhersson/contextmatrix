package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

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
		content, err := buildSkillContent(ctx, svc, skillsDir, "create-task", skillArgs{
			Description: req.Params.Arguments["description"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Create a new task on the board",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
		}, nil
	}
}

// createPlanPromptHandler returns the handler for create-plan prompt.
func createPlanPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		content, err := buildSkillContent(ctx, svc, skillsDir, "create-plan", skillArgs{
			CardID: req.Params.Arguments["card_id"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Create plan and subtasks for a card",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
		}, nil
	}
}

// executeTaskPromptHandler returns the handler for execute-task prompt.
func executeTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		content, err := buildSkillContent(ctx, svc, skillsDir, "execute-task", skillArgs{
			CardID: req.Params.Arguments["card_id"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Claim and execute a task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
		}, nil
	}
}

// reviewTaskPromptHandler returns the handler for review-task prompt.
func reviewTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		content, err := buildSkillContent(ctx, svc, skillsDir, "review-task", skillArgs{
			CardID: req.Params.Arguments["card_id"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Review a completed task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
		}, nil
	}
}

// documentTaskPromptHandler returns the handler for document-task prompt.
func documentTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		content, err := buildSkillContent(ctx, svc, skillsDir, "document-task", skillArgs{
			CardID: req.Params.Arguments["card_id"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Write documentation for a completed task",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
		}, nil
	}
}

// initProjectPromptHandler returns the handler for init-project prompt.
func initProjectPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		content, err := buildSkillContent(ctx, svc, skillsDir, "init-project", skillArgs{
			Name: req.Params.Arguments["name"],
		})
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: "Initialize a new project board",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: content}}},
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
// get_skill tool.
func buildSkillContent(ctx context.Context, svc *service.CardService, skillsDir, skillName string, args skillArgs) (string, error) {
	switch skillName {
	case "create-task":
		return buildCreateTask(skillsDir, args.Description)
	case "create-plan":
		return buildCardSkill(ctx, svc, skillsDir, "create-plan.md", args.CardID, false)
	case "execute-task":
		return buildCardSkill(ctx, svc, skillsDir, "execute-task.md", args.CardID, true)
	case "review-task":
		return buildSubtaskSkill(ctx, svc, skillsDir, "review-task.md", args.CardID)
	case "document-task":
		return buildSubtaskSkill(ctx, svc, skillsDir, "document-task.md", args.CardID)
	case "init-project":
		return buildInitProject(ctx, svc, skillsDir, args.Name)
	default:
		return "", fmt.Errorf("unknown skill %q; valid skills: %v", skillName, validSkillNames)
	}
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
