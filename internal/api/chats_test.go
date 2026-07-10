package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

type chatStubRunner struct {
	sendErr    error
	sendErrSeq []error
	mu         sync.Mutex
	sendCalls  int64
}

func (r *chatStubRunner) StartChat(_ context.Context, opts chat.StartChatOpts) (string, error) {
	return "container-" + opts.SessionID, nil
}

func (r *chatStubRunner) EndChat(_ context.Context, _ string) error { return nil }
func (r *chatStubRunner) SendChatMessage(_ context.Context, _, _, _ string) error {
	r.mu.Lock()
	idx := r.sendCalls
	r.sendCalls++
	seq := r.sendErrSeq
	r.mu.Unlock()

	if idx >= 0 && int(idx) < len(seq) {
		return seq[idx]
	}

	return r.sendErr
}

func (r *chatStubRunner) StreamLogs(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
	<-ctx.Done()

	return ctx.Err()
}

type fixtureOpts struct {
	// chatBackendCfg is the dedicated "chat" backend entry. Zero value (the
	// default) means no chat backend is configured → no-backend fallback.
	// Set an enabled+keyed entry with a DefaultModel to exercise OpenRouter
	// mode.
	chatBackendCfg config.BackendConfig
	// endpointModels, when non-nil, is the raw (uncached) endpoint model
	// fetcher. The fixture wraps it with a TTL cache before wiring into
	// the handler, matching the production path in NewRouter.
	endpointModels func(ctx context.Context) ([]chatModelEntry, error)
	// servedModels, when non-nil, is wired directly into chatHandlers.servedModels
	// (no caching wrapper — the catalog builder handles its own caching).
	servedModels func(ctx context.Context) []chatModelEntry
	// validateModel, when non-nil, is wired directly into chatHandlers.validateModel
	// (no caching wrapper — the catalog builder handles its own caching).
	validateModel func(ctx context.Context, slug string) bool
}

// defaultFixtureOpts is the no-backend topology: no chat backend configured
// and no endpoint picker wired.
func defaultFixtureOpts() fixtureOpts {
	return fixtureOpts{}
}

func jsonReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "human:web-x")
	req.Header.Set("Content-Type", "application/json")

	return req
}

func newChatFixture(t *testing.T, opts fixtureOpts) (*http.ServeMux, *chat.Manager) {
	t.Helper()
	mux, mgr, _ := newChatFixtureWithRunner(t, opts)

	return mux, mgr
}

func newChatFixtureWithRunner(t *testing.T, opts fixtureOpts) (*http.ServeMux, *chat.Manager, *chatStubRunner) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &chatStubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Backend: runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		// Mirrors production wiring: the manager's cold-open fallback is
		// the chat backend entry's default_model (empty when no backend).
		DefaultModel: opts.chatBackendCfg.DefaultModel,
	})
	hub := chat.NewSSEHub(64)
	mux := http.NewServeMux()

	chh := newChatHandlers(mgr, hub, opts.chatBackendCfg)
	if opts.endpointModels != nil {
		chh.endpointModels = newCachedEndpointFetcher(opts.endpointModels, endpointModelCacheTTL)
	}

	if opts.servedModels != nil {
		chh.servedModels = opts.servedModels
	}

	if opts.validateModel != nil {
		chh.validateModel = opts.validateModel
	}

	mux.HandleFunc("GET /api/chats/models", chh.listModels)
	mux.HandleFunc("GET /api/chats", chh.listChats)
	mux.HandleFunc("POST /api/chats", chh.createChat)
	mux.HandleFunc("GET /api/chats/{id}", chh.getChat)
	mux.HandleFunc("DELETE /api/chats/{id}", chh.deleteChat)
	mux.HandleFunc("PATCH /api/chats/{id}", chh.patchChat)
	mux.HandleFunc("POST /api/chats/{id}/open", chh.openChat)
	mux.HandleFunc("POST /api/chats/{id}/end", chh.endChat)
	mux.HandleFunc("POST /api/chats/{id}/clear", chh.clearChat)
	mux.HandleFunc("POST /api/chats/{id}/messages", chh.sendMessage)
	mux.HandleFunc("GET /api/chats/{id}/messages", chh.listMessages)
	mux.HandleFunc("GET /api/chats/{id}/stream", chh.streamChat)

	return mux, mgr, runner
}

