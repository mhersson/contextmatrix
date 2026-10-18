package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/chat"
)

func TestBackendClient_StartChat_HappyPath(t *testing.T) {
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

	rc := chat.NewBackendClient(chat.BackendClientConfig{
		BaseURL: srv.URL,
		HMACKey: "k",
	})
	containerID, err := rc.StartChat(context.Background(), chat.StartChatOpts{
		SessionID:   "S1",
		Project:     "alpha",
		RepoURL:     "https://x/y",
		WorkerImage: "ghcr.io/acme/rust-worker:latest",
		Model:       "claude-sonnet-4-6",
		Resume: &chat.ResumeContext{
			Turns: []chat.ResumeTurn{{Seq: 1, Role: "user", Content: "hi"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "c-1", containerID)
	assert.Equal(t, "/chat/start", received.path)
	assert.Equal(t, "S1", received.body["session_id"])
	assert.Equal(t, "alpha", received.body["project"])
	assert.Equal(t, "ghcr.io/acme/rust-worker:latest", received.body["worker_image"])
	assert.Equal(t, "claude-sonnet-4-6", received.body["model"])
	assert.NotEmpty(t, received.sig)
	assert.NotEmpty(t, received.ts)

	resume, ok := received.body["resume"].(map[string]any)
	require.True(t, ok, "resume should be present in payload")

	turns, ok := resume["turns"].([]any)
	require.True(t, ok)
	require.Len(t, turns, 1)
}

func TestBackendClient_EndChat_ReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no_container"}`))
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})
	err := rc.EndChat(context.Background(), "S1")
	require.Error(t, err)
}

func TestBackendClient_StartChat_MarshalsLLMEndpoint(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "container_id": "c-1"})
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

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

func TestBackendClient_StartChat_MarshalsGitCredentialsToken(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "container_id": "c-1"})
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	// Case 1: GitCredentialsToken set → JSON contains "git_credentials_token".
	_, err := rc.StartChat(context.Background(), chat.StartChatOpts{
		SessionID:           "S1",
		GitCredentialsToken: "S1.abcd1234",
	})
	require.NoError(t, err)
	assert.Equal(t, "S1.abcd1234", received["git_credentials_token"])

	// Case 2: GitCredentialsToken empty → field omitted from JSON.
	received = nil
	_, err = rc.StartChat(context.Background(), chat.StartChatOpts{SessionID: "S2"})
	require.NoError(t, err)

	_, present := received["git_credentials_token"]
	assert.False(t, present, "git_credentials_token field must be omitted when empty (omitempty)")
}

func TestBackendClient_SendChatMessage_PostsToMessage(t *testing.T) {
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

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})
	err := rc.SendChatMessage(context.Background(), protocol.MessagePayload{SessionID: "S1", Content: "hello", MessageID: "msg-1"})
	require.NoError(t, err)
	assert.Equal(t, "/message", receivedPath)
	assert.Equal(t, "S1", receivedBody["session_id"])
	assert.Equal(t, "hello", receivedBody["content"])
	assert.Equal(t, "msg-1", receivedBody["message_id"])
}

func TestBackendClient_SendChatMessage_WrapsErrBackendUnreachableOnDialFailure(t *testing.T) {
	// Port 1 refuses connections without needing a listener.
	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: "http://127.0.0.1:1", HMACKey: "k"})

	err := rc.SendChatMessage(context.Background(), protocol.MessagePayload{SessionID: "S1", Content: "hi", MessageID: "msg-1"})
	require.Error(t, err)
	assert.ErrorIs(t, err, chat.ErrBackendUnreachable)
}

