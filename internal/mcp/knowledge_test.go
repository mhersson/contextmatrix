package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/refresh"
	"github.com/mhersson/contextmatrix/internal/service"
)

// seedKB configures a "core" repo on test-project and writes two KB docs.
func seedKB(t *testing.T, env *testEnv) {
	t.Helper()

	ctx := context.Background()

	cfg, err := env.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, env.store.SaveProject(ctx, cfg))

	_, err = env.svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "# A\n", "glossary.md": "# G\n"},
		Source:     service.KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)
}

func TestGetKnowledgeBase_ReturnsDocs(t *testing.T) {
	env := setupMCP(t)
	seedKB(t, env)

	result := callTool(t, env, "get_knowledge_base", map[string]any{
		"project": "test-project",
		"repo":    "core",
	})
	require.False(t, result.IsError)

	var out getKnowledgeBaseOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "test-project", out.Project)
	assert.Equal(t, "core", out.Repo)
	assert.Equal(t, "# A\n", out.Docs["architecture.md"])
	assert.Equal(t, "# G\n", out.Docs["glossary.md"])
	assert.Equal(t, "abc", out.Meta.LastBuiltCommit)
}

func TestGetKnowledgeBase_EmptyWhenNoRepo(t *testing.T) {
	env := setupMCP(t)
	// no seedKB — project has no repos configured

	result := callTool(t, env, "get_knowledge_base", map[string]any{
		"project": "test-project",
	})
	require.False(t, result.IsError)

	var out getKnowledgeBaseOutput
	unmarshalResult(t, result, &out)
	assert.Empty(t, out.Docs)
}

func TestReadKnowledgeDoc_ReturnsContent(t *testing.T) {
	env := setupMCP(t)
	seedKB(t, env)

	result := callTool(t, env, "read_knowledge_doc", map[string]any{
		"project": "test-project",
		"repo":    "core",
		"doc":     "architecture.md",
	})
	require.False(t, result.IsError)

	var out readKnowledgeDocOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "# A\n", out.Content)
	assert.False(t, out.Meta.HumanEdited)
}

func TestReadKnowledgeDoc_ErrorOnMissingDoc(t *testing.T) {
	env := setupMCP(t)
	seedKB(t, env)

	result := callTool(t, env, "read_knowledge_doc", map[string]any{
		"project": "test-project",
		"repo":    "core",
		"doc":     "api-documentation.md",
	})
	assert.True(t, result.IsError)
}

func TestListKnowledgeBases_ReturnsSummaries(t *testing.T) {
	env := setupMCP(t)
	seedKB(t, env)

	result := callTool(t, env, "list_knowledge_bases", map[string]any{})
	require.False(t, result.IsError)

	var out listKnowledgeBasesOutput
	unmarshalResult(t, result, &out)
	require.Len(t, out.Bases, 1)
	assert.Equal(t, "test-project", out.Bases[0].Project)
	require.Len(t, out.Bases[0].Repos, 1)
	assert.Equal(t, "core", out.Bases[0].Repos[0].Name)
}

func TestListKnowledgeBases_ProjectFilter(t *testing.T) {
	env := setupMCP(t)
	seedKB(t, env)

	result := callTool(t, env, "list_knowledge_bases", map[string]any{
		"project": "test-project",
	})
	require.False(t, result.IsError)

	var out listKnowledgeBasesOutput
	unmarshalResult(t, result, &out)
	require.Len(t, out.Bases, 1)
	assert.Equal(t, "test-project", out.Bases[0].Project)
}

func TestRefreshKnowledgeBase_HumanCanCall(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	cfg, err := env.svc.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, env.store.SaveProject(ctx, cfg))

	result := callTool(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "test-project",
		"agent_id": "human:tester",
	})
	require.False(t, result.IsError)

	var out refreshKnowledgeBaseOutput
	unmarshalResult(t, result, &out)
	assert.Len(t, out.Plan.Items, 4)
}

func TestRefreshKnowledgeBase_RejectsNonHuman(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "test-project",
		"agent_id": "agent-1",
	})
	require.True(t, resultIsError(result, err), "non-human refresh should fail")
}

func TestCommitKnowledgeDocs_HumanWritesAndPersists(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	cfg, err := env.svc.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, env.store.SaveProject(ctx, cfg))

	result := callTool(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"head_commit": "deadbeef",
		"agent_id":    "human:tester",
		"docs": map[string]any{
			"architecture.md": "# A\n",
			"glossary.md":     "# G\n",
		},
	})
	require.False(t, result.IsError)

	var out commitKnowledgeDocsOutput
	unmarshalResult(t, result, &out)
	assert.ElementsMatch(t, []string{"architecture.md", "glossary.md"}, out.FilesWritten)

	// Verify persistence via the public service API.
	doc, err := env.svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.NoError(t, err)
	assert.Equal(t, "# A\n", doc.Content)
}

func TestCommitKnowledgeDocs_RejectsNonHuman(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"head_commit": "x",
		"agent_id":    "bot-1",
		"docs":        map[string]any{"architecture.md": "x"},
	})
	require.True(t, resultIsError(result, err))
}

