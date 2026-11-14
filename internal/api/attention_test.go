package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/attention"
	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// attentionCards serves one shared project with one parked card, or no project when empty.
type attentionCards struct {
	err   error
	empty bool
}

func (c attentionCards) ListProjects(context.Context) ([]board.ProjectConfig, error) {
	if c.empty {
		return nil, c.err
	}

	return []board.ProjectConfig{{Name: "p"}}, c.err
}

func (attentionCards) ListCards(_ context.Context, _ string, f storage.CardFilter) ([]*board.Card, error) {
	if f.WorkerStatus != "parked" {
		return nil, nil
	}

	return []*board.Card{{ID: "P-1", Project: "p", Title: "parked", State: "in_progress", WorkerStatus: "parked"}}, nil
}

func (attentionCards) GetCard(context.Context, string, string) (*board.Card, error) {
	return nil, storage.ErrCardNotFound
}

// newAttentionServer serves GET /api/attention over a real chat store holding
// a waiting blueprint for alice and one for bob; multi wires the session guard
// with both users.
func newAttentionServer(t *testing.T, multi bool, cards attentionCards) *httptest.Server {
	t.Helper()

	ops, err := sqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ops.Close() })

	now := time.Now().UTC().Truncate(time.Second)

	for id, owner := range map[string]string{"bp-alice": "human:alice", "bp-bob": "human:bob"} {
		require.NoError(t, ops.CreateSession(t.Context(), chat.Session{
			ID: id, Title: id, Kind: chat.KindArchitect, Status: chat.StatusActive,
			CreatedBy: owner, CreatedAt: now, LastActive: now,
		}))
		_, err := ops.UpdateSessionWaiting(t.Context(), id, "gate", now)
		require.NoError(t, err)
	}

	attn := attention.New(events.NewBus())
	attn.Start(t.Context(), attention.Sources{Blueprints: ops, Cards: cards})

	cfg := RouterConfig{Attention: attn}

	if multi {
		users, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = users.Close() })

		for _, name := range []string{"alice", "bob"} {
			u, err := users.CreateUser(t.Context(), name, name, false, now)
			require.NoError(t, err)

			hash, err := auth.HashPassword(name + " password1")
			require.NoError(t, err)
			require.NoError(t, users.SetPasswordHash(t.Context(), u.ID, hash, now))
		}

		cfg.AuthService = auth.NewService(users, time.Hour)
		cfg.AuthMode = "multi"
	}

	server := httptest.NewServer(newTestRouter(cfg))
	t.Cleanup(server.Close)

	return server
}

func getAttention(t *testing.T, server *httptest.Server, agentID string, cookie *http.Cookie) (int, []attention.Item) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/attention", nil)
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")

	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}

	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	var items []attention.Item
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&items))
	}

	return resp.StatusCode, items
}

// mineByID maps each item id to its mine flag.
func mineByID(items []attention.Item) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[it.ID] = it.Mine
	}

	return out
}

func TestAttention_MultiModeListsOnlyOwnBlueprints(t *testing.T) {
	server := newAttentionServer(t, true, attentionCards{})

	status, items := getAttention(t, server, "human:bob", login(t, server, "alice", "alice password1"))
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, map[string]bool{"bp-alice": true, "P-1": false}, mineByID(items))

	status, items = getAttention(t, server, "", login(t, server, "bob", "bob password1"))
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, map[string]bool{"bp-bob": true, "P-1": false}, mineByID(items))
}

func TestAttention_NoneModeListsAll(t *testing.T) {
	server := newAttentionServer(t, false, attentionCards{})

	status, items := getAttention(t, server, "human:alice", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, map[string]bool{"bp-alice": true, "bp-bob": false, "P-1": false}, mineByID(items))

	status, items = getAttention(t, server, "human:bob", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, map[string]bool{"bp-alice": false, "bp-bob": true, "P-1": false}, mineByID(items))
}

func TestAttention_RequiresSession(t *testing.T) {
	server := newAttentionServer(t, true, attentionCards{})

	status, _ := getAttention(t, server, "human:alice", nil)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestAttention_SourceFailureIs500(t *testing.T) {
	server := newAttentionServer(t, false, attentionCards{err: errors.New("disk gone")})

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/attention", nil)
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	var body APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, ErrCodeInternalError, body.Code)
}

func TestAttention_EmptyListIsAnArray(t *testing.T) {
	attn := attention.New(events.NewBus())
	attn.Start(t.Context(), attention.Sources{Cards: attentionCards{empty: true}})

	req := httptest.NewRequest(http.MethodGet, "/api/attention", nil)
	req.Header.Set("X-Requested-With", "contextmatrix")

	w := httptest.NewRecorder()
	newTestRouter(RouterConfig{Attention: attn}).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, "[]", w.Body.String())
}
