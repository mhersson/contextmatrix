//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	stubScenarioTimeout       = 60 * time.Second
	realClaudeScenarioTimeout = 8 * time.Minute
)

func TestIntegrationHarness(t *testing.T) {
	t.Run("Autonomous", testAutonomousStub)
	t.Run("HITL", testHITLStub)
	t.Run("HITL_ReviewLoop", testHITLReviewLoopStub)
	t.Run("HITL_Promote", testHITLPromoteStub)
	t.Run("HITL_BrainstormPromote", testHITLBrainstormPromoteStub)

	if realClaudeOn {
		t.Run("Autonomous_RealClaude", testAutonomousRealClaude)
		t.Run("HITL_RealClaude", testHITLRealClaude)
		t.Run("HITL_BrainstormPromote_RealClaude", testHITLBrainstormPromoteRealClaude)
	}
}

func testAutonomousStub(t *testing.T) {
	start := time.Now()

	defer func() {
		recordSummary(summaryRow{
			mode:      "Stub",
			scenario:  "Autonomous",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
		})
	}()

	s := bootScenario(t, "auto", "intauto", false)

	cardID := s.createCard(t, "Stub: autonomous canary", true /*autonomous*/)
	s.triggerRun(t, cardID, false /*interactive*/)

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if card.AssignedAgent != "" {
		t.Errorf("assigned_agent: got %q, want empty (released)", card.AssignedAgent)
	}

	if card.RunnerStatus != "" {
		t.Errorf("runner_status: got %q, want empty (cleared on terminal)", card.RunnerStatus)
	}

	// Phase markers from FSM actions.go (Task 1 added these).
	expected := []string{"plan", "subtasks", "execute", "document", "review", "commit"}

	got := phaseMarkers(card)
	if !equalStrings(got, expected) {
		t.Errorf("phase markers:\n got: %v\nwant: %v", got, expected)
	}

	// Runner stderr shows FSM transition log lines.
	if !s.runner.hasLine("Initializing") {
		t.Errorf("runner stderr missing FSM Initializing entry\nstderr tail:\n%s",
			tail(s.runner.stderr.String(), 30))
	}

	// `Completing` is the terminal FSM state for autonomous.
	if !s.runner.hasLine("Completing") {
		t.Errorf("runner stderr missing FSM Completing entry\nstderr tail:\n%s",
			tail(s.runner.stderr.String(), 30))
	}
}

// testHITLStub drives the HITL chat-loop happy-path with the stub fake
// claude. Triggers a non-autonomous card, sends "approve" via /message
// to terminate the plan chat-loop, sends any chat to clear the
// execution-start gate, then sends "approve" again to terminate the
// review chat-loop. Asserts the FSM walks plan → subtasks → execute →
// document → review → commit → done.
func testHITLStub(t *testing.T) {
	start := time.Now()

	defer func() {
		recordSummary(summaryRow{
			mode:      "Stub",
			scenario:  "HITL",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
		})
	}()

	s := bootScenario(t, "hitl", "inthitl", false)

	cardID := s.createCard(t, "Stub: HITL canary", false /*autonomous*/)
	s.triggerRun(t, cardID, true /*interactive*/)

	// Wait for the runner to enter the planning chat-loop, then for the
	// agent's first-turn proposal to land. The runner emits "phase=plan"
	// when it spawns the chat session and "phase=plan_awaiting" after the
	// first non-terminal turn — that's the cue for the human to reply.
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "plan_awaiting", stubScenarioTimeout)
	s.messageCard(t, cardID, "looks good, approve the plan")

	// Plan approval flows directly into Executing — no second gate
	// between subtask creation and execute.

	// Drive review chat-loop after document phase completes; same
	// agent-first protocol — wait for review_awaiting before approving.
	s.waitForPhase(t, cardID, "review", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "review_awaiting", stubScenarioTimeout)
	s.messageCard(t, cardID, "approve")

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if card.AssignedAgent != "" {
		t.Errorf("assigned_agent: got %q, want empty (released)", card.AssignedAgent)
	}

	expected := []string{"plan", "plan_awaiting", "subtasks", "execute", "document", "review", "review_awaiting", "commit"}

	got := phaseMarkers(card)
	if !equalStrings(got, expected) {
		t.Errorf("phase markers:\n got: %v\nwant: %v", got, expected)
	}
}

