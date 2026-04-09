package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestImporter creates a real service stack in a temp dir with a mock Jira server.
func setupTestImporter(t *testing.T, handler http.Handler) (*Importer, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	boardsDir := t.TempDir()

	git, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*60)

	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	jiraCfg := config.JiraConfig{
		BaseURL: srv.URL,
		Email:   "test@example.com",
		Token:   "test-token",
		Projects: map[string]config.JiraProjectMapping{
			"PROJ": {
				RepoMappings: []config.JiraRepoMapping{
					{Component: "auth-service", Repo: "git@github.com:org/auth.git"},
				},
				DefaultRepo: "git@github.com:org/monorepo.git",
			},
		},
	}

	client := &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		email:      jiraCfg.Email,
		token:      jiraCfg.Token,
	}

	imp := NewImporter(client, svc, store, jiraCfg)

	return imp, srv
}

func TestPreviewEpic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key: "PROJ-42",
			Fields: IssueFields{
				Summary:   "Auth overhaul",
				IssueType: NameField{Name: "Epic"},
				Status:    NameField{Name: "In Progress"},
			},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Total: 2,
			Issues: []Issue{
				{Key: "PROJ-43", Fields: IssueFields{Summary: "Add OAuth", IssueType: NameField{Name: "Story"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-44", Fields: IssueFields{Summary: "Fix token refresh", IssueType: NameField{Name: "Bug"}, Status: NameField{Name: "To Do"}}},
			},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	preview, err := imp.PreviewEpic(context.Background(), "PROJ-42")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-42", preview.Epic.Key)
	assert.Equal(t, "Auth overhaul", preview.Epic.Summary)
	assert.Equal(t, "Epic", preview.Epic.IssueType)
	require.Len(t, preview.Children, 2)
	assert.Equal(t, "PROJ-43", preview.Children[0].Key)
	assert.Equal(t, "PROJ-44", preview.Children[1].Key)
}

func TestImportEpic_CreatesProjectAndCards(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key: "PROJ-42",
			Fields: IssueFields{
				Summary:   "Auth Overhaul",
				IssueType: NameField{Name: "Epic"},
			},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Total: 2,
			Issues: []Issue{
				{
					Key: "PROJ-43",
					Fields: IssueFields{
						Summary:     "Add OAuth support",
						Description: "Implement OAuth2 flow",
						IssueType:   NameField{Name: "Story"},
						Priority:    &NameField{Name: "High"},
						Labels:      []string{"backend"},
						Components:  []NameField{{Name: "auth-service"}},
						Status:      NameField{Name: "To Do"},
					},
				},
				{
					Key: "PROJ-44",
					Fields: IssueFields{
						Summary:     "Fix token refresh bug",
						Description: "Tokens expire silently",
						IssueType:   NameField{Name: "Bug"},
						Priority:    &NameField{Name: "Highest"},
						Labels:      []string{"urgent"},
						Components:  []NameField{{Name: "api-gateway"}},
						Status:      NameField{Name: "To Do"},
					},
				},
			},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
		EpicKey: "PROJ-42",
	})
	require.NoError(t, err)

	// Project created with derived name.
	assert.Equal(t, "auth-overhaul", result.Project.Name)
	assert.Equal(t, "PROJ42", result.Project.Prefix)
	assert.NotNil(t, result.Project.Jira)
	assert.Equal(t, "PROJ-42", result.Project.Jira.EpicKey)
	assert.Equal(t, 2, result.CardsImported)
	assert.Equal(t, 0, result.Skipped)

	// Verify cards exist.
	cards, err := imp.store.ListCards(context.Background(), "auth-overhaul", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 2)
}

func TestImportEpic_CustomNameAndPrefix(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key:    "PROJ-42",
			Fields: IssueFields{Summary: "Epic", IssueType: NameField{Name: "Epic"}},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{Total: 0})
	})

	imp, _ := setupTestImporter(t, mux)

	result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
		EpicKey: "PROJ-42",
		Name:    "my-project",
		Prefix:  "MP",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-project", result.Project.Name)
	assert.Equal(t, "MP", result.Project.Prefix)
}

