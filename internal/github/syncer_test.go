package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOwnerRepo(t *testing.T) {
	defaultHosts := []string{"github.com"}

	tests := []struct {
		name         string
		cfg          *board.ProjectConfig
		allowedHosts []string
		wantOwner    string
		wantRepo     string
	}{
		{
			name: "explicit config",
			cfg: &board.ProjectConfig{
				GitHub: &board.GitHubImportConfig{
					ImportIssues: true, Owner: "explicit-org", Repo: "explicit-repo",
				},
			},
			allowedHosts: defaultHosts,
			wantOwner:    "explicit-org",
			wantRepo:     "explicit-repo",
		},
		{
			name: "from SSH repo URL",
			cfg: &board.ProjectConfig{
				Repo:   "git@github.com:myorg/myrepo.git",
				GitHub: &board.GitHubImportConfig{ImportIssues: true},
			},
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
		},
		{
			name: "non-GitHub URL",
			cfg: &board.ProjectConfig{
				Repo:   "git@gitlab.com:myorg/myrepo.git",
				GitHub: &board.GitHubImportConfig{ImportIssues: true},
			},
			allowedHosts: defaultHosts,
		},
		{
			name:         "nil GitHub config",
			cfg:          &board.ProjectConfig{Repo: "git@github.com:myorg/myrepo.git"},
			allowedHosts: defaultHosts,
		},
		{
			name: "partial explicit falls back to URL",
			cfg: &board.ProjectConfig{
				Repo: "https://github.com/url-org/url-repo.git",
				GitHub: &board.GitHubImportConfig{
					ImportIssues: true, Owner: "partial-org",
				},
			},
			allowedHosts: defaultHosts,
			wantOwner:    "url-org",
			wantRepo:     "url-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo := resolveOwnerRepo(tt.cfg, tt.allowedHosts)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

// setupTestProject creates a minimal project in a temp dir with a valid .board.yaml
// and returns the boards dir, a CardService, and a FilesystemStore.
func setupTestProject(t *testing.T, projectName string, ghCfg *board.GitHubImportConfig) (string, *service.CardService, *storage.FilesystemStore) {
	t.Helper()

	boardsDir := t.TempDir()
	projectDir := filepath.Join(boardsDir, projectName)
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))

	cfg := &board.ProjectConfig{
		Name:   projectName,
		Prefix: "TEST",
		NextID: 1,
		Repo:   "git@github.com:testorg/testrepo.git",
		States: []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:  []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {},
			"stalled":     {"todo"},
			"not_planned": {"todo"},
		},
		GitHub: ghCfg,
	}
	require.NoError(t, board.SaveProjectConfig(projectDir, cfg))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return boardsDir, svc, store
}

func TestSyncProject_ImportsNewIssues(t *testing.T) {
	issues := []Issue{
		{Number: 1, Title: "First issue", Body: "Body 1", HTMLURL: "https://github.com/testorg/testrepo/issues/1",
			Labels: []Label{{Name: "bug"}, {Name: "help wanted"}}},
		{Number: 2, Title: "Second issue", Body: "Body 2", HTMLURL: "https://github.com/testorg/testrepo/issues/2"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	ghCfg := &board.GitHubImportConfig{ImportIssues: true}
	boardsDir, svc, store := setupTestProject(t, "test-project", ghCfg)

	client := &Client{httpClient: srv.Client(), token: "t", baseURL: srv.URL}
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	imported, err := syncer.syncProject(ctx, cfg, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 2, imported)

	// Verify cards were created.
	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 2)

	assert.Equal(t, "First issue", cards[0].Title)
	assert.Equal(t, "task", cards[0].Type)
	assert.Equal(t, "medium", cards[0].Priority)
	assert.Equal(t, "todo", cards[0].State)
	require.NotNil(t, cards[0].Source)
	assert.Equal(t, "github", cards[0].Source.System)
	assert.Equal(t, "testorg/testrepo#1", cards[0].Source.ExternalID)
	assert.Equal(t, []string{"bug", "help wanted"}, cards[0].Labels)
}

func TestSyncProject_SkipsDuplicates(t *testing.T) {
	issues := []Issue{
		{Number: 1, Title: "Existing issue", Body: "Body", HTMLURL: "https://github.com/testorg/testrepo/issues/1"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	ghCfg := &board.GitHubImportConfig{ImportIssues: true}
	boardsDir, svc, store := setupTestProject(t, "test-project", ghCfg)

	client := &Client{httpClient: srv.Client(), token: "t", baseURL: srv.URL}
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	// First sync: creates the card.
	imported, err := syncer.syncProject(ctx, cfg, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	// Reload config (next_id was bumped).
	cfg, err = board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	// Second sync: same issue, should be skipped.
	imported, err = syncer.syncProject(ctx, cfg, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 0, imported)

	// Still only 1 card.
	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
}

func TestSyncProject_CustomTypeAndPriority(t *testing.T) {
	issues := []Issue{
		{Number: 5, Title: "Bug from GH", HTMLURL: "https://github.com/o/r/issues/5"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	ghCfg := &board.GitHubImportConfig{
		ImportIssues:    true,
		CardType:        "bug",
		DefaultPriority: "high",
	}
	boardsDir, svc, store := setupTestProject(t, "test-project", ghCfg)

	client := &Client{httpClient: srv.Client(), token: "t", baseURL: srv.URL}
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	imported, err := syncer.syncProject(ctx, cfg, "o", "r")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, "bug", cards[0].Type)
	assert.Equal(t, "high", cards[0].Priority)
}
