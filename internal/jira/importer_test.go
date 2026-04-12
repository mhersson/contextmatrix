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
	assert.Equal(t, "PROJ", result.Project.Prefix)
	assert.NotNil(t, result.Project.Jira)
	assert.Equal(t, "PROJ-42", result.Project.Jira.EpicKey)
	assert.Equal(t, 2, result.CardsImported)
	assert.Equal(t, 0, result.Skipped)

	// Verify cards exist with Jira issue keys as IDs.
	cards, err := imp.store.ListCards(context.Background(), "auth-overhaul", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 2)

	// Collect card IDs for assertion (order may vary).
	cardIDs := make(map[string]bool, len(cards))
	for _, c := range cards {
		cardIDs[c.ID] = true
	}
	assert.True(t, cardIDs["PROJ-43"], "expected card with ID PROJ-43")
	assert.True(t, cardIDs["PROJ-44"], "expected card with ID PROJ-44")
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
		_ = json.NewEncoder(w).Encode(searchResult{})
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
			Issues: []Issue{{Key: "PROJ-43", Fields: IssueFields{Summary: "Task", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}}},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	// First import.
	result1, err := imp.ImportEpic(context.Background(), ImportEpicInput{EpicKey: "PROJ-42"})
	require.NoError(t, err)
	assert.Equal(t, 1, result1.CardsImported)

	// Second import of the same epic must succeed (reuses the existing project)
	// and skip already-imported cards.
	result2, err := imp.ImportEpic(context.Background(), ImportEpicInput{EpicKey: "PROJ-42"})
	require.NoError(t, err)
	assert.Equal(t, 0, result2.CardsImported) // PROJ-43 already imported
	assert.Equal(t, 1, result2.Skipped)
	assert.Equal(t, result1.Project.Name, result2.Project.Name) // same project reused

	// Verify only one card exists (no duplicates created).
	cards, err := imp.store.ListCards(context.Background(), result1.Project.Name, storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
}

func TestImportEpic_ReimportWithSelectedKeys(t *testing.T) {
	// Epic has three children; first import brings in only PROJ-43.
	// Second import with SelectedKeys=["PROJ-44"] should import PROJ-44 into the
	// existing project without error and without duplicating PROJ-43.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key:    "PROJ-42",
			Fields: IssueFields{Summary: "Epic", IssueType: NameField{Name: "Epic"}},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Issues: []Issue{
				{Key: "PROJ-43", Fields: IssueFields{Summary: "Task A", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-44", Fields: IssueFields{Summary: "Task B", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}},
			},
		})
	})

	imp, _ := setupTestImporter(t, mux)

	// First import: bring in only PROJ-43.
	result1, err := imp.ImportEpic(context.Background(), ImportEpicInput{
		EpicKey:      "PROJ-42",
		SelectedKeys: []string{"PROJ-43"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result1.CardsImported)
	assert.Equal(t, 1, result1.Skipped) // PROJ-44 not selected

	// Second import: bring in PROJ-44 from the already-existing project.
	result2, err := imp.ImportEpic(context.Background(), ImportEpicInput{
		EpicKey:      "PROJ-42",
		SelectedKeys: []string{"PROJ-44"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result2.CardsImported) // PROJ-44 newly imported
	assert.Equal(t, 1, result2.Skipped)       // PROJ-43 not selected
	assert.Equal(t, result1.Project.Name, result2.Project.Name) // same project reused

	// Both cards must exist in the project with no duplicates.
	cards, err := imp.store.ListCards(context.Background(), result1.Project.Name, storage.CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)

	cardIDs := make(map[string]bool, len(cards))
	for _, c := range cards {
		cardIDs[c.ID] = true
	}
	assert.True(t, cardIDs["PROJ-43"], "expected PROJ-43 card")
	assert.True(t, cardIDs["PROJ-44"], "expected PROJ-44 card")
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
	assert.Equal(t, "PROJ-50 Implement retry logic", card.Title)
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


func TestImportEpic_SelectedKeys(t *testing.T) {
	// Three children: PROJ-43 (To Do), PROJ-44 (To Do), PROJ-45 (Done).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/issue/PROJ-42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Issue{
			Key:    "PROJ-42",
			Fields: IssueFields{Summary: "Epic", IssueType: NameField{Name: "Epic"}},
		})
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResult{
			Issues: []Issue{
				{Key: "PROJ-43", Fields: IssueFields{Summary: "Task A", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-44", Fields: IssueFields{Summary: "Task B", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-45", Fields: IssueFields{Summary: "Task C", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "Done"}}},
			},
		})
	})

	t.Run("empty selected_keys imports all non-done", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey:      "PROJ-42",
			Name:         "all-non-done",
			Prefix:       "AND",
			SelectedKeys: []string{},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, result.CardsImported) // PROJ-43, PROJ-44
		assert.Equal(t, 1, result.Skipped)       // PROJ-45 (done)
	})

	t.Run("nil selected_keys imports all non-done", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey: "PROJ-42",
			Name:    "nil-selected",
			Prefix:  "NIL",
		})
		require.NoError(t, err)
		assert.Equal(t, 2, result.CardsImported) // PROJ-43, PROJ-44
		assert.Equal(t, 1, result.Skipped)       // PROJ-45 (done)
	})

	t.Run("all keys selected imports all non-done", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey:      "PROJ-42",
			Name:         "all-selected",
			Prefix:       "ALS",
			SelectedKeys: []string{"PROJ-43", "PROJ-44", "PROJ-45"},
		})
		require.NoError(t, err)
		// PROJ-45 is done so still skipped even though it is in SelectedKeys.
		assert.Equal(t, 2, result.CardsImported)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("subset selected imports only matching non-done keys", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey:      "PROJ-42",
			Name:         "subset-selected",
			Prefix:       "SUB",
			SelectedKeys: []string{"PROJ-43"},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, result.CardsImported) // only PROJ-43
		assert.Equal(t, 2, result.Skipped)       // PROJ-44 (not selected), PROJ-45 (done)

		cards, err := imp.store.ListCards(context.Background(), "subset-selected", storage.CardFilter{})
		require.NoError(t, err)
		require.Len(t, cards, 1)
		assert.Equal(t, "PROJ-43 Task A", cards[0].Title)
	})

	t.Run("done key explicitly selected is still skipped", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		result, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey:      "PROJ-42",
			Name:         "done-selected",
			Prefix:       "DNS",
			SelectedKeys: []string{"PROJ-45"}, // only the done issue
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.CardsImported) // PROJ-45 is done, still skipped
		assert.Equal(t, 3, result.Skipped)       // PROJ-45 (done), PROJ-43 (not selected), PROJ-44 (not selected)

		cards, err := imp.store.ListCards(context.Background(), "done-selected", storage.CardFilter{})
		require.NoError(t, err)
		assert.Len(t, cards, 0)
	})
}

