package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteKnowledgeDocs_RefreshSourceClearsHumanEdited(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	docs := map[string]string{
		"architecture.md": "# Arch\n",
		"glossary.md":     "# Glossary\n",
	}
	res, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       docs,
		HeadCommit: "abc1234",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:tester",
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"architecture.md", "glossary.md"}, res.FilesWritten)

	meta, err := svc.store.ReadKnowledgeMeta(ctx, "test-project")
	require.NoError(t, err)

	r := meta.Repos["core"]
	assert.Equal(t, "abc1234", r.LastBuiltCommit)
	assert.Equal(t, "human:tester", r.LastBuiltBy)
	assert.False(t, r.Docs["architecture.md"].HumanEdited)
	assert.False(t, r.Docs["glossary.md"].HumanEdited)
}

func TestWriteKnowledgeDocs_EditSourceSetsHumanEdited(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{"architecture.md": "# By Human\n"},
		Source:  KnowledgeWriteSourceEdit,
		AgentID: "human:editor",
	})
	require.NoError(t, err)

	meta, err := svc.store.ReadKnowledgeMeta(ctx, "test-project")
	require.NoError(t, err)
	assert.True(t, meta.Repos["core"].Docs["architecture.md"].HumanEdited)
}

func TestWriteKnowledgeDocs_AtomicSingleCommit(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	beforeCount, err := svc.git.CommitCount()
	require.NoError(t, err)

	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs: map[string]string{
			"architecture.md":   "# A\n",
			"code-structure.md": "# C\n",
			"glossary.md":       "# G\n",
		},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	afterCount, err := svc.git.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, beforeCount+1, afterCount, "expected exactly one commit")
}

func TestWriteKnowledgeDocs_RejectsInvalidDocName(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{"../bad.md": "x"},
		Source:  KnowledgeWriteSourceRefresh,
		AgentID: "human:t",
	})
	require.Error(t, err)
}

func TestWriteKnowledgeDocs_CommitMessageDiffersBySource(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "# Refresh\n"},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc", AgentID: "human:t",
	})
	require.NoError(t, err)
	msgRefresh, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msgRefresh, "chore(knowledge): refresh")

	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{"glossary.md": "# Edit\n"},
		Source:  KnowledgeWriteSourceEdit,
		AgentID: "human:editor",
	})
	require.NoError(t, err)
	msgEdit, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msgEdit, "docs(knowledge): edit")
}

func TestWriteKnowledgeDocs_RequiresProjectAndRepo(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "", Repo: "core",
		Docs:    map[string]string{"architecture.md": "x"},
		Source:  KnowledgeWriteSourceRefresh,
		AgentID: "human:t",
	})
	assert.Error(t, err)
}

func TestWriteKnowledgeDocs_RejectsEmptyDocsMap(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{},
		Source:  KnowledgeWriteSourceRefresh,
		AgentID: "human:t",
	})
	assert.Error(t, err)
}

func TestReadKnowledgeBase_EmptyProject(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	out, err := svc.ReadKnowledgeBase(context.Background(), "test-project", "")
	require.NoError(t, err)
	assert.Empty(t, out.Docs)
}

func TestReadKnowledgeBase_AfterWrite(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "# A\n", "glossary.md": "# G\n"},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	out, err := svc.ReadKnowledgeBase(ctx, "test-project", "core")
	require.NoError(t, err)
	assert.Equal(t, "core", out.Repo)
	assert.Equal(t, "# A\n", out.Docs["architecture.md"])
	assert.Equal(t, "# G\n", out.Docs["glossary.md"])
	assert.Equal(t, "abc", out.Meta.LastBuiltCommit)
}

func TestReadKnowledgeBase_DefaultsToPrimaryRepo(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	out, err := svc.ReadKnowledgeBase(ctx, "test-project", "")
	require.NoError(t, err)
	assert.Equal(t, "core", out.Repo)
}

func TestReadKnowledgeDoc_Found(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	out, err := svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.NoError(t, err)
	assert.Equal(t, "# A\n", out.Content)
	assert.False(t, out.Meta.HumanEdited)
}

func TestListKnowledgeBases(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "# A\n"},
		Source:     KnowledgeWriteSourceRefresh,
		HeadCommit: "abc",
		AgentID:    "human:t",
	})
	require.NoError(t, err)

	out, err := svc.ListKnowledgeBases(ctx, "")
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "test-project", out[0].Project)
	require.Len(t, out[0].Repos, 1)
	assert.Equal(t, "core", out[0].Repos[0].Name)
	require.Len(t, out[0].Repos[0].Docs, 1)
	assert.Equal(t, "architecture.md", out[0].Repos[0].Docs[0].Name)
}

func TestBuildRefreshPlan_NewProjectAllMissing(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	plan, err := svc.BuildRefreshPlan(ctx, "test-project", "")
	require.NoError(t, err)
	assert.Len(t, plan.Items, 4)

	for _, item := range plan.Items {
		assert.Equal(t, "core", item.Repo)
		assert.Equal(t, "missing", item.Reason)
		assert.False(t, item.HumanEdited)
	}
}