// testHITLReviewLoopStub drives a two-round review loop in HITL mode.
// Round 1 plans, executes, then sends "please revise" to trigger
// review_revise; round 2 replans, executes, and approves. Asserts the
// FSM walked through replan and review fired twice via the activity
// log's phase markers. RevisionAttempts itself is plumbed end-to-end
// (mcp/tools.go: updateCardInput → service/service_cards.go) but this
// test stays focused on the FSM transitions; a separate unit test in
// contextmatrix-runner/internal/orchestrator covers the IncrementRevisionAttemptsAction.
func testHITLReviewLoopStub(t *testing.T) {
	start := time.Now()

	defer func() {
		recordSummary(summaryRow{
			mode:      "Stub",
			scenario:  "HITL_ReviewLoop",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
		})
	}()

	s := bootScenario(t, "hitl-rev", "inthitlrev", false)

	cardID := s.createCard(t, "Stub: HITL review loop", false /*autonomous*/)
	s.triggerRun(t, cardID, true /*interactive*/)

	// Round 1: plan → execute → review (revise). Wait for the agent's
	// first-turn proposal in each chat loop before sending the human
	// reply (plan_awaiting / review_awaiting markers).
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "plan_awaiting", stubScenarioTimeout)
	s.messageCard(t, cardID, "approve the plan")
	s.waitForPhase(t, cardID, "review", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "review_awaiting", stubScenarioTimeout)
	s.messageCard(t, cardID, "please revise: tighten the test coverage")

	// Round 2: replan → execute → review (approve). The replan path
	// reuses runChatLoop with phase="plan", so the awaiting marker is
	// "plan_awaiting" (re-entered, hence waitForPhaseN).
	s.waitForPhase(t, cardID, "replan", stubScenarioTimeout)
	s.waitForPhaseN(t, cardID, "plan_awaiting", 2, stubScenarioTimeout)
	s.messageCard(t, cardID, "approve the revised plan")
	s.waitForPhaseN(t, cardID, "review", 2, stubScenarioTimeout)
	s.waitForPhaseN(t, cardID, "review_awaiting", 2, stubScenarioTimeout)
	s.messageCard(t, cardID, "approve")

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	// Activity-log proof of the revise path: two phase=review entries
	// (round 1 + round 2) and one phase=replan entry (after round 1's
	// revise verdict).
	reviewCount := 0
	replanCount := 0

	for _, e := range card.ActivityLog {
		if e.Action != "phase" {
			continue
		}

		switch e.Message {
		case "review":
			reviewCount++
		case "replan":
			replanCount++
		}
	}

	if reviewCount != 2 {
		t.Errorf("phase=review entries: got %d, want 2", reviewCount)
	}

	if replanCount != 1 {
		t.Errorf("phase=replan entries: got %d, want 1", replanCount)
	}
}

