package mcp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/service"
)

// realSkillsDir is the shipped workflow-skills directory. Tests run with the
// package directory as their working directory, so ../.. reaches the repo root.
// This is the only test file that reads it; everything else uses the stub
// skills setupMCP writes into t.TempDir().
const realSkillsDir = "../../workflow-skills"

// realSkillFiles lists the *.md files actually present in workflow-skills/.
func realSkillFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(realSkillsDir)
	require.NoError(t, err, "workflow-skills/ must be readable")

	var names []string

	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}

	require.NotEmpty(t, names, "workflow-skills/ must contain skill files")

	return names
}

// TestSkillFiles_AllBuildersResolve builds every registered skill against the
// real workflow-skills directory. skillBuilders hardcodes a filename per skill,
// so a deleted, renamed, or never-created file is invisible to every other test
// in the package - they all run against stubs in t.TempDir(). This is the only
// guard that a shipped skill file exists for each builder.
func TestSkillFiles_AllBuildersResolve(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// run-autonomous refuses a non-autonomous card, and the subtask builders
	// accept any card, so one autonomous card satisfies every builder.
	card, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title:      "Skill file resolution",
		Type:       "task",
		Priority:   "medium",
		Autonomous: true,
	})
	require.NoError(t, err)

	args := skillArgs{
		CardID:          card.ID,
		Description:     "a task description",
		Name:            "some-project",
		IncludeCardBody: true,
	}

	for _, name := range validSkillNames {
		t.Run(name, func(t *testing.T) {
			result, err := buildSkillContent(ctx, env.svc, realSkillsDir, name, args, true)
			require.NoError(t, err, "skill %q must build against the shipped workflow-skills dir", name)
			assert.NotEmpty(t, result.Content, "skill %q produced empty content", name)
		})
	}
}

// TestSkillFiles_NoOrphanFiles is the reverse direction: a skill file with no
// builder is dead weight that get_skill can never serve. Every skill file is
// named after its skill (create-plan.md serves "create-plan"), so the basename
// must appear in validSkillNames.
func TestSkillFiles_NoOrphanFiles(t *testing.T) {
	for _, file := range realSkillFiles(t) {
		name := strings.TrimSuffix(file, ".md")
		assert.True(t, slices.Contains(validSkillNames, name),
			"workflow-skills/%s has no builder in skillBuilders (valid: %v)", file, validSkillNames)
	}
}

// TestSkillFiles_AgentConfigStrippable guards the one structural contract the
// skill files owe the server: agentConfigPattern needs both the heading and a
// trailing --- separator. Drop the separator and stripAgentConfig silently
// no-ops, shipping orchestrator-only metadata to the executing agent.
func TestSkillFiles_AgentConfigStrippable(t *testing.T) {
	for _, file := range realSkillFiles(t) {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(realSkillsDir, file))
			require.NoError(t, err)

			content := string(data)
			assert.NotEqual(t, content, stripAgentConfig(content),
				"%s: stripAgentConfig left the file unchanged - the "+
					"'## Agent Configuration' block is missing or lacks its trailing '---'", file)
		})
	}
}