func TestBackendClient_SendChatMessage_DoesNotWrapErrBackendUnreachableOnCallerCancel(t *testing.T) {
	// A real, reachable server - proves the failure is caller-side, not a
	// dial/DNS/timeout problem with the backend.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled: Do fails immediately with context.Canceled before any dial matters

	err := rc.SendChatMessage(ctx, protocol.MessagePayload{SessionID: "S1", Content: "hi", MessageID: "msg-1"})
	require.Error(t, err)
	require.NotErrorIs(t, err, chat.ErrBackendUnreachable)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestBackendClient_SendChatMessage_SessionUnknown(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		unknown bool
	}{
		{"protocol not_found", http.StatusNotFound, `{"ok":false,"code":"not_found","message":"no such blueprint session"}`, true},
		{"mux 404 text", http.StatusNotFound, "404 page not found", false},
		{"404 with another code", http.StatusNotFound, `{"ok":false,"code":"invalid_field"}`, false},
		{"not_found code on another status", http.StatusBadRequest, `{"ok":false,"code":"not_found"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})
			err := rc.SendChatMessage(context.Background(), protocol.MessagePayload{SessionID: "S1", Content: "hi", MessageID: "m1"})
			require.Error(t, err)
			assert.Equal(t, tt.unknown, errors.Is(err, chat.ErrSessionUnknown))
			assert.NotErrorIs(t, err, chat.ErrBackendUnreachable)
		})
	}
}

func TestBackendClient_StreamLogs_CarriesThePrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:05Z","type":"text","content":"Pick a store.",`+
			`"prompt":{"id":"Q3","free_text":true,"actions":[{"label":"Postgres","reply":"Postgres"}]}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:06Z","type":"text","content":"plain"}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	var got []chat.LogEntry

	require.NoError(t, rc.StreamLogs(context.Background(), "S1", func(e chat.LogEntry) { got = append(got, e) }))

	require.Len(t, got, 2)
	require.NotNil(t, got[0].Prompt)
	assert.Equal(t, protocol.LogPrompt{
		ID: "Q3", FreeText: true,
		Actions: []protocol.LogAction{{Label: "Postgres", Reply: "Postgres"}},
	}, *got[0].Prompt)
	assert.Nil(t, got[1].Prompt, "a frame without a prompt carries none")
}

func TestBackendClient_StreamLogs_CarriesTheMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:05Z","type":"text","content":"Design rev 2",`+
			`"marker":{"kind":"artifact","stage":"solution_design","persona":"System designer","version":2,"note":"Revised for the 3 findings"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:06Z","type":"text","content":"plain"}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	var got []chat.LogEntry

	require.NoError(t, rc.StreamLogs(context.Background(), "S1", func(e chat.LogEntry) { got = append(got, e) }))

	require.Len(t, got, 2)
	require.NotNil(t, got[0].Marker)
	assert.Equal(t, protocol.LogMarker{
		Kind: "artifact", Stage: "solution_design", Persona: "System designer", Version: 2, Note: "Revised for the 3 findings",
	}, *got[0].Marker)
	assert.Nil(t, got[1].Marker, "a frame without a marker carries none")
}

func TestBackendClient_StreamLogs_KeepsTheSlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:05Z","type":"text","content":"Design rev 1",`+
			`"marker":{"kind":"artifact","stage":"solution_design","slice":"SL2","version":1}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"ts":"2026-01-02T03:04:06Z","type":"text","content":"1 question",`+
			`"prompt":{"id":"round:4","kind":"round","round":{"persona":"system_designer","stage":"solution_design","slice":"SL2",`+
			`"questions":[{"number":1,"title":"Q1","text":"Which log store?","options":[{"title":"Loki"}]}]}}}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	rc := chat.NewBackendClient(chat.BackendClientConfig{BaseURL: srv.URL, HMACKey: "k"})

	var got []chat.LogEntry

	require.NoError(t, rc.StreamLogs(context.Background(), "S1", func(e chat.LogEntry) { got = append(got, e) }))
	require.Len(t, got, 2)
	require.NotNil(t, got[0].Marker)
	require.NotNil(t, got[1].Prompt)
	require.NotNil(t, got[1].Prompt.Round)

	assert.Equal(t, "SL2", jsonField(t, got[0].Marker, "slice"), "a sheet reference opens its slice's version")
	assert.Equal(t, "SL2", jsonField(t, got[1].Prompt.Round, "slice"), "a round header names its slice")
}

// jsonField marshals v and returns one top-level string field: the wire
// shape CM stores, whichever protocol version the build pins.
func jsonField(t *testing.T, v any, key string) string {
	t.Helper()

	b, err := json.Marshal(v)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	s, _ := m[key].(string)

	return s
}
