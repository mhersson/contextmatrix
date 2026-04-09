package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// setupWriteBackTest creates a service stack, bus, and mock Jira server for write-back tests.
func setupWriteBackTest(t *testing.T) (*WriteBackHandler, *events.Bus, *service.CardService, *storage.FilesystemStore, *commentCapture) {
	t.Helper()

	capture := &commentCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			capture.add(r.URL.Path, body.Body)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	boardsDir := t.TempDir()
	git, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*60)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	client := &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		email:      "test@example.com",
		token:      "test-token",
	}

	handler := NewWriteBackHandler(client, store, bus)

	return handler, bus, svc, store, capture
}

// commentCapture collects Jira comments posted during tests.
type commentCapture struct {
	mu       sync.Mutex
	comments []capturedComment
}

type capturedComment struct {
	Path string
	Body string
}

func (c *commentCapture) add(path, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.comments = append(c.comments, capturedComment{Path: path, Body: body})
}

func (c *commentCapture) get() []capturedComment {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedComment{}, c.comments...)
}

func TestWriteBack_PostsCommentOnCardDone(t *testing.T) {
	handler, _, svc, _, capture := setupWriteBackTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler.Start(ctx)

	// Create a project with Jira config.
	_, err := svc.CreateProject(ctx, service.CreateProjectInput{
		Name:        "test-project",
		Prefix:      "TP",
		States:      defaultStates(),
		Types:       defaultTypes(),
		Priorities:  defaultPriorities(),
		Transitions: defaultTransitions(),
		Jira:        &board.JiraEpicConfig{EpicKey: "PROJ-42", ProjectKey: "PROJ"},
	})
	require.NoError(t, err)

	// Create a card with Jira source.
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title:    "Test task",
		Type:     "task",
		Priority: "medium",
		Vetted:   true,
		Source:   &board.Source{System: "jira", ExternalID: "PROJ-43", ExternalURL: "https://jira.example.com/browse/PROJ-43"},
	})
	require.NoError(t, err)

	// Transition to done (walks todo → in_progress → review → done).
	_, err = svc.TransitionTo(ctx, "test-project", card.ID, "done")
	require.NoError(t, err)

	// Wait for the event to be processed.
	time.Sleep(100 * time.Millisecond)

	comments := filterJiraComments(capture.get())
	// Single Jira card done → card comment + epic comment (all 1/1 done).
	require.Len(t, comments, 2)
	// First comment: card completion.
	assert.Contains(t, comments[0].Path, "/rest/api/3/issue/PROJ-43/comment")
	assert.Contains(t, comments[0].Body, "Task completed")
	assert.Contains(t, comments[0].Body, "Test task")
	// Second comment: epic completion.
	assert.Contains(t, comments[1].Path, "/rest/api/3/issue/PROJ-42/comment")
	assert.Contains(t, comments[1].Body, "All tasks completed")
}

func TestWriteBack_IgnoresNonJiraCards(t *testing.T) {
	handler, _, svc, _, capture := setupWriteBackTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler.Start(ctx)

	// Create a project.
	_, err := svc.CreateProject(ctx, service.CreateProjectInput{
		Name:        "test-project",
		Prefix:      "TP",
		States:      defaultStates(),
		Types:       defaultTypes(),
		Priorities:  defaultPriorities(),
		Transitions: defaultTransitions(),
	})
	require.NoError(t, err)

	// Create a card WITHOUT Jira source.
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title:    "Regular task",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Transition to done (walks the state machine path).
	_, err = svc.TransitionTo(ctx, "test-project", card.ID, "done")
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// No Jira comment should be posted.
	assert.Empty(t, capture.get())
}

func TestWriteBack_PostsEpicCommentWhenAllDone(t *testing.T) {
	handler, _, svc, _, capture := setupWriteBackTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler.Start(ctx)

	// Create a project with Jira config.
	_, err := svc.CreateProject(ctx, service.CreateProjectInput{
		Name:        "test-project",
		Prefix:      "TP",
		States:      defaultStates(),
		Types:       defaultTypes(),
		Priorities:  defaultPriorities(),
		Transitions: defaultTransitions(),
		Jira:        &board.JiraEpicConfig{EpicKey: "PROJ-42", ProjectKey: "PROJ"},
	})
	require.NoError(t, err)

	// Create two Jira cards.
	card1, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task one", Type: "task", Priority: "medium", Vetted: true,
		Source: &board.Source{System: "jira", ExternalID: "PROJ-43"},
	})
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task two", Type: "task", Priority: "medium", Vetted: true,
		Source: &board.Source{System: "jira", ExternalID: "PROJ-44"},
	})
	require.NoError(t, err)

	// Complete first card — should not trigger epic comment yet.
	_, err = svc.TransitionTo(ctx, "test-project", card1.ID, "done")
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	comments1 := capture.get()
	// Multiple events from intermediate transitions; only the done one triggers a Jira comment.
	// Should have exactly 1 Jira comment (for card1 done).
	jiraComments1 := filterJiraComments(comments1)
	require.Len(t, jiraComments1, 1)

	// Complete second card — should trigger both card and epic comments.
	_, err = svc.TransitionTo(ctx, "test-project", card2.ID, "done")
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	comments2 := capture.get()
	jiraComments2 := filterJiraComments(comments2)
	// card1 done + card2 done + epic completion = 3
	require.Len(t, jiraComments2, 3)
	assert.Contains(t, jiraComments2[2].Path, "/rest/api/3/issue/PROJ-42/comment")
	assert.Contains(t, jiraComments2[2].Body, "All tasks completed")
}

func TestWriteBack_IgnoresNonDoneTransitions(t *testing.T) {
	handler, bus, _, _, capture := setupWriteBackTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler.Start(ctx)

	// Publish a non-done state change.
	bus.Publish(events.Event{
		Type:    events.CardStateChanged,
		Project: "test-project",
		CardID:  "TP-001",
		Data:    map[string]any{"old_state": "todo", "new_state": "in_progress"},
	})

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, capture.get())
}

func TestFormatCardComment(t *testing.T) {
	card := &board.Card{ID: "TP-001", Title: "Fix the login page"}
	comment := formatCardComment(card)
	assert.Contains(t, comment, "TP-001")
	assert.Contains(t, comment, "Fix the login page")
	assert.Contains(t, comment, "Task completed")
}

// filterJiraComments returns only comments posted to Jira issue comment endpoints.
func filterJiraComments(comments []capturedComment) []capturedComment {
	var filtered []capturedComment
	for _, c := range comments {
		if strings.Contains(c.Path, "/rest/api/3/issue/") {
			filtered = append(filtered, c)
		}
	}
	return filtered
}