// testHITLPromoteStub validates the promotion path: a HITL card is
// triggered, plan session opens, /promote is called mid-run, and the
// FSM finishes autonomously. The driver pushes the canned promotion
// chat into ChatInputCh; the stub recognises the canned text and
// terminates the plan chat-loop with plan_complete. Post-promotion the
// FSM completes via ephemeral subprocesses (the post-plan execution
// gate has been removed entirely; both modes go straight to Executing
// after CreatingSubtasks).
func testHITLPromoteStub(t *testing.T) {
	start := time.Now()

	defer func() {
		recordSummary(summaryRow{
			mode:      "Stub",
			scenario:  "HITL_Promote",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
		})
	}()

	s := bootScenario(t, "hitl-promote", "inthitlpromote", false)

	cardID := s.createCard(t, "Stub: HITL promote mid-run", false /*autonomous*/)
	s.triggerRun(t, cardID, true /*interactive*/)

	// Wait for the plan chat session to open and the agent's first-turn
	// proposal to land (plan_awaiting), then promote. The driver sees
	// the SSE promotion event, flips Mode to autonomous, and pushes the
	// canned promotionChatMessage into ChatInputCh. The stub reads that
	// as the next stream-json user turn and emits plan_complete.
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "plan_awaiting", stubScenarioTimeout)
	s.promoteCard(t, cardID)

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if !card.Autonomous {
		t.Errorf("card.Autonomous: got %v, want true (after promotion)", card.Autonomous)
	}

	// Regression guard: the post-plan gate has been removed entirely,
	// so no `wait_execution_start` activity entry should ever appear,
	// regardless of mode. If this fails, someone has re-introduced
	// the gate.
	for _, e := range card.ActivityLog {
		if e.Action == "phase" && e.Message == "wait_execution_start" {
			t.Errorf("activity log has phase=wait_execution_start; gate has been removed and should never fire")

			break
		}
	}
}

// testHITLBrainstormPromoteStub validates the regression covered by the
// chat-loop drain fix: when /promote fires DURING an in-flight
// brainstorming turn, the driver flips Mode to autonomous AND buffers
// the canned promotion chat into ChatInputCh; the loop must drain that
// buffered message and run ONE more turn so the agent can react per the
// brainstorm.md "Promotion mid-dialogue" handler (write the synthesized
// `## Design` via update_card, then emit discovery_complete).
//
// Before the fix, the loop returned ErrPromoted on the post-turn Mode
// check, dropping the buffered message; the FSM advanced to Planning
// with no `## Design` written. This test creates a feature-type card
// (so NeedsBrainstormGuard fires), waits for `brainstorm_awaiting` (the
// chat-loop's first turn ended with no marker, parking the agent), then
// promotes. The stub agent recognises the promotion text on its drain
// turn and emits update_card+discovery_complete; the FSM finishes
// autonomously. Asserts: the card body contains a `## Design` section,
// `discovery_complete: true` is stamped, and the card reaches `done`.
func testHITLBrainstormPromoteStub(t *testing.T) {
	start := time.Now()

	defer func() {
		recordSummary(summaryRow{
			mode:      "Stub",
			scenario:  "HITL_BrainstormPromote",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
		})
	}()

	s := bootScenario(t, "hitl-brainstorm-promote", "intbrainpromote", false)

	// type=feature trips NeedsBrainstormGuard so the FSM enters the
	// Brainstorming state instead of routing straight to Planning.
	cardID := s.createCardOfType(t, "Stub: HITL brainstorm promote mid-run", "feature", false /*autonomous*/)
	s.triggerRun(t, cardID, true /*interactive*/)

	// Wait for the brainstorm chat-loop to open and the stub's first-turn
	// proposal to land. brainstorm_awaiting is the activity-log entry
	// the chat-loop emits when a turn ended without a terminal marker.
	s.waitForPhase(t, cardID, "brainstorm", stubScenarioTimeout)
	s.waitForPhase(t, cardID, "brainstorm_awaiting", stubScenarioTimeout)

	// Promote. The driver flips Mode to autonomous and pushes the
	// canned promotionChatMessage into ChatInputCh. The loop's drain
	// branch consumes the buffered message and runs one more turn;
	// the stub recognises the promotion text and emits
	// update_card + discovery_complete.
	s.promoteCard(t, cardID)

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if !card.Autonomous {
		t.Errorf("card.Autonomous: got %v, want true (after promotion)", card.Autonomous)
	}

	if !card.DiscoveryComplete {
		t.Errorf("card.DiscoveryComplete: got %v, want true (brainstorm completed via drain turn)", card.DiscoveryComplete)
	}

	// Pin the regression: the activity log must contain a
	// `discovery_complete` entry produced by the brainstorm action's
	// onTerminalMarker callback when the agent emits the marker on
	// the drain turn. The ErrPromoted fallback path does NOT call
	// add_log — it only stamps `discovery_complete: true` directly.
	// So presence of this entry proves the drain branch ran one more
	// turn AND the agent reacted to the buffered promotion message.
	// Before the chat-loop fix, the loop returned ErrPromoted on the
	// post-turn Mode check, the agent never saw the promotion text,
	// and this entry never fired.
	hasDiscoveryLog := false

	for _, e := range card.ActivityLog {
		if e.Action == "discovery_complete" {
			hasDiscoveryLog = true

			break
		}
	}

	if !hasDiscoveryLog {
		t.Errorf("activity log missing `discovery_complete` entry; promotion drain did not run a turn that emitted the marker\nactivity log:\n%+v",
			card.ActivityLog)
	}
}