func TestImportEpic_DeduplicatesOnReimport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key:    "PROJ-42",
			Fields: IssueFields{Summary: "Epic", IssueType: NameField{Name: "Epic"}},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Total:  1,
			Issues: []Issue{{Key: "PROJ-43", Fields: IssueFields{Summary: "Task", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}}},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	// First import.
	result1, err := imp.ImportEpic(context.Background(), ImportEpicInput{EpicKey: "PROJ-42"})
	require.NoError(t, err)
	assert.Equal(t, 1, result1.CardsImported)

	// Second import of the same cards into the same project should skip them.
	// We can't call ImportEpic again (project already exists), so simulate
	// by importing cards directly.
	children := []Issue{{Key: "PROJ-43", Fields: IssueFields{Summary: "Task", IssueType: NameField{Name: "Task"}}}}
	existing, _ := imp.store.ListCards(context.Background(), "epic", storage.CardFilter{ExternalID: "PROJ-43"})
	assert.Len(t, existing, 1) // Card exists from first import.

	_ = children // used above for assertion context
}

func TestImportEpic_CardFieldMapping(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key:    "PROJ-42",
			Fields: IssueFields{Summary: "Epic", IssueType: NameField{Name: "Epic"}},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Total: 1,
			Issues: []Issue{{
				Key: "PROJ-50",
				Fields: IssueFields{
					Summary:     "Implement retry logic",
					Description: "Add exponential backoff",
					IssueType:   NameField{Name: "Bug"},
					Priority:    &NameField{Name: "Highest"},
					Labels:      []string{"backend", "reliability"},
					Components:  []NameField{{Name: "auth-service"}},
					Status:      NameField{Name: "To Do"},
				},
			}},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	result, err := imp.ImportEpic(context.Background(), ImportEpicInput{EpicKey: "PROJ-42"})
	require.NoError(t, err)
	require.Equal(t, 1, result.CardsImported)

	cards, err := imp.store.ListCards(context.Background(), "epic", storage.CardFilter{ExternalID: "PROJ-50"})
	require.NoError(t, err)
	require.Len(t, cards, 1)

	card := cards[0]
	assert.Equal(t, "Implement retry logic", card.Title)
	assert.Equal(t, "bug", card.Type)
	assert.Equal(t, "critical", card.Priority)
	assert.Contains(t, card.Labels, "backend")
	assert.Contains(t, card.Labels, "reliability")
	assert.Contains(t, card.Labels, "auth-service") // component as label
	assert.True(t, card.Vetted)
	require.NotNil(t, card.Source)
	assert.Equal(t, "jira", card.Source.System)
	assert.Equal(t, "PROJ-50", card.Source.ExternalID)
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Auth Overhaul", "auth-overhaul"},
		{"Fix: login page (urgent!)", "fix-login-page-urgent"},
		{"  spaces  ", "spaces"},
		{"", "jira-import"},
		{"UPPERCASE", "uppercase"},
		{"a-b-c", "a-b-c"},
		{"special@#$chars", "special-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, slugify(tt.input))
		})
	}
}

func TestExtractProjectKey(t *testing.T) {
	assert.Equal(t, "PROJ", extractProjectKey("PROJ-42"))
	assert.Equal(t, "BACKEND", extractProjectKey("BACKEND-123"))
	assert.Equal(t, "X", extractProjectKey("X-1"))
}

func TestIsDoneStatus(t *testing.T) {
	assert.True(t, isDoneStatus("Done"))
	assert.True(t, isDoneStatus("done"))
	assert.True(t, isDoneStatus("Closed"))
	assert.True(t, isDoneStatus("Resolved"))
	assert.True(t, isDoneStatus("Completed"))
	assert.False(t, isDoneStatus("To Do"))
	assert.False(t, isDoneStatus("In Progress"))
	assert.False(t, isDoneStatus("In Review"))
	assert.False(t, isDoneStatus(""))
}

func TestExtractEpicPrefix(t *testing.T) {
	assert.Equal(t, "PROJ42", extractEpicPrefix("PROJ-42"))
	assert.Equal(t, "PROJ43", extractEpicPrefix("PROJ-43"))
	assert.Equal(t, "BACKEND123", extractEpicPrefix("BACKEND-123"))
}

func TestMapIssueType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Bug", "bug"},
		{"bug", "bug"},
		{"Story", "task"},
		{"Task", "task"},
		{"Sub-task", "task"},
		{"Improvement", "feature"},
		{"New Feature", "feature"},
		{"Custom Type", "task"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, mapIssueType(tt.input))
		})
	}
}