func TestCreateChat_Success(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	req := httptest.NewRequest(http.MethodPost, "/api/chats",
		bytes.NewBufferString(`{"title":"t","project":"alpha"}`))
	req.Header.Set("X-Agent-ID", "human:web-x")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var sess chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&sess))
	assert.Equal(t, "t", sess.Title)
	assert.Equal(t, "alpha", sess.Project)
	assert.Equal(t, chat.StatusCold, sess.Status)
}

func TestGetChat_NotFound(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	req := httptest.NewRequest(http.MethodGet, "/api/chats/missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListChats_EmptyReturnsArray(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]\n", w.Body.String())
}

func TestSendMessage_OpensColdSession(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/messages",
		bytes.NewBufferString(`{"content":"hello"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestDeleteChat_Success(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "to-del", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/chats/"+sess.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestEndChat_ReturnsColdSession verifies that POST /api/chats/{id}/end
// returns 200 with the fresh (cold) session body. The frontend depends on
// this body to update its local state without an extra getChat call; an
// empty 2xx response would also have made the client's response.json()
// call throw and surface as "Failed to end session" in the UI.
func TestEndChat_ReturnsColdSession(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "to-end", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Drive the session active so EndSession has work to do.
	_, err = mgr.OpenSession(context.Background(), sess.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/end", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var got chat.Session

	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, chat.StatusCold, got.Status)
	assert.Empty(t, got.ContainerID, "ended session must not carry a container_id")
}

func TestPatchChat_UpdatesTitle(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "old", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPatch, "/api/chats/"+sess.ID,
		bytes.NewBufferString(`{"title":"new"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var got chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "new", got.Title)
}

func TestListChats_InvalidStatusBadRequest(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	req := httptest.NewRequest(http.MethodGet, "/api/chats?status=bogus", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestStreamChat_ConnectedBeforeAnyEvent verifies that the SSE handler
// flushes a ": connected\n\n" comment immediately on subscribe so browsers
// (and proxies that buffer until first body byte) see onopen fire even
// when no events are pending. Without this, the chat status dot stays grey
// and the UI thinks the stream is disconnected.
func TestStreamChat_ConnectedBeforeAnyEvent(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := t.Context()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/chats/"+sess.ID+"/stream", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	done := make(chan string, 1)

	go func() {
		reader := bufio.NewReader(resp.Body)

		line, err := reader.ReadString('\n')
		if err != nil {
			done <- ""

			return
		}

		done <- line
	}()

	select {
	case line := <-done:
		assert.True(t, strings.HasPrefix(line, ": connected"),
			"expected first SSE line to be `: connected`, got %q", line)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive `: connected` within 500ms — handler is not flushing before blocking")
	}
}

// TestStreamChat_UnknownSession_404 verifies that GET .../stream against a
// session that does not exist returns 404 without creating a hub entry.
// Without the existence check, subscribing would lazily create a per-session
// ring buffer and any GET against an unknown id would grow perSess
// permanently, so the handler validates the session exists before subscribing.
func TestStreamChat_UnknownSession_404(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())

	// Bounded deadline so the test fails fast if the handler subscribes and
	// blocks instead of returning 404 immediately.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/chats/never-existed/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"streamChat must 404 on unknown session, not silently create a hub entry")
}

func TestListMessages_EmptyEnvelope(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/chats/"+sess.ID+"/messages", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Messages []chat.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.NotNil(t, body.Messages, "messages must be [] not null")
	assert.Empty(t, body.Messages)
}

func TestListMessages_FiltersSinceSeqExclusively(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx,
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	for i := range 5 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText,
			`{"text":"m`+strconv.Itoa(i)+`"}`)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chats/"+sess.ID+"/messages?since_seq=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Messages []chat.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Len(t, body.Messages, 3)
	assert.Equal(t, int64(3), body.Messages[0].Seq)
	assert.Equal(t, int64(5), body.Messages[2].Seq)
}

func TestListMessages_RespectsLimit(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx,
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	for i := range 5 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText,
			`{"text":"m`+strconv.Itoa(i)+`"}`)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chats/"+sess.ID+"/messages?limit=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Messages []chat.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Len(t, body.Messages, 3)
	assert.Equal(t, int64(1), body.Messages[0].Seq, "oldest-first ordering")
	assert.Equal(t, int64(3), body.Messages[2].Seq)
}

func TestListMessages_ClampsLimitToMax(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx,
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	for range 1001 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, `{"text":"m"}`)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chats/"+sess.ID+"/messages?limit=99999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Messages []chat.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Len(t, body.Messages, 1000, "limit must be clamped to maxLimit")
}

