//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestIntegrationHarness is the entry point for all stub scenarios.
func TestIntegrationHarness(t *testing.T) {
	t.Run("Autonomous", testAutonomousStub)
	t.Run("HITL", testHITLStub)
	t.Run("KillMidRun", testKillMidRunStub)
	t.Run("HeartbeatTimeout", testHeartbeatTimeoutStub)
	t.Run("PromoteHITLToAuto", testPromoteHITLToAutoStub)
	t.Run("IdleWatchdog", testIdleWatchdogStub)
	t.Run("Chat", testChatStub)
}

func testAutonomousStub(t *testing.T) {
	scenarioID := "autonomous"
	project := "harness"

	s := bootScenario(t, scenarioID, project)

	cardID := s.createCard(t, "stub autonomous", false /* autonomous flag */)

	s.triggerRun(t, cardID, false /* interactive */)

	// Stub completes in ~600ms; allow generous margin.
	final := s.waitForState(t, cardID, "done", 30*time.Second)

	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared after release, got %q", final.AssignedAgent)
	}

	// Activity-log assertion: the MCP loop should have produced at
	// least one entry. A regression that swallowed activity logging
	// would still pass the state checks above; this catches it.
	if len(final.ActivityLog) == 0 {
		t.Errorf("activity log empty; expected entries from claim/transition/release")
	}
}

func testKillMidRunStub(t *testing.T) {
	scenarioID := "killmidrun"
	project := "harness"

	s := bootScenario(t, scenarioID, project)

	body := "Test card.\n\nSTUB-DIRECTIVE: hang-after-claim=1\n"
	cardID := s.createCardWithBody(t, "stub kill", body, false /* autonomous */)

	s.triggerRun(t, cardID, false /* interactive */)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	s.waitForCardClaimed(ctx, t, cardID)

	s.stopCard(t, cardID)

	s.waitForAgentCleared(ctx, t, cardID)

	// Container should be removed within ~10s.
	pollUntil(ctx, t, "worker container removed", func() bool {
		return len(dockerListByScenario(scenarioID)) == 0
	})

	card := s.client.getCard(t, project, cardID)
	if card.State == "done" {
		t.Errorf("card should not have reached done after /stop, state=%s", card.State)
	}
}

func testHITLStub(t *testing.T) {
	scenarioID := "hitl"
	project := "harness"

	s := bootScenario(t, scenarioID, project)

	cardID := s.createCard(t, "stub HITL", false /* autonomous flag */)
	buf := s.startTranscript(t, cardID)

	s.triggerRun(t, cardID, true /* interactive */)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stub emits "Awaiting input…" after claim_card.
	waitForTranscriptText(ctx, t, buf, "Awaiting input")

	s.messageCard(t, cardID, "approve")

	final := s.waitForState(t, cardID, "done", 30*time.Second)
	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared, got %q", final.AssignedAgent)
	}
}

func testHeartbeatTimeoutStub(t *testing.T) {
	scenarioID := "heartbeat"
	project := "harness"

	s := bootScenarioWithConfig(t, scenarioID, project, func(cfg *scenarioConfig) {
		cfg.heartbeatTimeoutSeconds = 5
		// Shrink CM's stalled-check tick to 2s (default 1m). Without this
		// the test races a 60s tick — and if the runner kills the worker
		// container for ANY reason before 60s, the runner posts failed,
		// CM clears AssignedAgent, FindStalled then skips the card, and
		// the test can never reach state=stalled. With 2s tick the
		// stalled transition fires reliably within ~7s of trigger.
		cfg.stalledCheckSeconds = 2
	})

	body := "Test card.\n\nSTUB-DIRECTIVE: skip-heartbeat=1\nSTUB-DIRECTIVE: hang-after-claim=1\n"
	cardID := s.createCardWithBody(t, "stub heartbeat", body, false)

	s.triggerRun(t, cardID, false)

	// CM's stalled-checker now ticks every 2s (per stalledCheckSeconds
	// override above). 5s heartbeat_timeout + 2s tick + processing = well
	// under 15s; 30s is generous slack.
	final := s.waitForState(t, cardID, "stalled", 30*time.Second)
	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared on stalled, got %q", final.AssignedAgent)
	}
}