func TestContainsKey(t *testing.T) {
	assert.True(t, containsKey([]string{"PROJ-43", "PROJ-44"}, "PROJ-43"))
	assert.True(t, containsKey([]string{"PROJ-43", "PROJ-44"}, "PROJ-44"))
	assert.False(t, containsKey([]string{"PROJ-43", "PROJ-44"}, "PROJ-45"))
	assert.False(t, containsKey([]string{}, "PROJ-43"))
	assert.False(t, containsKey(nil, "PROJ-43"))
}

func TestPreviewEpic_AlreadyImported(t *testing.T) {
	// Jira mock: epic PROJ-42 with three children.
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
			Issues: []Issue{
				{Key: "PROJ-43", Fields: IssueFields{Summary: "Add OAuth", IssueType: NameField{Name: "Story"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-44", Fields: IssueFields{Summary: "Fix token refresh", IssueType: NameField{Name: "Bug"}, Status: NameField{Name: "To Do"}}},
				{Key: "PROJ-45", Fields: IssueFields{Summary: "Write docs", IssueType: NameField{Name: "Task"}, Status: NameField{Name: "To Do"}}},
			},
		})
	})

	t.Run("no prior import — all AlreadyImported false", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		preview, err := imp.PreviewEpic(context.Background(), "PROJ-42")
		require.NoError(t, err)
		require.Len(t, preview.Children, 3)
		for _, child := range preview.Children {
			assert.False(t, child.AlreadyImported, "expected AlreadyImported=false for %s", child.Key)
		}
	})

	t.Run("epic key not matching any project — all AlreadyImported false", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		// Import a different epic so a project exists, but with a different key.
		differentMux := http.NewServeMux()
		differentMux.HandleFunc("GET /rest/api/3/issue/PROJ-99", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(Issue{
				Key:    "PROJ-99",
				Fields: IssueFields{Summary: "Other epic", IssueType: NameField{Name: "Epic"}},
			})
		})
		differentMux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(searchResult{})
		})
		otherSrv := httptest.NewServer(differentMux)
		defer otherSrv.Close()
		otherClient := &Client{
			httpClient: otherSrv.Client(),
			baseURL:    otherSrv.URL,
			email:      "test@example.com",
			token:      "test-token",
		}
		otherImp := NewImporter(otherClient, imp.svc, imp.store, imp.jiraCfg)
		_, err := otherImp.ImportEpic(context.Background(), ImportEpicInput{EpicKey: "PROJ-99", Name: "other-project", Prefix: "OTH"})
		require.NoError(t, err)

		// Preview PROJ-42 — no project matches this epic key.
		preview, err := imp.PreviewEpic(context.Background(), "PROJ-42")
		require.NoError(t, err)
		require.Len(t, preview.Children, 3)
		for _, child := range preview.Children {
			assert.False(t, child.AlreadyImported, "expected AlreadyImported=false for %s", child.Key)
		}
	})

	t.Run("partial prior import — mixed AlreadyImported", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		// Import only PROJ-43 by selecting just that key.
		_, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey:      "PROJ-42",
			Name:         "auth-overhaul",
			Prefix:       "AUTH",
			SelectedKeys: []string{"PROJ-43"},
		})
		require.NoError(t, err)

		preview, err := imp.PreviewEpic(context.Background(), "PROJ-42")
		require.NoError(t, err)
		require.Len(t, preview.Children, 3)

		byKey := make(map[string]IssuePreview, 3)
		for _, c := range preview.Children {
			byKey[c.Key] = c
		}
		assert.True(t, byKey["PROJ-43"].AlreadyImported, "PROJ-43 was imported, expected AlreadyImported=true")
		assert.False(t, byKey["PROJ-44"].AlreadyImported, "PROJ-44 was not imported, expected AlreadyImported=false")
		assert.False(t, byKey["PROJ-45"].AlreadyImported, "PROJ-45 was not imported, expected AlreadyImported=false")
	})

	t.Run("full prior import — all AlreadyImported true", func(t *testing.T) {
		imp, _ := setupTestImporter(t, mux)

		// Import all three children.
		_, err := imp.ImportEpic(context.Background(), ImportEpicInput{
			EpicKey: "PROJ-42",
			Name:    "auth-overhaul",
			Prefix:  "AUTH",
		})
		require.NoError(t, err)

		preview, err := imp.PreviewEpic(context.Background(), "PROJ-42")
		require.NoError(t, err)
		require.Len(t, preview.Children, 3)
		for _, child := range preview.Children {
			assert.True(t, child.AlreadyImported, "expected AlreadyImported=true for %s (all were imported)", child.Key)
		}
	})
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