func TestBuildRefreshPlan_FlagsHumanEdited(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{"architecture.md": "# Hand-edited\n"},
		Source:  KnowledgeWriteSourceEdit,
		AgentID: "human:editor",
	})
	require.NoError(t, err)

	plan, err := svc.BuildRefreshPlan(ctx, "test-project", "")
	require.NoError(t, err)

	var found bool

	for _, item := range plan.Items {
		if item.Doc == "architecture.md" {
			assert.True(t, item.HumanEdited)
			assert.Equal(t, "scheduled rebuild", item.Reason)

			found = true
		}
	}

	assert.True(t, found, "architecture.md should be in plan with human_edited=true")
}

func TestBuildRefreshPlan_RepoFilter(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{
		{Name: "core", URL: "git@github.com:o/c.git", Primary: true},
		{Name: "runner", URL: "git@github.com:o/r.git"},
	}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	plan, err := svc.BuildRefreshPlan(ctx, "test-project", "runner")
	require.NoError(t, err)

	for _, item := range plan.Items {
		assert.Equal(t, "runner", item.Repo)
	}
}

func TestBuildRefreshPlan_DefaultsToPrimaryForMultiRepo(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{
		{Name: "core", URL: "git@github.com:o/c.git", Primary: true},
		{Name: "runner", URL: "git@github.com:o/r.git"},
	}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	plan, err := svc.BuildRefreshPlan(ctx, "test-project", "")
	require.NoError(t, err)

	for _, item := range plan.Items {
		assert.Equal(t, "core", item.Repo, "default should be primary repo")
	}
}

func TestBuildRefreshPlan_UnknownRepo(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	_, err = svc.BuildRefreshPlan(ctx, "test-project", "doesnotexist")
	assert.Error(t, err)
}

func TestBuildRefreshPlan_EmptyReposList(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	plan, err := svc.BuildRefreshPlan(context.Background(), "test-project", "")
	require.NoError(t, err)
	assert.Empty(t, plan.Items)
}

func TestWriteKnowledgeDocs_RollsBackOnCommitFailure(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a prior write so there's state to restore.
	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "v1"},
		HeadCommit: "sha-v1",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:tester",
	})
	require.NoError(t, err)

	// Inject a failure for the next commit.
	svc.setKnowledgeCommitFnForTest(func(_ context.Context, _ []string, _ string) error {
		return errors.New("simulated commit failure")
	})

	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "v2"},
		HeadCommit: "sha-v2",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "commit knowledge docs")

	// Disk state must be the prior write, not the failed one.
	out, err := svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.NoError(t, err)
	assert.Equal(t, "v1", out.Content)

	meta, err := svc.store.ReadKnowledgeMeta(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, "sha-v1", meta.Repos["core"].LastBuiltCommit)
}

func TestWriteKnowledgeDocs_RollbackDeletesDocsThatDidNotExist(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Inject commit failure from the start — doc has never existed.
	svc.setKnowledgeCommitFnForTest(func(_ context.Context, _ []string, _ string) error {
		return errors.New("boom")
	})

	_, err := svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "v1"},
		HeadCommit: "sha",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:tester",
	})
	require.Error(t, err)

	// Doc must not exist on disk after the failed write.
	_, err = svc.ReadKnowledgeDoc(ctx, "test-project", "core", "architecture.md")
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrKnowledgeDocNotFound)
}

func TestWriteKnowledgeDocs_RefreshRejectsBlankAgentID(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "x"},
		HeadCommit: "abc1234",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent_id required")
}

func TestWriteKnowledgeDocs_RefreshRejectsBlankHeadCommit(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "x"},
		HeadCommit: "",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:alice",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "head_commit required")
}

func TestWriteKnowledgeDocs_RefreshRejectsNonHumanAgentID(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:       map[string]string{"architecture.md": "x"},
		HeadCommit: "abc1234",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "agent-foo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "human-only")
}

func TestWriteKnowledgeDocs_InvalidDocNameReturnsSentinel(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.WriteKnowledgeDocs(context.Background(), WriteKnowledgeDocsInput{
		Project: "test-project", Repo: "core",
		Docs:    map[string]string{"random.md": "x"},
		Source:  KnowledgeWriteSourceEdit,
		AgentID: "human:tester",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidKnowledgeDoc)
}

func TestBuildRefreshPlan_ReasonsOnlyMissingOrScheduled(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.Repo{{Name: "core", URL: "git@github.com:o/c.git", Primary: true}}
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	// Plan with no prior writes — all docs should be "missing".
	plan, err := svc.BuildRefreshPlan(ctx, "test-project", "")
	require.NoError(t, err)

	for _, item := range plan.Items {
		assert.Equal(t, "missing", item.Reason, "doc %q", item.Doc)
	}

	// Write one doc as a refresh.
	_, err = svc.WriteKnowledgeDocs(ctx, WriteKnowledgeDocsInput{
		Project:    "test-project",
		Repo:       "core",
		Docs:       map[string]string{"architecture.md": "x"},
		HeadCommit: "sha-1",
		Source:     KnowledgeWriteSourceRefresh,
		AgentID:    "human:tester",
	})
	require.NoError(t, err)

	// Plan again — architecture.md should now be "scheduled rebuild", others still "missing".
	plan2, err := svc.BuildRefreshPlan(ctx, "test-project", "")
	require.NoError(t, err)

	for _, item := range plan2.Items {
		if item.Doc == "architecture.md" {
			assert.Equal(t, "scheduled rebuild", item.Reason)
		} else {
			assert.Equal(t, "missing", item.Reason)
		}

		assert.NotEqual(t, "stale", item.Reason, "stale should not be returned")
	}
}
