package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// twoRepoComposite builds repo "one" holding alpha and repo "two" holding
// beta, in that config order.
func twoRepoComposite(t *testing.T) (*Composite, string, string) {
	t.Helper()

	one := t.TempDir()
	two := t.TempDir()
	setupTestProject(t, one, "alpha", "ALPHA")
	setupTestProject(t, two, "beta", "BETA")

	sOne, err := NewFilesystemStore(one)
	require.NoError(t, err)

	sTwo, err := NewFilesystemStore(two)
	require.NoError(t, err)

	c, err := NewComposite(NamedStore{Name: "one", Store: sOne}, NamedStore{Name: "two", Store: sTwo})
	require.NoError(t, err)

	return c, one, two
}

func TestComposite_ListProjectsIgnoresEntriesTheOwnerTableDoesNotKnow(t *testing.T) {
	c, _, _ := twoRepoComposite(t)
	ctx := context.Background()

	// alpha lives in repo "one" (index 0): a missing owner key reads as 0
	// under the single-value form, so dropping it from the table is what
	// exposes the zero-value trap the two-value read closes.
	c.mu.Lock()
	delete(c.owner, "alpha")
	c.mu.Unlock()

	projects, err := c.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "beta", projects[0].Name)

	_, ok := c.RepoOf("alpha")
	assert.False(t, ok)
}

