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
	}, createTaskPromptHandler(skillsDir))

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
}

// createTaskPromptHandler returns the handler for create-task prompt.
func createTaskPromptHandler(skillsDir string) mcp.PromptHandler {
	return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		skill, err := readSkillFile(skillsDir, "create-task.md")
		if err != nil {
			return nil, err
		}

		description := req.Params.Arguments["description"]
		if description != "" {
			skill = "User description: " + description + "\n\n" + skill
		}

		return &mcp.GetPromptResult{
			Description: "Create a new task on the board",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: skill}},
			},
		}, nil
	}
}

// createPlanPromptHandler returns the handler for create-plan prompt.
func createPlanPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		skill, err := readSkillFile(skillsDir, "create-plan.md")
		if err != nil {
			return nil, err
		}

		cardID := req.Params.Arguments["card_id"]
		if cardID == "" {
			return nil, fmt.Errorf("card_id argument is required")
		}

		// Find the card across all projects
		card, project, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, err
		}

		// Inject card context
		cardContext := formatCardContext(card, project)
		skill = cardContext + "\n\n" + skill

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Create plan for %s: %s", card.ID, card.Title),
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: skill}},
			},
		}, nil
	}
}

// executeTaskPromptHandler returns the handler for execute-task prompt.
func executeTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		skill, err := readSkillFile(skillsDir, "execute-task.md")
		if err != nil {
			return nil, err
		}

		cardID := req.Params.Arguments["card_id"]
		if cardID == "" {
			return nil, fmt.Errorf("card_id argument is required")
		}

		card, project, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, err
		}

		// Build full task context (card + parent + siblings)
		var contextParts []string
		contextParts = append(contextParts, formatCardContext(card, project))

		if card.Parent != "" {
			parent, err := svc.GetCard(ctx, project, card.Parent)
			if err == nil {
				contextParts = append(contextParts, "\n## Parent Card\n"+formatCardBrief(parent))
			}

			siblings, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.Parent})
			if err == nil {
				var siblingLines []string
				for _, s := range siblings {
					if s.ID != card.ID {
						siblingLines = append(siblingLines, fmt.Sprintf("- %s [%s] %s", s.ID, s.State, s.Title))
					}
				}
				if len(siblingLines) > 0 {
					contextParts = append(contextParts, "\n## Sibling Tasks\n"+strings.Join(siblingLines, "\n"))
				}
			}
		}

		fullContext := strings.Join(contextParts, "\n") + "\n\n" + skill

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Execute task %s: %s", card.ID, card.Title),
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fullContext}},
			},
		}, nil
	}
}

// reviewTaskPromptHandler returns the handler for review-task prompt.
func reviewTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		skill, err := readSkillFile(skillsDir, "review-task.md")
		if err != nil {
			return nil, err
		}

		cardID := req.Params.Arguments["card_id"]
		if cardID == "" {
			return nil, fmt.Errorf("card_id argument is required")
		}

		card, project, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, err
		}

		var contextParts []string
		contextParts = append(contextParts, formatCardContext(card, project))

		// Load all subtasks
		subtasks, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.ID})
		if err == nil && len(subtasks) > 0 {
			contextParts = append(contextParts, "\n## Subtasks")
			for _, sub := range subtasks {
				contextParts = append(contextParts, formatCardBrief(sub))
			}
		}

		fullContext := strings.Join(contextParts, "\n") + "\n\n" + skill

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Review task %s: %s", card.ID, card.Title),
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fullContext}},
			},
		}, nil
	}
}

// documentTaskPromptHandler returns the handler for document-task prompt.
func documentTaskPromptHandler(svc *service.CardService, skillsDir string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		skill, err := readSkillFile(skillsDir, "document-task.md")
		if err != nil {
			return nil, err
		}

		cardID := req.Params.Arguments["card_id"]
		if cardID == "" {
			return nil, fmt.Errorf("card_id argument is required")
		}

		card, project, err := findCard(ctx, svc, cardID)
		if err != nil {
			return nil, err
		}

		var contextParts []string
		contextParts = append(contextParts, formatCardContext(card, project))

		// Load all subtasks
		subtasks, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.ID})
		if err == nil && len(subtasks) > 0 {
			contextParts = append(contextParts, "\n## Subtasks")
			for _, sub := range subtasks {
				contextParts = append(contextParts, formatCardBrief(sub))
			}
		}

		fullContext := strings.Join(contextParts, "\n") + "\n\n" + skill

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Document task %s: %s", card.ID, card.Title),
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fullContext}},
			},
		}, nil
	}
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