func TestListMessages_UnknownSessionReturns404(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())

	req := httptest.NewRequest(http.MethodGet, "/api/chats/no-such/messages", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSendMessage_TooLong(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	long := make([]byte, 8193)
	for i := range long {
		long[i] = 'x'
	}

	body, _ := json.Marshal(map[string]string{"content": string(long)})
	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/messages",
		bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestCreateChat_Model_RoundTrip(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	body := `{"title":"x","project":"p","model":"claude-sonnet-4-6"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))
	require.Equal(t, 201, rec.Code)

	var sess chat.Session
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sess))
	require.Equal(t, "claude-sonnet-4-6", sess.Model)
}

// TestCreateChat_NoBackend_AcceptsAnyModel verifies that with no chat backend
// configured there is no catalog to validate against: any model slug is
// accepted at create time and stored verbatim. Sends fail at open time via
// the disabled-backend stub instead.
func TestCreateChat_NoBackend_AcceptsAnyModel(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	body := `{"title":"x","project":"p","model":"gpt-5"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))
	require.Equal(t, 201, rec.Code, "body=%s", rec.Body.String())

	var sess chat.Session
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sess))
	require.Equal(t, "gpt-5", sess.Model)
}

// openRouterFixtureOpts builds fixtureOpts with the dedicated chat backend
// enabled + keyed and a default OpenRouter slug — i.e. the contextmatrix-chat
// (OpenRouter) chat-server topology, which puts listModels/createChat in
// openRouter mode.
func openRouterFixtureOpts() fixtureOpts {
	return fixtureOpts{
		chatBackendCfg: config.BackendConfig{
			Name:         config.BackendNameChat,
			APIKey:       "chat-backend-hmac-key-0123456789ab",
			DefaultModel: "anthropic/claude-sonnet-4",
		},
	}
}

// TestListModels_NoBackendFallsBackToEndpointSource verifies the no-backend
// fallback: with no chat backend configured (zero-value BackendConfig) and no
// endpoint picker wired, listModels serves an empty endpoint-source response
// so the picker renders nothing.
func TestListModels_NoBackendFallsBackToEndpointSource(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chats/models", nil))
	require.Equal(t, 200, rec.Code)

	var body struct {
		Source  string            `json:"source"`
		Models  []json.RawMessage `json:"models"`
		Default string            `json:"default"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "endpoint", body.Source)
	require.Empty(t, body.Default)
	require.NotNil(t, body.Models, "models must be [] not null")
	require.Empty(t, body.Models)
}

func TestListModels_OpenRouterSource(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, openRouterFixtureOpts())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chats/models", nil))
	require.Equal(t, 200, rec.Code)

	var body struct {
		Source  string            `json:"source"`
		Models  []json.RawMessage `json:"models"`
		Default string            `json:"default"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "openrouter", body.Source)
	require.Empty(t, body.Models, "openrouter mode returns an empty list when no catalog builder is wired")
	require.Equal(t, "anthropic/claude-sonnet-4", body.Default)
}

func TestListModels_OpenRouter_ServesScreenedCatalog(t *testing.T) {
	t.Parallel()

	opts := openRouterFixtureOpts()
	opts.servedModels = func(_ context.Context) []chatModelEntry {
		return []chatModelEntry{{ID: "anthropic/claude-sonnet-4.5", Label: "anthropic/claude-sonnet-4.5", MaxTokens: 200000}}
	}
	mux, _ := newChatFixture(t, opts)

	req := httptest.NewRequest(http.MethodGet, "/api/chats/models", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Source  string           `json:"source"`
		Models  []chatModelEntry `json:"models"`
		Default string           `json:"default"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "openrouter", resp.Source)
	require.Len(t, resp.Models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4.5", resp.Models[0].ID)
	assert.Equal(t, int64(200000), resp.Models[0].MaxTokens)
}

func TestCreateChat_OpenRouter_SkipsAllowlist(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, openRouterFixtureOpts())
	// No validateModel is wired, so openrouter mode fails open and accepts
	// any slug as-is.
	body := `{"title":"x","project":"p","model":"openai/gpt-5"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))
	require.Equal(t, 201, rec.Code, "body=%s", rec.Body.String())

	var sess chat.Session
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sess))
	require.Equal(t, "openai/gpt-5", sess.Model)
}

