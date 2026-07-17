//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestChatREST validates the chat REST surface (create/get/list/patch/delete)
// against a real CM with a live SQLite store and no backend configured - the
// chat routes register whenever the manager is wired.
func TestChatREST(t *testing.T) {
	const project = "harness"

	sc := newScenarioConfig(t, "chatrest")
	initBoardsRepo(t, sc, project)

	rl, err := newRunLog("chatrest")
	if err != nil {
		t.Fatalf("runlog: %v", err)
	}

	start := time.Now()
	t.Cleanup(func() {
		status := "PASS"
		if t.Failed() {
			status = "FAIL"
		}

		rl.finalize("chatrest", status, time.Since(start), nil)
	})

	cfgPath := sc.writeCMConfig(t, cmConfigOptions{})
	cm := startCM(t, cfgPath, sc.cmPort, rl)

	client := bootAdminSession(t, fmt.Sprintf("http://127.0.0.1:%d", sc.cmPort), cm)

	// Create - no project field means a cross-project chat (no container).
	var createdChat struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if status, body := client.do(t, http.MethodPost, "/api/chats", map[string]any{"title": "smoke"}, &createdChat); status != http.StatusCreated {
		t.Fatalf("create chat: HTTP %d body=%s", status, body)
	}

	if createdChat.ID == "" || createdChat.Status != "cold" {
		t.Fatalf("create chat returned unexpected payload: %+v", createdChat)
	}

	// Get.
	var got struct {
		ID string `json:"id"`
	}
	if status, body := client.do(t, http.MethodGet, "/api/chats/"+createdChat.ID, nil, &got); status != http.StatusOK {
		t.Fatalf("get chat: HTTP %d body=%s", status, body)
	}

	if got.ID != createdChat.ID {
		t.Fatalf("get chat id mismatch: %s vs %s", got.ID, createdChat.ID)
	}

	// List - the new chat must appear.
	var list []map[string]any
	if status, body := client.do(t, http.MethodGet, "/api/chats", nil, &list); status != http.StatusOK {
		t.Fatalf("list chats: HTTP %d body=%s", status, body)
	}

	if !hasField(list, "id", createdChat.ID) {
		t.Fatalf("list chats: created id %s not present", createdChat.ID)
	}

	// PATCH title.
	var patched struct {
		Title string `json:"title"`
	}
	if status, body := client.do(t, http.MethodPatch, "/api/chats/"+createdChat.ID, map[string]any{"title": "renamed"}, &patched); status != http.StatusOK {
		t.Fatalf("patch chat: HTTP %d body=%s", status, body)
	}

	if patched.Title != "renamed" {
		t.Fatalf("patch chat: title not updated: %q", patched.Title)
	}

	// DELETE, then GET → 404.
	if status, _ := client.do(t, http.MethodDelete, "/api/chats/"+createdChat.ID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete chat: HTTP %d", status)
	}

	if status, _ := client.do(t, http.MethodGet, "/api/chats/"+createdChat.ID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("get deleted chat: expected 404 got %d", status)
	}
}
