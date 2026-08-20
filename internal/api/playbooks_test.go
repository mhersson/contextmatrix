package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// playbookTestSetup mirrors testSetup but also builds a playbook service
// over the same boards dir, so card refs resolve against real cards.
func playbookTestSetup(t *testing.T) (*service.CardService, *service.PlaybookService, *events.Bus, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	boardConfig := `name: test-project
prefix: TEST
next_id: 1
states: [todo, in_progress, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	require.NoError(t, git.CommitFile(context.Background(), "test-project/.board.yaml", "init: seed boards repo"))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()

	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	pbStore, err := storage.NewFilesystemPlaybookStore(boardsDir)
	require.NoError(t, err)

	pbSvc := service.NewPlaybookService(pbStore, store, bus, nil, false) // gitAutoCommit=false: API tests skip git

	cleanup := func() {
		// Temp directory is automatically cleaned up by t.TempDir()
	}

	return svc, pbSvc, bus, cleanup
}

// doJSON issues method with an optional JSON body, the CSRF header, and an
// optional X-Agent-ID header, returning the raw response.
func doJSON(t *testing.T, method, url string, body any, agentID string) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		reader = jsonBody(t, body)
	}

	var req *http.Request

	var err error
	if reader != nil {
		req, err = http.NewRequest(method, url, reader)
	} else {
		req, err = http.NewRequest(method, url, http.NoBody)
	}

	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func TestPlaybooksAPI_CRUD(t *testing.T) {
	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc})

	server := httptest.NewServer(router)
	defer server.Close()

	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Seed Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// POST /api/playbooks {title, entries:[{type:card,...},{type:manual,...}]}
	// with X-Agent-ID: human:alice -> 201; id == "rollout"; created_by == "human:alice"
	createReq := map[string]any{
		"title": "Rollout",
		"entries": []map[string]any{
			{"type": "card", "project": "test-project", "card": "TEST-001"},
			{"type": "manual", "text": "deploy"},
		},
	}

	createResp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks", createReq, "human:alice")
	defer closeBody(t, createResp.Body)

	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var created service.PlaybookDetail
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	assert.Equal(t, "rollout", created.ID)
	assert.Equal(t, "human:alice", created.CreatedBy)
	require.Len(t, created.Entries, 2)

	// GET /api/playbooks -> 200, one summary {id, title, complete:0, total:2}
	listResp := doGet(t, server.URL+"/api/playbooks")
	defer closeBody(t, listResp.Body)

	assert.Equal(t, http.StatusOK, listResp.StatusCode)

	var summaries []service.PlaybookSummary
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&summaries))
	require.Len(t, summaries, 1)
	assert.Equal(t, "rollout", summaries[0].ID)
	assert.Equal(t, "Rollout", summaries[0].Title)
	assert.Equal(t, 0, summaries[0].Complete)
	assert.Equal(t, 2, summaries[0].Total)

	// GET /api/playbooks/rollout -> 200, entries[0].card_state == "todo"
	getResp := doGet(t, server.URL+"/api/playbooks/rollout")
	defer closeBody(t, getResp.Body)

	assert.Equal(t, http.StatusOK, getResp.StatusCode)

	var got service.PlaybookDetail
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&got))
	require.Len(t, got.Entries, 2)
	assert.Equal(t, "todo", got.Entries[0].CardState)

	// PATCH /api/playbooks/rollout {"description":"d"} -> 200
	patchResp := doJSON(t, http.MethodPatch, server.URL+"/api/playbooks/rollout", map[string]any{"description": "d"}, "human:alice")
	defer closeBody(t, patchResp.Body)

	assert.Equal(t, http.StatusOK, patchResp.StatusCode)

	var patched service.PlaybookDetail
	require.NoError(t, json.NewDecoder(patchResp.Body).Decode(&patched))
	assert.Equal(t, "d", patched.Description)

	// POST /api/playbooks/rollout/entries {type:manual,text:"verify"} -> 201, 3 entries
	addResp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/entries",
		map[string]any{"type": "manual", "text": "verify"}, "human:alice")
	defer closeBody(t, addResp.Body)

	require.Equal(t, http.StatusCreated, addResp.StatusCode)

	var added service.PlaybookDetail
	require.NoError(t, json.NewDecoder(addResp.Body).Decode(&added))
	require.Len(t, added.Entries, 3)
	assert.Equal(t, "e3", added.Entries[2].ID)

	// PATCH /api/playbooks/rollout/entries/e3 {"done":true} -> 200, done_by == "human:alice"
	doneResp := doJSON(t, http.MethodPatch, server.URL+"/api/playbooks/rollout/entries/e3",
		map[string]any{"done": true}, "human:alice")
	defer closeBody(t, doneResp.Body)

	assert.Equal(t, http.StatusOK, doneResp.StatusCode)

	var doneDetail service.PlaybookDetail
	require.NoError(t, json.NewDecoder(doneResp.Body).Decode(&doneDetail))
	require.Len(t, doneDetail.Entries, 3)
	assert.Equal(t, "human:alice", doneDetail.Entries[2].DoneBy)

	// DELETE /api/playbooks/rollout/entries/e3 -> 200, 2 entries
	delEntryResp := doJSON(t, http.MethodDelete, server.URL+"/api/playbooks/rollout/entries/e3", nil, "human:alice")
	defer closeBody(t, delEntryResp.Body)

	assert.Equal(t, http.StatusOK, delEntryResp.StatusCode)

	var afterDelEntry service.PlaybookDetail
	require.NoError(t, json.NewDecoder(delEntryResp.Body).Decode(&afterDelEntry))
	assert.Len(t, afterDelEntry.Entries, 2)

	// DELETE /api/playbooks/rollout -> 204; GET -> 404 code PLAYBOOK_NOT_FOUND
	delResp := doJSON(t, http.MethodDelete, server.URL+"/api/playbooks/rollout", nil, "human:alice")
	defer closeBody(t, delResp.Body)

	assert.Equal(t, http.StatusNoContent, delResp.StatusCode)

	finalGet := doGet(t, server.URL+"/api/playbooks/rollout")
	defer closeBody(t, finalGet.Body)

	assert.Equal(t, http.StatusNotFound, finalGet.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(finalGet.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodePlaybookNotFound, apiErr.Code)
}

func TestPlaybooksAPI_ErrorContract(t *testing.T) {
	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc})

	server := httptest.NewServer(router)
	defer server.Close()

	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Seed Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("get unknown playbook - 404 PLAYBOOK_NOT_FOUND", func(t *testing.T) {
		resp := doGet(t, server.URL+"/api/playbooks/nope")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookNotFound, apiErr.Code)
	})

	t.Run("create with duplicate card entries - 409 PLAYBOOK_ENTRY_EXISTS", func(t *testing.T) {
		body := map[string]any{
			"title": "Dup",
			"entries": []map[string]any{
				{"type": "card", "project": "test-project", "card": "TEST-001"},
				{"type": "card", "project": "test-project", "card": "TEST-001"},
			},
		}

		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks", body, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookEntryExists, apiErr.Code)
	})

	t.Run("create with empty title - 400 BAD_REQUEST", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks", map[string]any{"title": ""}, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
	})

	// Seed a playbook with a card entry and a manual entry to exercise the
	// entry-patch validation paths below.
	seedResp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks", map[string]any{
		"title": "Errors",
		"entries": []map[string]any{
			{"type": "card", "project": "test-project", "card": "TEST-001"},
			{"type": "manual", "text": "step"},
		},
	}, "human:alice")
	defer closeBody(t, seedResp.Body)

	require.Equal(t, http.StatusCreated, seedResp.StatusCode)

	var seeded service.PlaybookDetail
	require.NoError(t, json.NewDecoder(seedResp.Body).Decode(&seeded))
	require.Len(t, seeded.Entries, 2)
	cardEntryID := seeded.Entries[0].ID

	t.Run("patch done on a card entry - 422 VALIDATION_ERROR", func(t *testing.T) {
		resp := doJSON(t, http.MethodPatch, server.URL+"/api/playbooks/"+seeded.ID+"/entries/"+cardEntryID,
			map[string]any{"done": true}, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})

	t.Run("patch entry position=-1 - 422 VALIDATION_ERROR", func(t *testing.T) {
		resp := doJSON(t, http.MethodPatch, server.URL+"/api/playbooks/"+seeded.ID+"/entries/"+cardEntryID,
			map[string]any{"position": -1}, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})

	t.Run("patch unknown entry - 404 PLAYBOOK_ENTRY_NOT_FOUND", func(t *testing.T) {
		resp := doJSON(t, http.MethodPatch, server.URL+"/api/playbooks/"+seeded.ID+"/entries/e999",
			map[string]any{"note": "x"}, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookEntryNotFound, apiErr.Code)
	})

	t.Run("post without X-Requested-With - 403", func(t *testing.T) {
		// rawHTTPClient (csrf_test.go) bypasses the package's test-only
		// transport that auto-injects the CSRF header on every request -
		// this test needs to see what a real cross-origin request looks
		// like without it.
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/playbooks", jsonBody(t, map[string]any{"title": "NoCSRF"}))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := rawHTTPClient().Do(req)
		require.NoError(t, err)

		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestPlaybooksAPI_AttributionFallback(t *testing.T) {
	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc})

	server := httptest.NewServer(router)
	defer server.Close()

	// POST create with NO X-Agent-ID header -> created_by == "human:web"
	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks", map[string]any{"title": "No Header"}, "")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var detail service.PlaybookDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	assert.Equal(t, "human:web", detail.CreatedBy)
}

func TestPlaybooksAPI_SessionGuard(t *testing.T) {
	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	authSvc := auth.NewService(store, time.Hour)

	root, err := store.CreateUser(t.Context(), "root", "Root", true, time.Now())
	require.NoError(t, err)

	hash, err := auth.HashPassword("root password1")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), root.ID, hash, time.Now()))

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc, AuthService: authSvc, AuthMode: "multi"})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// GET /api/playbooks without a session cookie -> 401.
	t.Run("no session - 401", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/playbooks", http.NoBody)
		require.NoError(t, err)
		req.Header.Set("X-Requested-With", "contextmatrix")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	cookie := login(t, server, "root", "root password1")

	// With a session cookie -> 200, and created_by on a create is
	// "human:root" even when X-Agent-ID says otherwise.
	t.Run("with session - 200 and session identity wins over header", func(t *testing.T) {
		getReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/playbooks", http.NoBody)
		require.NoError(t, err)
		getReq.Header.Set("X-Requested-With", "contextmatrix")
		getReq.AddCookie(cookie)

		getResp, err := http.DefaultClient.Do(getReq)
		require.NoError(t, err)

		defer closeBody(t, getResp.Body)

		assert.Equal(t, http.StatusOK, getResp.StatusCode)

		createReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/playbooks",
			jsonBody(t, map[string]any{"title": "Session Attribution"}))
		require.NoError(t, err)
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-Requested-With", "contextmatrix")
		createReq.Header.Set("X-Agent-ID", "claude-spoof")
		createReq.AddCookie(cookie)

		createResp, err := http.DefaultClient.Do(createReq)
		require.NoError(t, err)

		defer closeBody(t, createResp.Body)

		require.Equal(t, http.StatusCreated, createResp.StatusCode)

		var detail service.PlaybookDetail
		require.NoError(t, json.NewDecoder(createResp.Body).Decode(&detail))
		assert.Equal(t, "human:root", detail.CreatedBy)
	})
}

func TestPlaybooksAPI_NilServiceIs404(t *testing.T) {
	svc, _, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp := doGet(t, server.URL+"/api/playbooks")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