func TestCreateChat_OpenRouter_EmptyModelFallsBackToBackendDefault(t *testing.T) {
	t.Parallel()
	mux, _ := newChatFixture(t, openRouterFixtureOpts())
	body := `{"title":"x","project":"p"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))
	require.Equal(t, 201, rec.Code, "body=%s", rec.Body.String())

	var sess chat.Session
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sess))
	require.Equal(t, "anthropic/claude-sonnet-4", sess.Model)
}

func TestCreateChat_OpenRouter_RejectsUnknownModel(t *testing.T) {
	t.Parallel()

	opts := openRouterFixtureOpts()
	opts.validateModel = func(_ context.Context, slug string) bool {
		return slug == "anthropic/claude-sonnet-4.5"
	}
	mux, _ := newChatFixture(t, opts)

	body := `{"title":"t","project":"alpha","model":"anthropic/claude-sonet-4.5"}` // typo
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_MODEL")
	assert.Contains(t, rec.Body.String(), "anthropic/claude-sonet-4.5")
}

func TestCreateChat_OpenRouter_AcceptsKnownModel(t *testing.T) {
	t.Parallel()

	opts := openRouterFixtureOpts()
	opts.validateModel = func(_ context.Context, slug string) bool {
		return slug == "anthropic/claude-sonnet-4.5"
	}
	mux, _ := newChatFixture(t, opts)

	body := `{"title":"t","project":"alpha","model":"anthropic/claude-sonnet-4.5"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))

	assert.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
}

func TestCreateChat_Endpoint_RejectsUnknownModel(t *testing.T) {
	t.Parallel()

	opts := defaultFixtureOpts()
	opts.endpointModels = func(_ context.Context) ([]chatModelEntry, error) {
		return []chatModelEntry{{ID: "model-a", Label: "Model A", MaxTokens: 100000}}, nil
	}
	mux, _ := newChatFixture(t, opts)

	body := `{"title":"t","project":"alpha","model":"model-b"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_MODEL")
}

func TestClearChat_Success(t *testing.T) {
	mux, mgr, _ := newChatFixtureWithRunner(t, defaultFixtureOpts())
	// Wrap with csrfGuard so the success path exercises the same gate the
	// production router uses; the bare mux would otherwise skip it.
	h := csrfGuard(mux)

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open the session so the runner container is active. ClearContext now
	// requires an active or warm-idle session.
	_, err = mgr.OpenSession(context.Background(), sess.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/clear",
		bytes.NewBufferString(`{}`))
	req.Header.Set("X-Requested-With", "contextmatrix")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, "body=%s", w.Body.String())

	// Divider row was persisted with kind=divider and the canonical marker.
	msgs, err := mgr.ListMessages(context.Background(), sess.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, chat.RoleSystem, msgs[0].Role)
	assert.Equal(t, chat.ContextClearedMarker, msgs[0].Content)
	assert.Equal(t, chat.EventKindDivider, msgs[0].Kind,
		"persisted divider row must carry kind so REST bootstrap renders the rule on reload")
}

func TestClearChat_MissingCSRF(t *testing.T) {
	mux, mgr, _ := newChatFixtureWithRunner(t, defaultFixtureOpts())
	h := csrfGuard(mux)

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// CSRF check runs before the handler body, so the session state does not
	// matter for this test. Open anyway to be consistent with the happy path.
	_, err = mgr.OpenSession(context.Background(), sess.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/clear",
		bytes.NewBufferString(`{}`))
	// Intentionally omit X-Requested-With.

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestClearChat_NotFound(t *testing.T) {
	mux, _, _ := newChatFixtureWithRunner(t, defaultFixtureOpts())

	req := httptest.NewRequest(http.MethodPost, "/api/chats/no-such/clear",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestClearChat_RunnerFailure(t *testing.T) {
	mux, mgr, runner := newChatFixtureWithRunner(t, defaultFixtureOpts())

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open the session so it is active, then arm the send error for /clear.
	_, err = mgr.OpenSession(context.Background(), sess.ID)
	require.NoError(t, err)

	runner.sendErr = errors.New("runner unreachable")

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/clear",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code, "body=%s", w.Body.String())

	var apiErr APIError

	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerUnavailable, apiErr.Code)
	assert.Equal(t, "clear_failed", apiErr.Details, "body must carry detail=clear_failed for /clear step failure")
}

// TestClearChat_ColdSession asserts that POST .../clear returns 409
// RUNNER_NOT_RUNNING when the target session is cold (no running container).
func TestClearChat_ColdSession(t *testing.T) {
	mux, mgr, _ := newChatFixtureWithRunner(t, defaultFixtureOpts())

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Session is cold — do NOT open it.
	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/clear",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerNotRunning, apiErr.Code)
}

// TestClearChat_RunnerFailure_TranscriptUntouched asserts that when the runner
// /clear call fails (502 path) the transcript remains empty — no rows were
// persisted and no divider was inserted.
func TestClearChat_RunnerFailure_TranscriptUntouched(t *testing.T) {
	mux, mgr, runner := newChatFixtureWithRunner(t, defaultFixtureOpts())

	sess, err := mgr.CreateSession(context.Background(),
		chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open the session so it is active.
	_, err = mgr.OpenSession(context.Background(), sess.ID)
	require.NoError(t, err)

	// Arm the runner to fail on /clear.
	runner.sendErr = errors.New("runner unreachable")

	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+sess.ID+"/clear",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code, "body=%s", w.Body.String())

	// Transcript must be completely untouched: no rows, no phase flips.
	msgs, err := mgr.ListMessages(context.Background(), sess.ID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, msgs,
		"transcript must remain empty when the runner /clear call fails")
}

func TestListModelsEndpointSource(t *testing.T) {
	t.Parallel()

	h := &chatHandlers{
		endpointModels: func(_ context.Context) ([]chatModelEntry, error) {
			return []chatModelEntry{{ID: "model-a", Label: "model-a", MaxTokens: 200000}}, nil
		},
		orDefault: "model-a",
	}

	rec := httptest.NewRecorder()
	h.listModels(rec, httptest.NewRequest(http.MethodGet, "/api/chats/models", nil))

	var resp struct {
		Source  string           `json:"source"`
		Models  []chatModelEntry `json:"models"`
		Default string           `json:"default"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "endpoint", resp.Source)
	require.Len(t, resp.Models, 1)
	assert.Equal(t, "model-a", resp.Models[0].ID)
}

