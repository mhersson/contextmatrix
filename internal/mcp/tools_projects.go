package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// --- Project management tools ---

type (
	listProjectsInput  struct{}
	listProjectsOutput struct {
		Projects []board.ProjectConfig `json:"projects"`
	}
)

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

func registerCreateProject(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_project",
		Description: "Create a new project board with directory structure and configuration. The project name becomes the directory name. States must include 'stalled' and 'not_planned' (validator-enforced). The names 'todo', 'in_progress', 'review', and 'done' are also hardcoded into lifecycle behaviour (claim auto-transitions, complete_task, parent/child orchestration, dashboard metrics) — extra states may be added freely but these six built-in names should not be renamed. All states must have transition entries.",
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