func testAutonomousRealClaude(t *testing.T) {
	start := time.Now()

	var frictionN int

	defer func() {
		recordSummary(summaryRow{
			mode:      "Real-Claude",
			scenario:  "Autonomous",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
			costNote:  "~haiku usage",
			frictionN: frictionN,
		})
	}()

	s := bootScenario(t, "auto-real", "intautoreal", true /*realClaude*/)
	// bootScenario provisions the fixture and bakes its URL into
	// .board.yaml at boot time (CM's PUT path doesn't reconcile
	// cfg.Repos with cfg.Repo, so retargeting after boot leaves the
	// runner's MCP-fetched registry stale).

	canaryUUID := randomHex(t, 4)
	canaryPort := canaryServerPort()
	cardTitle := fmt.Sprintf("Real-Claude canary: SYSINFO-%s", canaryUUID)
	cardBody := canaryCardBody(canaryUUID, canaryPort)

	cardID := s.createCard(t, cardTitle, true /*autonomous*/)
	// Set the body via PATCH so the agent reads the canary instructions.
	{
		body := map[string]any{"body": cardBody}
		path := fmt.Sprintf("/api/projects/%s/cards/%s", s.project, cardID)

		status, raw := s.client.patchRaw(t, path, body, nil)
		if status != http.StatusOK {
			t.Fatalf("patch card body: HTTP %d body=%s", status, raw)
		}
	}

	transcriptBuf := newTranscriptBuffer(5 * 1024 * 1024)
	s.tb = transcriptBuf
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf, s.rl)

	s.triggerRun(t, cardID, false /*interactive*/)

	card := s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	assertCanaryServer(t, s, canaryUUID, canaryPort)
	assertSkillEngaged(t, s, cardID, canarySkillName)

	// review_attempts counts REVISION rounds (incremented by
	// IncrementRevisionAttempts on a revise verdict), not the number
	// of reviews performed. A first-pass approve leaves it at 0.
	// HITL review-loop scenarios exercise the > 0 case.
	if card.ReviewAttempts != 0 {
		t.Errorf("review_attempts: got %d, want 0 (canary should approve on first pass)", card.ReviewAttempts)
	}

	// Always save the transcript to disk — the controlling Claude Code
	// session reads it inline to produce the friction report by default.
	// CM_FRICTION_ANALYZER=1 additionally runs an in-test Haiku-powered
	// analyser for unattended use; both paths are informational and
	// don't fail the test.
	events := transcriptBuf.snapshot()
	transcriptPath := saveTranscript(t, events, "Autonomous_RealClaude")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := analyzeTranscript(ctx, t, events)
	if err != nil {
		t.Logf("friction analyzer failed: %v", err)
	}

	if report != nil {
		frictionN = len(report.Findings)
	}

	printFrictionReport("Autonomous_RealClaude", transcriptPath, report)
}

