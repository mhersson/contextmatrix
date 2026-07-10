package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// TestUpdateProject_MCP_PreservesDefaultSkillsAndRepo is the regression for the
// update_project MCP fix. The tool input carries no default_skills channel and
// no repo-clear channel, yet the service applies both wholesale (nil
// default_skills clears; empty repo clears). The handler backfills both from
// the current config, so an agent-driven update must leave an operator's
// default_skills and repo intact.
func TestUpdateProject_MCP_PreservesDefaultSkillsAndRepo(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Seed the project with a repo and operator-configured default_skills —
	// neither is expressible through the update_project tool input.
	skills := []string{"go-development"}
	_, err := env.svc.UpdateProject(ctx, "test-project", service.UpdateProjectInput{
		Repo:          "https://github.com/org/test",
		States:        []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		Types:         []string{"task", "bug", "feature"},
		Priorities:    []string{"low", "medium", "high", "critical"},
		Transitions:   testProjectConfig().Transitions,
		DefaultSkills: &skills,
	})
	require.NoError(t, err)

	// Agent-driven update via the MCP tool: only the required fields, no repo,
	// no default_skills.
	result := callTool(t, env, "update_project", map[string]any{
		"project":     "test-project",
		"states":      []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		"types":       []string{"task", "bug", "feature"},
		"priorities":  []string{"low", "medium", "high", "critical"},
		"transitions": testProjectConfig().Transitions,
	})
	require.False(t, result.IsError, "update_project should not error")

	// Both operator-managed fields must survive the agent update.
	cur, err := env.svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/org/test", cur.Repo, "repo must be preserved when the tool omits it")
	require.NotNil(t, cur.DefaultSkills, "default_skills must not be wiped by an MCP update")
	assert.Equal(t, []string{"go-development"}, *cur.DefaultSkills)
}

// TestUpdateProject_MCP_PreservesVerify pins that an MCP-driven update_project
// leaves an operator's project verify config intact. The tool input carries no
// verify channel, and UpdateProjectInput.Verify defaults to nil (preserve), so
// this is structural — but verify is a human-only field and must never be
// touched through the agent surface.
func TestUpdateProject_MCP_PreservesVerify(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	verify := &board.VerifyConfig{Command: "make test", TimeoutSeconds: 600}
	_, err := env.svc.UpdateProject(ctx, "test-project", service.UpdateProjectInput{
		States:      []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		Types:       []string{"task", "bug", "feature"},
		Priorities:  []string{"low", "medium", "high", "critical"},
		Transitions: testProjectConfig().Transitions,
		Verify:      verify,
	})
	require.NoError(t, err)

	result := callTool(t, env, "update_project", map[string]any{
		"project":     "test-project",
		"states":      []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		"types":       []string{"task", "bug", "feature"},
		"priorities":  []string{"low", "medium", "high", "critical"},
		"transitions": testProjectConfig().Transitions,
	})
	require.False(t, result.IsError, "update_project should not error")

	cur, err := env.svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	require.NotNil(t, cur.Verify, "verify must not be wiped by an MCP update")
	assert.Equal(t, "make test", cur.Verify.Command)
	assert.Equal(t, 600, cur.Verify.TimeoutSeconds)
}
