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
	err := rc.SendChatMessage(context.Background(), "S1", "hello", "msg-1", "")
	require.NoError(t, err)
	assert.Equal(t, "/message", receivedPath)
	assert.Equal(t, "S1", receivedBody["session_id"])
	assert.Equal(t, "hello", receivedBody["content"])
	assert.Equal(t, "msg-1", receivedBody["message_id"])
}

func TestRunnerClient_SendChatMessage_ToolUseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		toolUseID     string
		wantInBody    bool
		wantToolUseID string
	}{
		{
			name:          "with tool_use_id set",
			toolUseID:     "toolu_abc",
			wantInBody:    true,
			wantToolUseID: "toolu_abc",
		},
		{
			name:       "with empty tool_use_id omits field",
			toolUseID:  "",
			wantInBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var receivedBody map[string]any

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &receivedBody)

				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(srv.Close)

			rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})
			err := rc.SendChatMessage(context.Background(), "S1", "answer", "msg-1", tt.toolUseID)
			require.NoError(t, err)

			_, present := receivedBody["tool_use_id"]
			assert.Equal(t, tt.wantInBody, present, "tool_use_id presence mismatch")

			if tt.wantInBody {
				assert.Equal(t, tt.wantToolUseID, receivedBody["tool_use_id"])
			}
		})
	}
}

func TestRunnerClient_StreamLogs_ToolUseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		frame         string
		wantToolUseID string
	}{
		{
			name:          "frame with tool_use_id",
			frame:         `{"ts":"2024-01-01T00:00:00Z","type":"user_question","content":"Which?","tool_use_id":"toolu_abc"}`,
			wantToolUseID: "toolu_abc",
		},
		{
			name:          "frame without tool_use_id",
			frame:         `{"ts":"2024-01-01T00:00:00Z","type":"user_question","content":"Which?"}`,
			wantToolUseID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got chat.LogEntry

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("data: " + tt.frame + "\n\n"))
			}))
			t.Cleanup(srv.Close)

			rc := chat.NewRunnerClient(chat.RunnerClientConfig{BaseURL: srv.URL, HMACKey: "k"})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			_ = rc.StreamLogs(ctx, "S1", func(e chat.LogEntry) {
				got = e

				cancel()
			})

			assert.Equal(t, tt.wantToolUseID, got.ToolUseID)
		})
	}
}