// testHITLRealClaude drives the same sysinfo-server canary as the
// autonomous scenario but through the HITL chat-loop. Sends terse,
// directive messages so real Claude calls plan_complete and
// review_approve in a single turn each, then runs the same
// build/test/curl assertions via assertCanaryServer.
func testHITLRealClaude(t *testing.T) {
	start := time.Now()

	var frictionN int

	defer func() {
		recordSummary(summaryRow{
			mode:      "Real-Claude",
			scenario:  "HITL",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
			costNote:  "~haiku usage",
			frictionN: frictionN,
		})
	}()

	s := bootScenario(t, "hitl-real", "inthitlreal", true /*realClaude*/)

	canaryUUID := randomHex(t, 4)
	canaryPort := canaryServerPort()
	cardTitle := fmt.Sprintf("Real-Claude HITL canary: SYSINFO-%s", canaryUUID)
	cardBody := canaryCardBody(canaryUUID, canaryPort)

	cardID := s.createCard(t, cardTitle, false /*autonomous=false → HITL*/)
	{
		body := map[string]any{"body": cardBody}
		path := fmt.Sprintf("/api/projects/%s/cards/%s", s.project, cardID)

		status, raw := s.client.patchRaw(t, path, body, nil)
		if status != http.StatusOK {
			t.Fatalf("patch card body: HTTP %d body=%s", status, raw)
		}
	}

	transcriptBuf := newTranscriptBuffer(5 * 1024 * 1024)
	s.tb = transcriptBuf
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf, s.rl)

	s.triggerRun(t, cardID, true /*interactive*/)

	// Agent-first HITL flow: claude reads the card body on the first
	// turn, drafts the plan, writes ## Plan to the card via update_card,
	// and proposes in chat. The runner emits "phase=plan_awaiting"; the
	// human only types after seeing the proposal. Same shape on review.
	// Each chat message is a naturalistic free-text approval — no tool
	// names, no plan content. If claude calls plan_complete on the first
	// turn or never proposes, that's a product bug to surface.
	s.waitForPhase(t, cardID, "plan", realClaudeScenarioTimeout)
	s.waitForPhase(t, cardID, "plan_awaiting", realClaudeScenarioTimeout)
	s.messageCard(t, cardID, "lgtm, ship it")

	// Plan approval flows directly into Executing — no second gate.

	s.waitForPhase(t, cardID, "review", realClaudeScenarioTimeout)
	s.waitForPhase(t, cardID, "review_awaiting", realClaudeScenarioTimeout)
	s.messageCard(t, cardID, "looks good, ship it")

	_ = s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	assertCanaryServer(t, s, canaryUUID, canaryPort)
	assertSkillEngaged(t, s, cardID, canarySkillName)

	events := transcriptBuf.snapshot()
	transcriptPath := saveTranscript(t, events, "HITL_RealClaude")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := analyzeTranscript(ctx, t, events)
	if err != nil {
		t.Logf("friction analyzer failed: %v", err)
	}

	if report != nil {
		frictionN = len(report.Findings)
	}

	printFrictionReport("HITL_RealClaude", transcriptPath, report)
}