func TestComposite_RoutesByProjectInConfigOrder(t *testing.T) {
	c, one, two := twoRepoComposite(t)
	ctx := context.Background()

	assert.Equal(t, []string{"one", "two"}, c.RepoNames())

	repo, ok := c.RepoOf("alpha")
	require.True(t, ok)
	assert.Equal(t, "one", repo)

	repo, ok = c.RepoOf("beta")
	require.True(t, ok)
	assert.Equal(t, "two", repo)

	_, ok = c.RepoOf("gamma")
	assert.False(t, ok)

	projects, err := c.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	assert.Equal(t, "alpha", projects[0].Name)
	assert.Equal(t, "one", projects[0].BoardsRepo)
	assert.Equal(t, "beta", projects[1].Name)
	assert.Equal(t, "two", projects[1].BoardsRepo)

	cfg, err := c.GetProject(ctx, "beta")
	require.NoError(t, err)
	assert.Equal(t, "two", cfg.BoardsRepo)

	card := &board.Card{ID: "BETA-001", Project: "beta", Title: "t", State: "todo", Type: "task", Priority: "medium"}
	require.NoError(t, c.CreateCard(ctx, "beta", card))
	assert.FileExists(t, filepath.Join(two, "beta", "tasks", "BETA-001.md"))
	assert.NoFileExists(t, filepath.Join(one, "beta", "tasks", "BETA-001.md"))

	got, err := c.GetCard(ctx, "beta", "BETA-001")
	require.NoError(t, err)
	assert.Equal(t, "t", got.Title)

	_, err = c.GetCard(ctx, "gamma", "G-001")
	require.ErrorIs(t, err, ErrProjectNotFound)

	// BoardsRepo never reaches the file: the stamp is a read-side annotation.
	require.NoError(t, c.SaveProject(ctx, cfg))

	data, err := os.ReadFile(filepath.Join(two, "beta", ".board.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "boards_repo")
}

func TestComposite_DuplicateProjectIsHiddenBehindTheEarlierRepo(t *testing.T) {
	c, _, two := twoRepoComposite(t)
	ctx := context.Background()

	setupTestProject(t, two, "alpha", "ALPHA")
	require.NoError(t, c.ReloadRepo(ctx, "two"))

	hidden := c.Hidden()
	require.Len(t, hidden, 1)
	assert.Equal(t, HiddenProject{Name: "alpha", Repo: "two", VisibleIn: "one"}, hidden[0])

	repo, ok := c.RepoOf("alpha")
	require.True(t, ok)
	assert.Equal(t, "one", repo)

	projects, err := c.ListProjects(ctx)
	require.NoError(t, err)

	names := map[string]string{}
	for _, p := range projects {
		names[p.Name] = p.BoardsRepo
	}

	assert.Equal(t, map[string]string{"alpha": "one", "beta": "two"}, names)

	viewTwo, err := c.View("two")
	require.NoError(t, err)

	visible, err := viewTwo.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, visible, 1)
	assert.Equal(t, "beta", visible[0].Name)
	assert.Equal(t, hidden, viewTwo.Hidden())

	viewOne, err := c.View("one")
	require.NoError(t, err)
	assert.Empty(t, viewOne.Hidden())
}

func TestComposite_ReloadRepoRegistersAProjectThatArrivedOnDisk(t *testing.T) {
	c, _, two := twoRepoComposite(t)
	ctx := context.Background()

	setupTestProject(t, two, "gamma", "GAMMA")

	_, ok := c.RepoOf("gamma")
	assert.False(t, ok, "not visible before the reload")

	viewTwo, err := c.View("two")
	require.NoError(t, err)
	require.NoError(t, viewTwo.ReloadIndex(ctx))

	repo, ok := c.RepoOf("gamma")
	require.True(t, ok)
	assert.Equal(t, "two", repo)
}

func TestComposite_SaveProjectInTargetsTheNamedRepo(t *testing.T) {
	c, one, two := twoRepoComposite(t)
	ctx := context.Background()

	delta := validProjectConfig("delta", "DELTA")
	require.NoError(t, c.SaveProjectIn(ctx, "two", delta))
	assert.FileExists(t, filepath.Join(two, "delta", ".board.yaml"))
	assert.NoFileExists(t, filepath.Join(one, "delta", ".board.yaml"))

	repo, ok := c.RepoOf("delta")
	require.True(t, ok)
	assert.Equal(t, "two", repo)

	err := c.SaveProjectIn(ctx, "one", validProjectConfig("delta", "DELTA"))
	require.ErrorIs(t, err, ErrProjectExists)

	err = c.SaveProjectIn(ctx, "three", validProjectConfig("eps", "EPS"))
	require.ErrorIs(t, err, ErrUnknownRepo)

	err = c.SaveProject(ctx, validProjectConfig("zeta", "ZETA"))
	assert.ErrorIs(t, err, ErrProjectNotFound, "a new project needs SaveProjectIn")
}

func TestComposite_DeleteProjectUnhidesTheLaterCopy(t *testing.T) {
	c, _, two := twoRepoComposite(t)
	ctx := context.Background()

	setupTestProject(t, two, "alpha", "ALPHA")
	require.NoError(t, c.ReloadRepo(ctx, "two"))
	require.Len(t, c.Hidden(), 1)

	require.NoError(t, c.DeleteProject(ctx, "alpha"))

	assert.Empty(t, c.Hidden())

	repo, ok := c.RepoOf("alpha")
	require.True(t, ok)
	assert.Equal(t, "two", repo)
}

func TestComposite_ListProjectsReturnsEmptySliceNeverNil(t *testing.T) {
	ctx := context.Background()

	empty := t.TempDir()
	sEmpty, err := NewFilesystemStore(empty)
	require.NoError(t, err)

	populated := t.TempDir()
	setupTestProject(t, populated, "beta", "BETA")
	sPopulated, err := NewFilesystemStore(populated)
	require.NoError(t, err)

	soloEmpty, err := NewComposite(NamedStore{Name: "solo", Store: sEmpty})
	require.NoError(t, err)

	twoRepos, err := NewComposite(
		NamedStore{Name: "one", Store: sEmpty},
		NamedStore{Name: "two", Store: sPopulated},
	)
	require.NoError(t, err)

	viewOfEmptyRepo, err := twoRepos.View("one")
	require.NoError(t, err)

	tests := []struct {
		name string
		list func(context.Context) ([]board.ProjectConfig, error)
	}{
		{"composite with no visible projects", soloEmpty.ListProjects},
		{"repo view of an empty repo", viewOfEmptyRepo.ListProjects},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects, err := tt.list(ctx)
			require.NoError(t, err)
			assert.NotNil(t, projects)
			assert.Empty(t, projects)
		})
	}
}

func TestNewComposite_RejectsBadInput(t *testing.T) {
	s, err := NewFilesystemStore(t.TempDir())
	require.NoError(t, err)

	_, err = NewComposite()
	require.Error(t, err)

	_, err = NewComposite(NamedStore{Name: "a", Store: s}, NamedStore{Name: "a", Store: s})
	require.ErrorContains(t, err, "duplicate repo name")

	_, err = NewComposite(NamedStore{Name: "", Store: s})
	require.Error(t, err)

	_, err = NewComposite(NamedStore{Name: "a", Store: nil})
	require.Error(t, err)

	notFound := ErrUnknownRepo
	assert.ErrorIs(t, notFound, ErrUnknownRepo)
}
