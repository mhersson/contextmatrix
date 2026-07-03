package chat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/chat"
)

func TestRunnerClient_StartChat_HappyPath(t *testing.T) {
	var received struct {
		path string
		body map[string]any
		sig  string
		ts   string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.path = r.URL.Path
		received.sig = r.Header.Get("X-Signature-256")
		received.ts = r.Header.Get("X-Webhook-Timestamp")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received.body)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "container_id": "c-1"})
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{
		BaseURL: srv.URL,
		HMACKey: "k",
	})
	containerID, err := rc.StartChat(context.Background(), chat.StartChatOpts{
		SessionID: "S1",
		Project:   "alpha",
		RepoURL:   "https://x/y",
		Model:     "claude-sonnet-4-6",
		Resume: &chat.ResumeContext{
			Turns: []chat.ResumeTurn{{Seq: 1, Role: "user", Content: "hi"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "c-1", containerID)
	assert.Equal(t, "/chat/start", received.path)
	assert.Equal(t, "S1", received.body["session_id"])
	assert.Equal(t, "alpha", received.body["project"])
	assert.Equal(t, "claude-sonnet-4-6", received.body["model"])
	assert.NotEmpty(t, received.sig)
	assert.NotEmpty(t, received.ts)

	resume, ok := received.body["resume"].(map[string]any)
	require.True(t, ok, "resume should be present in payload")

	turns, ok := resume["turns"].([]any)
	require.True(t, ok)
	require.Len(t, turns, 1)
}

func TestRunnerClient_EndChat_ReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no_container"}`))
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})
	err := rc.EndChat(context.Background(), "S1")
	require.Error(t, err)
}

func TestRunnerClient_StartChat_MarshalsPrimer(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "container_id": "c-1"})
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	// Case 1: Primer set → JSON contains "primer" with the value.
	_, err := rc.StartChat(context.Background(), chat.StartChatOpts{
		SessionID: "S1",
		Primer:    "ORIENT",
	})
	require.NoError(t, err)
	assert.Equal(t, "ORIENT", received["primer"])

	// Case 2: Primer empty → "primer" omitted from JSON.
	received = nil
	_, err = rc.StartChat(context.Background(), chat.StartChatOpts{SessionID: "S2"})
	require.NoError(t, err)

	_, present := received["primer"]
	assert.False(t, present, "primer field must be omitted when empty (omitempty)")
}

func TestRunnerClient_StartChat_MarshalsLLMEndpoint(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "container_id": "c-1"})
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	// Case 1: LLMEndpoint set → JSON contains "llm_endpoint" with the configured fields.
	_, err := rc.StartChat(context.Background(), chat.StartChatOpts{
		SessionID: "S1",
		LLMEndpoint: &protocol.LLMEndpoint{
			Type:    "openrouter",
			BaseURL: "https://openrouter.ai/api/v1",
			APIKey:  "sk-test-key",
		},
	})
	require.NoError(t, err)

	endpoint, ok := received["llm_endpoint"].(map[string]any)
	require.True(t, ok, "llm_endpoint should be present in payload")
	assert.Equal(t, "openrouter", endpoint["type"])
	assert.Equal(t, "https://openrouter.ai/api/v1", endpoint["base_url"])
	assert.Equal(t, "sk-test-key", endpoint["api_key"])

	// Case 2: LLMEndpoint nil → "llm_endpoint" omitted from JSON.
	received = nil
	_, err = rc.StartChat(context.Background(), chat.StartChatOpts{SessionID: "S2"})
	require.NoError(t, err)

	_, present := received["llm_endpoint"]
	assert.False(t, present, "llm_endpoint field must be omitted when nil (omitempty)")
}

func TestRunnerClient_SendChatMessage_PostsToMessage(t *testing.T) {
	var (
		receivedBody map[string]any
		receivedPath string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})
	err := rc.SendChatMessage(context.Background(), "S1", "hello", "msg-1")
	require.NoError(t, err)
	assert.Equal(t, "/message", receivedPath)
	assert.Equal(t, "S1", receivedBody["session_id"])
	assert.Equal(t, "hello", receivedBody["content"])
	assert.Equal(t, "msg-1", receivedBody["message_id"])
}

func TestRunnerClient_SendChatMessage_WrapsErrBackendUnreachableOnDialFailure(t *testing.T) {
	// Port 1 refuses connections without needing a listener.
	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: "http://127.0.0.1:1", HMACKey: "k"})

	err := rc.SendChatMessage(context.Background(), "S1", "hi", "msg-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, chat.ErrBackendUnreachable)
}

func TestRunnerClient_SendChatMessage_DoesNotWrapErrBackendUnreachableOnCallerCancel(t *testing.T) {
	// A real, reachable server — proves the failure is caller-side, not a
	// dial/DNS/timeout problem with the backend.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled: Do fails immediately with context.Canceled before any dial matters

	err := rc.SendChatMessage(ctx, "S1", "hi", "msg-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, chat.ErrBackendUnreachable)
	assert.ErrorIs(t, err, context.Canceled)
}
