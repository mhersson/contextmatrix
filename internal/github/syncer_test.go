package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
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
		{
			name: "explicit config with allowed repo URL",
			cfg: &board.ProjectConfig{
				Repo: "https://github.com/actual-org/actual-repo.git",
				GitHub: &board.GitHubImportConfig{
					ImportIssues: true, Owner: "explicit-org", Repo: "explicit-repo",
				},
			},
			allowedHosts: defaultHosts,
			wantOwner:    "explicit-org",
			wantRepo:     "explicit-repo",
		},
		{
			name: "explicit config rejected when repo URL is non-allowed host",
			cfg: &board.ProjectConfig{
				Repo: "https://gitlab.com/other-org/other-repo.git",
				GitHub: &board.GitHubImportConfig{
					ImportIssues: true, Owner: "explicit-org", Repo: "explicit-repo",
				},
			},
			allowedHosts: defaultHosts,
			// Explicit override must be rejected; repo URL is on a non-allowed host.
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
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	cfg := &board.ProjectConfig{
		Name:       projectName,
		Prefix:     "TEST",
		NextID:     1,
		Repo:       "git@github.com:testorg/testrepo.git",
		States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
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

	git, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return boardsDir, svc, store
}

func TestSyncProject_ImportsNewIssues(t *testing.T) {
	issues := []Issue{
		{
			Number: 1, Title: "First issue", Body: "Body 1", HTMLURL: "https://github.com/testorg/testrepo/issues/1",
			Labels: []Label{{Name: "bug"}, {Name: "help wanted"}},
		},
		{Number: 2, Title: "Second issue", Body: "Body 2", HTMLURL: "https://github.com/testorg/testrepo/issues/2"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	ghCfg := &board.GitHubImportConfig{ImportIssues: true}
	boardsDir, svc, store := setupTestProject(t, "test-project", ghCfg)

	p, err := githubauth.NewPATProvider("t")
	require.NoError(t, err)

	client := NewClientWithBaseURL(p, srv.URL)
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	imported, err := syncer.syncProject(ctx, cfg, client, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 2, imported)

	// Verify cards were created.
	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 2)

	// Build a map keyed by ExternalID to avoid relying on ListCards iteration order.
	byExtID := make(map[string]*board.Card, len(cards))
	for _, c := range cards {
		require.NotNil(t, c.Source, "card %s missing Source", c.ID)
		byExtID[c.Source.ExternalID] = c
	}

	first := byExtID["testorg/testrepo#1"]
	require.NotNil(t, first, "card for testorg/testrepo#1 not found")
	assert.Equal(t, "First issue", first.Title)
	assert.Equal(t, "task", first.Type)
	assert.Equal(t, "medium", first.Priority)
	assert.Equal(t, "todo", first.State)
	assert.Equal(t, "github", first.Source.System)
	assert.Equal(t, "testorg/testrepo#1", first.Source.ExternalID)
	assert.Equal(t, []string{"bug", "help wanted"}, first.Labels)

	second := byExtID["testorg/testrepo#2"]
	require.NotNil(t, second, "card for testorg/testrepo#2 not found")
	assert.Equal(t, "Second issue", second.Title)
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

	p, err := githubauth.NewPATProvider("t")
	require.NoError(t, err)

	client := NewClientWithBaseURL(p, srv.URL)
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	// First sync: creates the card.
	imported, err := syncer.syncProject(ctx, cfg, client, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	// Reload config (next_id was bumped).
	cfg, err = board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	// Second sync: same issue, should be skipped.
	imported, err = syncer.syncProject(ctx, cfg, client, "testorg", "testrepo")
	require.NoError(t, err)
	assert.Equal(t, 0, imported)

	// Still only 1 card.
	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
}

func TestSafeSyncAll_DoesNotPropagateSync(t *testing.T) {
	// Set up a project with GitHub import enabled and an explicit owner/repo so
	// syncAll will attempt to call syncProject.  A nil client causes syncProject
	// to panic on the first FetchOpenIssues call, which exercises the recover()
	// in safeSyncAll.
	ghCfg := &board.GitHubImportConfig{
		ImportIssues: true,
		Owner:        "testorg",
		Repo:         "testrepo",
	}
	boardsDir, svc, store := setupTestProject(t, "test-project", ghCfg)

	// nil client will panic inside syncProject → FetchOpenIssues.
	syncer := NewSyncer(svc, store, nil, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()

	// safeSyncAll must return normally - the panic must not escape.
	require.NotPanics(t, func() {
		syncer.safeSyncAll(ctx)
	})
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

	p, err := githubauth.NewPATProvider("t")
	require.NoError(t, err)

	client := NewClientWithBaseURL(p, srv.URL)
	syncer := NewSyncer(svc, store, client, boardsDir, 5*time.Minute, []string{"github.com"})

	ctx := context.Background()
	cfg, err := board.LoadProjectConfig(filepath.Join(boardsDir, "test-project"))
	require.NoError(t, err)

	imported, err := syncer.syncProject(ctx, cfg, client, "o", "r")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)

	cards, err := store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, "bug", cards[0].Type)
	assert.Equal(t, "high", cards[0].Priority)
}

// TestSyncAll_SetClientFor_SkipsProjectOnResolveError asserts that when a
// per-project client resolver is installed via SetClientFor, a resolution
// error for one project is logged and that project is skipped for the
// cycle - it must not panic, and it must not prevent other projects from
// syncing normally (fail-closed: a broken binding never falls back to the
// syncer's constructor-injected static client).
func TestSyncAll_SetClientFor_SkipsProjectOnResolveError(t *testing.T) {
	boardsDir := t.TempDir()

	makeProject := func(name string) {
		projectDir := filepath.Join(boardsDir, name)
		require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

		cfg := &board.ProjectConfig{
			Name:       name,
			Prefix:     "P",
			NextID:     1,
			Repo:       "git@github.com:testorg/" + name + ".git",
			States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
			Types:      []string{"task"},
			Priorities: []string{"medium"},
			Transitions: map[string][]string{
				"todo":        {"in_progress", "not_planned"},
				"in_progress": {"review", "todo"},
				"review":      {"done", "in_progress"},
				"done":        {},
				"stalled":     {"todo"},
				"not_planned": {"todo"},
			},
			GitHub: &board.GitHubImportConfig{
				ImportIssues: true,
				Owner:        "testorg",
				Repo:         name,
			},
		}
		require.NoError(t, board.SaveProjectConfig(projectDir, cfg))
	}

	makeProject("proj-a")
	makeProject("proj-b")

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	git, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	issues := []Issue{
		{Number: 1, Title: "Issue for proj-b", HTMLURL: "https://github.com/testorg/proj-b/issues/1"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "100")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	p, err := githubauth.NewPATProvider("t")
	require.NoError(t, err)

	okClient := NewClientWithBaseURL(p, srv.URL)

	// Constructor-injected static client is deliberately nil: it must never
	// be used as a fallback for a broken binding, and a working SetClientFor
	// resolution for proj-b must not depend on it either.
	syncer := NewSyncer(svc, store, nil, boardsDir, 5*time.Minute, []string{"github.com"})

	var resolvedFor []string

	syncer.SetClientFor(func(_ context.Context, cfg *board.ProjectConfig) (*Client, error) {
		resolvedFor = append(resolvedFor, cfg.Name)

		if cfg.Name == "proj-a" {
			return nil, errors.New("credential unavailable")
		}

		return okClient, nil
	})

	ctx := context.Background()

	require.NotPanics(t, func() {
		syncer.syncAll(ctx)
	})

	assert.ElementsMatch(t, []string{"proj-a", "proj-b"}, resolvedFor)

	cardsA, err := store.ListCards(ctx, "proj-a", storage.CardFilter{})
	require.NoError(t, err)
	assert.Empty(t, cardsA, "proj-a's resolution error must skip the project, not panic or fall back")

	cardsB, err := store.ListCards(ctx, "proj-b", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cardsB, 1, "proj-b must still sync normally despite proj-a's failure")
	assert.Equal(t, "Issue for proj-b", cardsB[0].Title)
}