func testPromoteHITLToAutoStub(t *testing.T) {
	scenarioID := "promote"
	project := "harness"

	s := bootScenario(t, scenarioID, project)

	cardID := s.createCard(t, "stub promote", false)
	buf := s.startTranscript(t, cardID)
	_ = buf

	s.triggerRun(t, cardID, true /* interactive */)

	// Wait for claim: card enters in_progress when the stub calls claim_card,
	// which means the HITL loop is running and the stub is blocking on stdin.
	// Using waitForState instead of waitForTranscriptText avoids a race where
	// the "Awaiting input" SSE event is emitted before CM's session pump
	// connects to the runner's broadcaster.
	s.waitForState(t, cardID, "in_progress", 30*time.Second)

	// Promote without sending a chat message.
	s.promoteCard(t, cardID)

	final := s.waitForState(t, cardID, "done", 30*time.Second)
	if !final.Autonomous {
		t.Errorf("card.autonomous should be true after promote")
	}

	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared, got %q", final.AssignedAgent)
	}
}

func testIdleWatchdogStub(t *testing.T) {
	scenarioID := "idle"
	project := "harness"

	s := bootScenarioWithConfig(t, scenarioID, project, func(cfg *scenarioConfig) {
		cfg.idleWatchdogSeconds = 2
		cfg.idleOutputTimeoutSeconds = 5
	})

	body := "Test card.\n\nSTUB-DIRECTIVE: hang-after-claim=1\n"
	cardID := s.createCardWithBody(t, "stub idle", body, false)

	s.triggerRun(t, cardID, false)

	// Wait for claim, then for the watchdog to kill the container.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.waitForCardClaimed(ctx, t, cardID)
	s.waitForAgentCleared(ctx, t, cardID)

	pollUntil(ctx, t, "worker container removed", func() bool {
		return len(dockerListByScenario(scenarioID)) == 0
	})
}

// testChatStub validates the global-chat REST API end-to-end (create, get,
// list, patch, delete) against a real CM binary with a live SQLite store.
//
// What this covers: router wiring, handler logic, SQLite persistence, and
// correct HTTP status codes for all five chat lifecycle operations.
//
// What this defers (follow-up work):
//   - Sending a message and receiving SSE events — requires the stub-worker to
//     accept /chat/start and the SSE bridge to pump events through.
//   - Reopen flow (cold → active → cold) — same dependency on the runner stub.
func testChatStub(t *testing.T) {
	scenarioID := "chat"
	project := "harness"

	s := bootScenario(t, scenarioID, project)

	// Create chat — no project field means cross-project / no container clone.
	var created struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	status, body := s.client.postRaw(t, "/api/chats", map[string]any{"title": "smoke"}, &created)
	if status != http.StatusCreated {
		t.Fatalf("create chat: HTTP %d body=%s", status, body)
	}
	if created.ID == "" || created.Status != "cold" {
		t.Fatalf("create chat returned unexpected payload: %+v", created)
	}

	// Get chat by ID.
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	statusGet := s.client.get(t, "/api/chats/"+created.ID, &got)
	if statusGet != http.StatusOK {
		t.Fatalf("get chat: HTTP %d", statusGet)
	}
	if got.ID != created.ID {
		t.Fatalf("get chat id mismatch: %s vs %s", got.ID, created.ID)
	}

	// List chats — the newly created one must appear.
	var list []map[string]any
	statusList := s.client.get(t, "/api/chats", &list)
	if statusList != http.StatusOK {
		t.Fatalf("list chats: HTTP %d", statusList)
	}
	found := false
	for _, c := range list {
		if c["id"] == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("list chats: created id %s not in list", created.ID)
	}

	// PATCH title.
	var patched struct {
		Title string `json:"title"`
	}
	statusPatch, patchBody := s.client.patch(t, "/api/chats/"+created.ID, map[string]any{"title": "renamed"}, &patched)
	if statusPatch != http.StatusOK {
		t.Fatalf("patch chat: HTTP %d body=%s", statusPatch, patchBody)
	}
	if patched.Title != "renamed" {
		t.Fatalf("patch chat: title not updated: %q", patched.Title)
	}

	// DELETE chat.
	statusDel := s.client.deleteReq(t, "/api/chats/"+created.ID)
	if statusDel != http.StatusNoContent {
		t.Fatalf("delete chat: HTTP %d", statusDel)
	}

	// GET after delete must return 404.
	statusGone := s.client.get(t, "/api/chats/"+created.ID, nil)
	if statusGone != http.StatusNotFound {
		t.Fatalf("get deleted chat: expected 404 got %d", statusGone)
	}
}
