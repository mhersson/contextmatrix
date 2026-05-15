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