// testHITLBrainstormPromoteRealClaude exercises the full end-to-end
// promote-mid-brainstorm path with real Claude and real MCP routing —
// the path stub-mode integration cannot validate because the stub
// agent never actually invokes the contextmatrix MCP server when it
// emits `update_card` tool_use stream events.
//
// Reproduces the user-reported scenario: a HITL feature card enters
// brainstorming, the human exchanges a partial dialogue, then clicks
// Promote before the design is confirmed. The runner's chat-loop must
// drain the buffered promotionChatMessage and run one more brainstorm
// turn so the agent reads the trigger and follows the brainstorm.md
// "Promotion mid-dialogue" handler — synthesize the design, call
// `update_card` to write the `## Design` section to the card body,
// then call `discovery_complete`. This is the ONLY scenario where the
// body mutation is exercised end-to-end (real Claude, real MCP).
//
// The card body is the canary spec so we can also verify that the
// captured design is solid enough for autonomous planning + execution
// to produce a working sysinfo server downstream — the strongest
// signal that the brainstorm-to-autonomous handoff actually preserves
// the design contract.
func testHITLBrainstormPromoteRealClaude(t *testing.T) {
	start := time.Now()

	var frictionN int

	defer func() {
		recordSummary(summaryRow{
			mode:      "Real-Claude",
			scenario:  "HITL_BrainstormPromote",
			pass:      !t.Failed() && !t.Skipped(),
			skipped:   t.Skipped(),
			durationS: time.Since(start).Truncate(100 * time.Millisecond).String(),
			costNote:  "~haiku-opus mix; brainstorm + plan + execute + review",
			frictionN: frictionN,
		})
	}()

	s := bootScenario(t, "hitl-bp-real", "intbpromreal", true /*realClaude*/)

	canaryUUID := randomHex(t, 4)
	canaryPort := canaryServerPort()
	cardTitle := fmt.Sprintf("Real-Claude HITL brainstorm-promote canary: SYSINFO-%s", canaryUUID)
	cardBody := canaryCardBody(canaryUUID, canaryPort)

	// type=feature trips NeedsBrainstormGuard so the FSM enters the
	// Brainstorming state instead of routing straight to Planning.
	cardID := s.createCardOfType(t, cardTitle, "feature", false /*autonomous=false → HITL*/)
	{
		body := map[string]any{"body": cardBody}
		path := fmt.Sprintf("/api/projects/%s/cards/%s", s.project, cardID)

		status, raw := s.client.patchRaw(t, path, body, nil)
		if status != http.StatusOK {
			t.Fatalf("patch card body: HTTP %d body=%s", status, raw)
		}
	}

	transcriptBuf := newTranscriptBuffer(5 * 1024 * 1024)
	s.tb = transcriptBuf
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf, s.rl)

	s.triggerRun(t, cardID, true /*interactive*/)

	// Brainstorm chat-loop opens; agent reads the card body and asks a
	// clarifying question (or proposes a design). brainstorm_awaiting
	// fires when the first turn ended without a terminal marker.
	s.waitForPhase(t, cardID, "brainstorm", realClaudeScenarioTimeout)
	s.waitForPhase(t, cardID, "brainstorm_awaiting", realClaudeScenarioTimeout)

	// Send one terse reply to give the agent concrete context to capture
	// into the synthesized `## Design` when promotion fires. Naturalistic
	// free-text — no tool names, no design content — so a real model
	// has to interpret intent like a human user.
	s.messageCard(t, cardID, "stdlib only, single main.go, basic httptest")

	// Wait for the agent's response to the reply to land
	// (brainstorm_awaiting count=2). At this point the dialogue has had
	// one real exchange but the design is NOT yet confirmed — exactly
	// the racy scenario the user reported. Promote now.
	s.waitForPhaseN(t, cardID, "brainstorm_awaiting", 2, realClaudeScenarioTimeout)
	s.promoteCard(t, cardID)

	card := s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	// Pin the post-promotion state.
	if !card.Autonomous {
		t.Errorf("card.Autonomous: got %v, want true (after promotion)", card.Autonomous)
	}

	if !card.DiscoveryComplete {
		t.Errorf("card.DiscoveryComplete: got %v, want true (brainstorm completed via promotion)", card.DiscoveryComplete)
	}

	// 100% accuracy assertion: the agent must have written `## Design`
	// to the card body via update_card before emitting discovery_complete.
	// This is the body mutation that stub-mode integration cannot exercise
	// because the stub never invokes the real MCP server. If this fails,
	// the brainstorm.md "Promotion mid-dialogue" handler did not engage
	// or the chat-loop drain branch dropped the buffered promotion msg.
	if !strings.Contains(card.Body, "## Design") {
		t.Errorf("card body missing `## Design` section after promotion mid-brainstorm; the agent did not capture the design before terminating\nbody:\n%s",
			tail(card.Body, 80))
	}

	// End-to-end signal: the autonomous downstream phases (plan + execute
	// + review + commit) must produce a working sysinfo server using the
	// captured design. If the design wasn't preserved, the plan agent
	// would draft empty subtasks and execution would be a no-op — the
	// canary verification would then fail at the assertCanaryServer step.
	assertCanaryServer(t, s, canaryUUID, canaryPort)
	assertSkillEngaged(t, s, cardID, canarySkillName)

	// Friction analyzer (informational, like the other real-Claude
	// scenarios). Doesn't fail the test.
	events := transcriptBuf.snapshot()
	transcriptPath := saveTranscript(t, events, "HITL_BrainstormPromote_RealClaude")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := analyzeTranscript(ctx, t, events)
	if err != nil {
		t.Logf("friction analyzer failed: %v", err)
	}

	if report != nil {
		frictionN = len(report.Findings)
	}

	printFrictionReport("HITL_BrainstormPromote_RealClaude", transcriptPath, report)
}

