//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// chatReplyText is the canned assistant reply for the chat scenario; the
// scenario asserts a substring of it arrives on the chat transcript.
const chatReplyText = "Hello from the scripted chat backend."

// chatScriptReply answers every chat-worker request with the same canned
// assistant turn (a plain stop, no tool calls), carrying a scripted usage cost.
func chatScriptReply(chatRequest) string {
	return sseStop(chatReplyText, 0.0020)
}

// TestChatScenario boots the contextmatrix-chat backend and its worker image,
// creates a project-scoped chat, sends one message, and asserts the scripted
// assistant reply arrives on the transcript before ending the session cleanly.
func TestChatScenario(t *testing.T) {
	requireContainerNetworking(t) // t.Skip when the daemon lacks bridge networking.
	assets := ensureChatAssets(t) // t.Skip when the sibling repo is absent.

	const project = "harness"

	h := bootHarness(t, "chat", project, cmConfigOptions{chat: true},
		chatScriptReply, chatWorkerImage, "contextmatrix.chat=true")

	chat := startChatBackend(t, assets.hostBinary, h.sc.writeChatConfig(t), h.sc.chatPort, h.rl)
	_ = chat

	capture := startWorkerCapture(h.rl, "contextmatrix.chat=true")
	t.Cleanup(capture.stop)

	// Project-scoped chat so the worker image resolves from the board's
	// remote_execution.chat_worker_image.
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	status, body := h.client.do(t, http.MethodPost, "/api/chats",
		map[string]any{"title": "smoke", "project": project}, &created)
	if status != http.StatusCreated {
		t.Fatalf("create chat: HTTP %d body=%s", status, body)
	}

	if created.ID == "" {
		t.Fatalf("create chat: empty id (%s)", body)
	}

	// Open boots the container; send the user message once it is opening.
	if status, body := h.client.do(t, http.MethodPost, "/api/chats/"+created.ID+"/open", nil, nil); status != http.StatusOK {
		t.Fatalf("open chat: HTTP %d body=%s\nchat stderr tail:\n%s", status, body, tail(h.rl.chatSink.String(), 60))
	}

	if status, body := h.client.do(t, http.MethodPost, "/api/chats/"+created.ID+"/messages",
		map[string]any{"content": "Hello, chat."}, nil); status != http.StatusAccepted {
		t.Fatalf("send message: HTTP %d body=%s\nchat stderr tail:\n%s", status, body, tail(h.rl.chatSink.String(), 60))
	}

	// Poll the transcript for the scripted assistant reply. Generous margin for
	// the container boot + repo clone + the LLM round-trip.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	pollUntil(ctx, t, "scripted assistant reply on transcript", func() bool {
		return h.chatHasAssistantReply(t, created.ID, chatReplyText)
	})

	// End the session cleanly.
	if status, body := h.client.do(t, http.MethodPost, "/api/chats/"+created.ID+"/end", nil, nil); status != http.StatusOK {
		t.Fatalf("end chat: HTTP %d body=%s", status, body)
	}
}

// chatHasAssistantReply reports whether the chat transcript carries an
// assistant message whose content includes want.
func (h *harness) chatHasAssistantReply(t *testing.T, chatID, want string) bool {
	t.Helper()

	var resp struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	status, body := h.client.do(t, http.MethodGet, "/api/chats/"+chatID+"/messages", nil, &resp)
	if status != http.StatusOK {
		t.Fatalf("list messages: HTTP %d body=%s", status, body)
	}

	for _, m := range resp.Messages {
		if m.Role != "user" && m.Role != "system" && strings.Contains(m.Content, want) {
			return true
		}
	}

	return false
}