// TestListModelsServesCachedEndpoint verifies that the endpoint model fetch is
// cached: two consecutive GET /api/chats/models requests must only trigger one
// upstream call.
func TestListModelsServesCachedEndpoint(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	mux, _ := newChatFixture(t, fixtureOpts{
		endpointModels: func(_ context.Context) ([]chatModelEntry, error) {
			calls.Add(1)

			return []chatModelEntry{{ID: "ep-model", Label: "EP Model", MaxTokens: 8192}}, nil
		},
	})

	for range 2 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chats/models", nil))
		require.Equal(t, 200, rec.Code)
	}

	require.Equal(t, int64(1), calls.Load(),
		"upstream endpoint model fetch must be cached — two requests should trigger only one call")
}

// TestListModelsSurfacesFetchError verifies that when the endpoint model fetch
// fails, listModels returns a response that distinguishes the error from "no
// models" (i.e. the fetch_error field is populated, not a bare empty 200).
func TestListModelsSurfacesFetchError(t *testing.T) {
	t.Parallel()

	mux, _ := newChatFixture(t, fixtureOpts{
		endpointModels: func(_ context.Context) ([]chatModelEntry, error) {
			return nil, fmt.Errorf("upstream unavailable")
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chats/models", nil))
	require.Equal(t, 200, rec.Code)

	var body struct {
		Source     string `json:"source"`
		FetchError string `json:"fetch_error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "endpoint", body.Source)
	require.NotEmpty(t, body.FetchError,
		"fetch error must be surfaced in fetch_error field, not silently dropped as an empty model list")
}

// TestCreateChatAcceptsEndpointModel verifies that when the endpoint picker is
// active, createChat accepts a model that is in the endpoint catalog and
// stores it verbatim.
func TestCreateChatAcceptsEndpointModel(t *testing.T) {
	t.Parallel()

	mux, _ := newChatFixture(t, fixtureOpts{
		endpointModels: func(_ context.Context) ([]chatModelEntry, error) {
			return []chatModelEntry{
				{ID: "ep-model-a", Label: "EP Model A", MaxTokens: 32768},
			}, nil
		},
	})

	body := `{"title":"x","project":"p","model":"ep-model-a"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, "POST", "/api/chats", body))
	require.Equal(t, 201, rec.Code,
		"endpoint picker model must be accepted without allowlist check: body=%s", rec.Body.String())

	var sess chat.Session
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sess))
	require.Equal(t, "ep-model-a", sess.Model)
}
