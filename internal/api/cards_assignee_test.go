package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// newAssigneeNoneServer builds a none-mode router over a real card service.
// No AuthService means cardHandlers.users stays nil, so every actual assignee
// change must be rejected as multi-user-only.
func newAssigneeNoneServer(t *testing.T) (*httptest.Server, *service.CardService) {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	t.Cleanup(server.Close)

	return server, svc
}

// newAssigneeMultiServer builds a multi-mode router wired with BOTH an auth
// service and a real card service. newAuthTestServer deliberately leaves
// Service nil (and many tests depend on that shape), so the assignee roster
// tests get their own harness. Seeds admin "root" / "root password1".
func newAssigneeMultiServer(t *testing.T) (*httptest.Server, *service.CardService, *authstore.Store) {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	authSvc := auth.NewService(store, time.Hour)
	seedAuthUser(t, store, "root", "Root", true, "root password1")

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service:     svc,
		Bus:         bus,
		AuthService: authSvc,
		AuthMode:    "multi",
	}))
	t.Cleanup(server.Close)

	return server, svc, store
}

// seedAuthUser creates a user, optionally with a usable password so the test
// can log in as them.
func seedAuthUser(t *testing.T, store *authstore.Store, username, displayName string, isAdmin bool, password string) *authstore.User {
	t.Helper()

	u, err := store.CreateUser(t.Context(), username, displayName, isAdmin, timeNow())
	require.NoError(t, err)

	if password != "" {
		hash, err := auth.HashPassword(password)
		require.NoError(t, err)
		require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, timeNow()))
	}

	return u
}

// seedAssignedCard creates a card straight through the service layer. The
// service has no human-only gate, so this is the only way to obtain a card
// that already carries an assignee - including a non-canonical one that only
// a hand edit of the board repo could produce.
func seedAssignedCard(t *testing.T, svc *service.CardService, title, assignee string) *board.Card {
	t.Helper()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    title,
		Type:     "task",
		Priority: "medium",
		Assignee: assignee,
	})
	require.NoError(t, err)

	return card
}

// sendCardJSON issues a request with the given raw JSON body. Raw JSON keeps
// the wire contract (the `assignee` tag) under test.
func sendCardJSON(t *testing.T, method, url, body string, mutate func(*http.Request)) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	if mutate != nil {
		mutate(req)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func asAgent(id string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("X-Agent-ID", id) }
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func TestCardAssignee_NoneMode_AgentGated(t *testing.T) {
	server, svc := newAssigneeNoneServer(t)
	cardsURL := server.URL + "/api/projects/test-project/cards"

	t.Run("create with assignee", func(t *testing.T) {
		resp := sendCardJSON(t, http.MethodPost, cardsURL,
			`{"title":"Gated","type":"task","priority":"medium","assignee":"alice"}`,
			asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "assignee")
	})

	t.Run("patch assignee", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Patch gated", "")

		resp := sendCardJSON(t, http.MethodPatch, cardsURL+"/"+card.ID,
			`{"assignee":"alice"}`, asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "assignee")
	})

	t.Run("put setting assignee", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Put set gated", "")

		resp := sendCardJSON(t, http.MethodPut, cardsURL+"/"+card.ID,
			`{"title":"Put set gated","type":"task","state":"todo","priority":"medium","create_pr":true,"vetted":true,"assignee":"alice"}`,
			asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("put clearing assignee", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Put clear gated", "alice")

		resp := sendCardJSON(t, http.MethodPut, cardsURL+"/"+card.ID,
			`{"title":"Put clear gated","type":"task","state":"todo","priority":"medium","create_pr":true,"vetted":true}`,
			asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode,
			"PUT omitting assignee on an assigned card is a clear, which agents may not do")

		stored, err := svc.GetCard(t.Context(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", stored.Assignee)
	})
}

func TestCardAssignee_NoneMode_HumanRejectedAsMultiUserOnly(t *testing.T) {
	server, svc := newAssigneeNoneServer(t)
	card := seedAssignedCard(t, svc, "No roster here", "")

	resp := sendCardJSON(t, http.MethodPatch,
		server.URL+"/api/projects/test-project/cards/"+card.ID,
		`{"assignee":"alice"}`, asAgent("human:web"))
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	var apiErr APIError

	require.NoError(t, jsonDecode(resp, &apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Equal(t, "assignee requires multi-user mode", apiErr.Error)
}

// TestCardAssignee_NoneMode_UnchangedEchoRoundTrips locks the escape hatch
// that keeps every other field editable on a card whose assignee predates the
// current roster (or was hand-written into the board repo).
func TestCardAssignee_NoneMode_UnchangedEchoRoundTrips(t *testing.T) {
	server, svc := newAssigneeNoneServer(t)
	cardsURL := server.URL + "/api/projects/test-project/cards"

	t.Run("exact echo", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Echo", "alice")

		resp := sendCardJSON(t, http.MethodPut, cardsURL+"/"+card.ID,
			`{"title":"Echo renamed","type":"task","state":"todo","priority":"medium","create_pr":true,"vetted":true,"assignee":"alice"}`,
			asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got board.Card

		require.NoError(t, jsonDecode(resp, &got))
		assert.Equal(t, "alice", got.Assignee)
		assert.Equal(t, "Echo renamed", got.Title)
	})

	t.Run("case variant echo", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Case echo", "Alice")

		resp := sendCardJSON(t, http.MethodPut, cardsURL+"/"+card.ID,
			`{"title":"Case echo","type":"task","state":"todo","priority":"medium","create_pr":true,"vetted":true,"assignee":"alice"}`,
			asAgent("agent-1"))
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got board.Card

		require.NoError(t, jsonDecode(resp, &got))
		assert.Equal(t, "alice", got.Assignee, "the echo is stored in canonical form")
	})
}

func TestCardAssignee_MultiMode_RosterValidation(t *testing.T) {
	server, svc, store := newAssigneeMultiServer(t)
	seedAuthUser(t, store, "alice", "Alice", false, "")

	carol := seedAuthUser(t, store, "carol", "Carol", false, "")
	require.NoError(t, store.SetDisabled(t.Context(), carol.ID, true, timeNow()))

	cookie := login(t, server, "root", "root password1")
	cardsURL := server.URL + "/api/projects/test-project/cards"

	t.Run("patch unknown user", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Unknown patch", "")

		resp := sendCardJSON(t, http.MethodPatch, cardsURL+"/"+card.ID,
			`{"assignee":"ghost"}`, withCookie(cookie))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Equal(t, "unknown user: ghost", apiErr.Error)
	})

	t.Run("patch disabled user", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Disabled patch", "")

		resp := sendCardJSON(t, http.MethodPatch, cardsURL+"/"+card.ID,
			`{"assignee":"carol"}`, withCookie(cookie))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Equal(t, "user is disabled: carol", apiErr.Error)
	})

	t.Run("patch normalizes padded mixed case", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Padded patch", "")

		resp := sendCardJSON(t, http.MethodPatch, cardsURL+"/"+card.ID,
			`{"assignee":"  Alice  "}`, withCookie(cookie))
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got board.Card

		require.NoError(t, jsonDecode(resp, &got))
		assert.Equal(t, "alice", got.Assignee)

		stored, err := svc.GetCard(t.Context(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", stored.Assignee)
	})

	t.Run("patch clears assignee", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Clear patch", "alice")

		resp := sendCardJSON(t, http.MethodPatch, cardsURL+"/"+card.ID,
			`{"assignee":""}`, withCookie(cookie))
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got board.Card

		require.NoError(t, jsonDecode(resp, &got))
		assert.Empty(t, got.Assignee)
	})

	t.Run("create with unknown user", func(t *testing.T) {
		resp := sendCardJSON(t, http.MethodPost, cardsURL,
			`{"title":"Unknown create","type":"task","priority":"medium","assignee":"ghost"}`,
			withCookie(cookie))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, "unknown user: ghost", apiErr.Error)
	})

	t.Run("create with known user", func(t *testing.T) {
		resp := sendCardJSON(t, http.MethodPost, cardsURL,
			`{"title":"Known create","type":"task","priority":"medium","assignee":" Alice "}`,
			withCookie(cookie))
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var got board.Card

		require.NoError(t, jsonDecode(resp, &got))
		assert.Equal(t, "alice", got.Assignee)
	})

	t.Run("put unknown user", func(t *testing.T) {
		card := seedAssignedCard(t, svc, "Unknown put", "")

		resp := sendCardJSON(t, http.MethodPut, cardsURL+"/"+card.ID,
			`{"title":"Unknown put","type":"task","state":"todo","priority":"medium","create_pr":true,"vetted":true,"assignee":"ghost"}`,
			withCookie(cookie))
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError

		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, "unknown user: ghost", apiErr.Error)
	})
}