func TestRefreshKnowledgeBase_RejectsEmptyAgentID(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "test-project",
		"agent_id": "",
	})
	require.True(t, resultIsError(result, err), "empty agent_id should be rejected")
}

func TestRefreshKnowledgeBase_RejectsHumanWithoutColon(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "test-project",
		"agent_id": "human",
	})
	require.True(t, resultIsError(result, err), "agent_id without colon should be rejected")
}

func TestRefreshKnowledgeBase_RejectsBareHumanPrefix(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "test-project",
		"agent_id": "human:",
	})
	require.True(t, resultIsError(result, err),
		"bare \"human:\" agent_id must be rejected — auditing the literal prefix is meaningless")
}

func TestUpdateRefreshProgress_RejectsBareHumanPrefix(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "update_refresh_progress", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"agent_id":    "human:",
		"docs_total":  4,
		"docs_done":   1,
		"current_doc": "architecture.md",
	})
	assert.True(t, result.IsError,
		"bare \"human:\" agent_id must be rejected — auditing the literal prefix is meaningless")
}

func TestCommitKnowledgeDocs_RejectsBareHumanPrefix(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"head_commit": "x",
		"agent_id":    "human:",
		"docs":        map[string]any{"architecture.md": "x"},
	})
	require.True(t, resultIsError(result, err),
		"bare \"human:\" agent_id must be rejected — auditing the literal prefix is meaningless")
}

func TestRefreshKnowledgeBase_UnknownProject(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "refresh_knowledge_base", map[string]any{
		"project":  "nope",
		"agent_id": "human:alice",
	})
	require.True(t, resultIsError(result, err), "unknown project should be rejected")
}

func TestCommitKnowledgeDocs_RejectsEmptyAgentID(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"head_commit": "x",
		"agent_id":    "",
		"docs":        map[string]any{"architecture.md": "x"},
	})
	require.True(t, resultIsError(result, err), "empty agent_id should be rejected")
}

func TestCommitKnowledgeDocs_RejectsHumanWithoutColon(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"head_commit": "x",
		"agent_id":    "human",
		"docs":        map[string]any{"architecture.md": "x"},
	})
	require.True(t, resultIsError(result, err), "agent_id without colon should be rejected")
}

func TestCommitKnowledgeDocs_UnknownProject(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "commit_knowledge_docs", map[string]any{
		"project":     "nope",
		"repo":        "core",
		"head_commit": "x",
		"agent_id":    "human:alice",
		"docs":        map[string]any{"architecture.md": "x"},
	})
	require.True(t, resultIsError(result, err), "unknown project should be rejected")
}

func TestGetSkill_RefreshKnowledge(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Configure repos so the prompt has something to render.
	cfg, err := env.svc.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, env.store.SaveProject(ctx, cfg))

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "refresh-knowledge",
		"name":       "test-project",
		"repo":       "core",
	})
	require.False(t, result.IsError, "tool should not error")

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "refresh-knowledge", out.SkillName)
	assert.Contains(t, out.Content, "test-project", "preamble should include project")
	assert.Contains(t, out.Content, "core", "preamble should include repo")
}

func TestUpdateRefreshProgress_HumanOnly(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "update_refresh_progress", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"agent_id":    "agent-not-human",
		"docs_total":  4,
		"docs_done":   1,
		"current_doc": "architecture.md",
	})
	assert.True(t, result.IsError, "non-human agent must be rejected")
}

func TestUpdateRefreshProgress_TrackedFalseWhenNoJob(t *testing.T) {
	env := setupMCP(t)

	// Wire an empty registry: tool should observe no in-flight job.
	reg := refresh.NewRegistry()
	env.svc.SetRefreshRegistry(reg)

	result := callTool(t, env, "update_refresh_progress", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"agent_id":    "human:test",
		"docs_total":  4,
		"docs_done":   1,
		"current_doc": "architecture.md",
	})
	require.False(t, result.IsError)

	var out updateRefreshProgressOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.OK)
	assert.False(t, out.Tracked, "no job in registry => tracked=false")
}

func TestUpdateRefreshProgress_TrackedTrueUpdatesRegistry(t *testing.T) {
	env := setupMCP(t)

	reg := refresh.NewRegistry()
	env.svc.SetRefreshRegistry(reg)

	_, err := reg.Acquire("test-project", "core", "human:test")
	require.NoError(t, err)
	require.NoError(t, reg.MarkRunning("test-project", "core", 4))

	result := callTool(t, env, "update_refresh_progress", map[string]any{
		"project":     "test-project",
		"repo":        "core",
		"agent_id":    "human:test",
		"docs_total":  4,
		"docs_done":   2,
		"current_doc": "code-structure.md",
	})
	require.False(t, result.IsError)

	snap := reg.Snapshot("test-project")
	assert.Equal(t, 2, snap["core"].DocsDone)
	assert.Equal(t, "code-structure.md", snap["core"].CurrentDoc)
}