// canaryServerPort picks a high port unlikely to collide with other
// services on the host. Re-rolls per scenario via the test rand source.
func canaryServerPort() int {
	return 18080 + rand.Intn(2000)
}

// canaryCardBody renders the prompt body for the sysinfo-server canary.
// Shared by autonomous and HITL real-Claude scenarios so both run the
// same task and the assertCanaryServer helper applies to both.
func canaryCardBody(canaryUUID string, canaryPort int) string {
	return fmt.Sprintf(`Implement a Go HTTP server, stdlib only (no go.mod entries beyond the standard library), in main.go that:

- Listens on port %d.
- Responds to GET / with a JSON body containing sysinfo about the host:
  hostname, goos, goarch, num_cpu, go_version. Field names exactly as
  written here (lowercase snake_case).
- Responds 405 to any other HTTP method.
- Includes a basic test in main_test.go using net/http/httptest that
  asserts the GET / response status, content-type application/json,
  and that all five sysinfo fields are present in the response body.

Add a leading comment line "// SYSINFO-CANARY-%s" at the very top of
main.go (before the package declaration is fine; the static check just
greps for the exact string). Commit and push.

## Required: engage harness-canary-skill before writing code

This card is a SYSINFO-CANARY task. Before you touch any code, invoke the
Skill tool with skill="harness-canary-skill" once. The skill is mounted
into ~/.claude/skills/harness-canary-skill/ by the harness and contains
required guidance for this task.`, canaryPort, canaryUUID)
}