// TestCardAssignee_MultiMode_ClaimedByOtherAgent documents the accepted
// interaction: assignee is informational, but the card-ownership check still
// guards the whole mutation surface, so a human cannot re-assign a card that
// an agent currently holds.
func TestCardAssignee_MultiMode_ClaimedByOtherAgent(t *testing.T) {
	server, svc, store := newAssigneeMultiServer(t)
	seedAuthUser(t, store, "alice", "Alice", false, "")

	card := seedAssignedCard(t, svc, "Claimed elsewhere", "")
	_, err := svc.ClaimCard(t.Context(), "test-project", card.ID, "agent-worker")
	require.NoError(t, err)

	cookie := login(t, server, "root", "root password1")

	resp := sendCardJSON(t, http.MethodPatch,
		server.URL+"/api/projects/test-project/cards/"+card.ID,
		`{"assignee":"alice"}`, withCookie(cookie))
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var apiErr APIError

	require.NoError(t, jsonDecode(resp, &apiErr))
	assert.Equal(t, ErrCodeAgentMismatch, apiErr.Code)
}

// TestCardAssignee_MultiMode_PatchAttribution covers the PATCH attribution
// change: the session identity now reaches the service layer, so activity
// entries name the acting human instead of falling back to "system".
func TestCardAssignee_MultiMode_PatchAttribution(t *testing.T) {
	server, svc, store := newAssigneeMultiServer(t)
	seedAuthUser(t, store, "bob", "Bob", false, "")

	card := seedAssignedCard(t, svc, "Attributed", "")
	cookie := login(t, server, "root", "root password1")

	resp := sendCardJSON(t, http.MethodPatch,
		server.URL+"/api/projects/test-project/cards/"+card.ID,
		`{"assignee":"bob"}`, withCookie(cookie))
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	stored, err := svc.GetCard(t.Context(), "test-project", card.ID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.ActivityLog)

	last := stored.ActivityLog[len(stored.ActivityLog)-1]
	assert.Equal(t, "assigned", last.Action)
	assert.Equal(t, "Assigned to bob", last.Message)
	assert.Equal(t, "human:root", last.Agent)
}