// assertCanaryServer drives the post-run canary verification: locate
// the branch in the bare fixture repo that contains main.go with the
// canary marker, clone it, run static checks for stdlib import
// discipline, then `go build` + `go test` + a runtime smoke test
// (HTTP GET / and JSON shape validation). Shared between the
// autonomous and HITL real-Claude scenarios.
func assertCanaryServer(t *testing.T, s *scenarioCtx, canaryUUID string, canaryPort int) {
	t.Helper()

	bareRepo := filepath.Join(s.cfg.tmpDir, "fixture.git")
	canaryMarker := "SYSINFO-CANARY-" + canaryUUID

	// Find the branch in the bare repo whose tip's main.go contains the
	// canary marker. The orchestrator pushes work to feature branches —
	// we don't assume any particular naming.
	refsRaw := mustOutput(t, s.cfg.tmpDir, "git", "-C", bareRepo,
		"for-each-ref", "--format=%(refname)", "refs/heads")

	var (
		canaryBranch string
		mainGo       string
	)

	for _, ref := range strings.Split(strings.TrimSpace(refsRaw), "\n") {
		if ref == "" {
			continue
		}

		out, err := exec.Command("git", "-C", bareRepo, "show", ref+":main.go").CombinedOutput()
		if err != nil {
			continue
		}

		if strings.Contains(string(out), canaryMarker) {
			canaryBranch = ref
			mainGo = string(out)

			break
		}
	}

	if canaryBranch == "" {
		oneliners := mustOutput(t, s.cfg.tmpDir, "git", "-C", bareRepo,
			"log", "--all", "--oneline")
		t.Fatalf("no branch in fixture has main.go containing %q\ngit log --all:\n%s",
			canaryMarker, oneliners)
	}

	// Static checks on main.go content. Catch the most common
	// shortcuts (skipping JSON, non-stdlib imports).
	for _, want := range []string{"net/http", "encoding/json", "runtime."} {
		if !strings.Contains(mainGo, want) {
			t.Errorf("main.go missing %q (canary expects stdlib http+json+runtime)", want)
		}
	}

	if strings.Contains(mainGo, `"github.com/`) {
		t.Errorf("main.go imports a third-party package; canary requires stdlib only")
	}

	// Clone the canary branch into a fresh working tree.
	workDir := t.TempDir()
	checkout := filepath.Join(workDir, "checkout")
	branchName := strings.TrimPrefix(canaryBranch, "refs/heads/")
	mustRun(t, workDir, "git", "clone", "--branch", branchName, bareRepo, "checkout")

	// Verify main_test.go exists — `go test ./...` returns 0 even when
	// no tests are present, which would otherwise let an agent skip the
	// test entirely.
	if _, err := os.Stat(filepath.Join(checkout, "main_test.go")); err != nil {
		t.Fatalf("main_test.go not in checkout: %v", err)
	}

	binPath := filepath.Join(workDir, "canary")

	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = checkout

	if buildOut, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("canary build failed: %v\n%s", err, buildOut)
	}

	testCmd := exec.Command("go", "test", "./...")
	testCmd.Dir = checkout

	if testOut, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("canary tests failed: %v\n%s", err, testOut)
	}

	// Run the binary briefly and curl the /. Use a context-bounded
	// CommandContext so the server is killed deterministically when
	// the test finishes.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	srv := exec.CommandContext(ctx, binPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("start canary server: %v", err)
	}

	defer func() {
		if srv.Process != nil {
			_ = srv.Process.Kill()
		}
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/", canaryPort)

	var (
		body       []byte
		statusCode int
	)

	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)

		resp, err := http.Get(url)
		if err != nil {
			continue
		}

		body, _ = io.ReadAll(resp.Body)
		statusCode = resp.StatusCode
		_ = resp.Body.Close()

		if statusCode == http.StatusOK {
			break
		}
	}

	if len(body) == 0 {
		t.Fatalf("canary server never answered on :%d (last status=%d)", canaryPort, statusCode)
	}

	if statusCode != http.StatusOK {
		t.Fatalf("canary GET / status: got %d, want 200\nbody=%s", statusCode, body)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("canary response not JSON: %v\nbody=%s", err, body)
	}

	for _, field := range []string{"hostname", "goos", "goarch", "num_cpu", "go_version"} {
		if _, ok := got[field]; !ok {
			t.Errorf("canary response missing field %q\nbody=%s", field, body)
		}
	}
}

// assertSkillEngaged verifies the card's activity log contains a
// skill_engaged entry for the given skill name. The runner's dispatcher
// emits this whenever the agent invokes the Skill tool with that name —
// either directly via Claude Code's filesystem-skill mechanism, or via
// the agent calling mcp__contextmatrix__add_log with action=skill_engaged.
// Either route flows through CardService.RecordSkillEngaged on CM, which
// dedupes within a 60s window, so a single entry is the expected shape.
func assertSkillEngaged(t *testing.T, s *scenarioCtx, cardID, skillName string) {
	t.Helper()

	var got struct {
		ActivityLog []struct {
			Agent   string `json:"agent"`
			Action  string `json:"action"`
			Message string `json:"message"`
			Skill   string `json:"skill"`
		} `json:"activity_log"`
	}

	path := fmt.Sprintf("/api/projects/%s/cards/%s", s.project, cardID)
	if status := s.client.get(t, path, &got); status != http.StatusOK {
		t.Fatalf("get card for skill assertion: HTTP %d", status)
	}

	for _, entry := range got.ActivityLog {
		if entry.Action == "skill_engaged" && entry.Skill == skillName {
			return
		}
	}

	var actions []string
	for _, e := range got.ActivityLog {
		actions = append(actions, fmt.Sprintf("%s/%s", e.Action, e.Skill))
	}

	t.Errorf("activity log missing skill_engaged for %q\nentries: %v",
		skillName, actions)
}

// mustOutput runs cmd in dir and returns stdout, fatals on error.
func mustOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run %s %v: %v", name, args, err)
	}

	return string(out)
}

// repoFromURL strips the file:// prefix.
func repoFromURL(url string) string {
	return strings.TrimPrefix(url, "file://")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
