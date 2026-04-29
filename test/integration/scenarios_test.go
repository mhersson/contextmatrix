//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
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

	if realClaudeOn {
		t.Run("Autonomous_RealClaude", testAutonomousRealClaude)
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

	// Wait for the runner to enter the planning chat-loop. The orchestrator
	// emits a "phase=plan" activity log entry just before opening the
	// session; once that lands, sending a chat input is safe.
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)

	// Drive plan chat-loop: the stub replies once to the primer, then waits
	// for our "approve" to fire plan_complete.
	s.messageCard(t, cardID, "looks good, approve the plan")

	// Drive execution-start gate: any chat lets the FSM proceed past
	// WaitingForExecutionStart into Executing.
	s.waitForPhase(t, cardID, "wait_execution_start", stubScenarioTimeout)
	s.messageCard(t, cardID, "go")

	// Drive review chat-loop after document phase completes.
	s.waitForPhase(t, cardID, "review", stubScenarioTimeout)
	s.messageCard(t, cardID, "approve")

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if card.AssignedAgent != "" {
		t.Errorf("assigned_agent: got %q, want empty (released)", card.AssignedAgent)
	}

	expected := []string{"plan", "subtasks", "wait_execution_start", "execute", "document", "review", "commit"}

	got := phaseMarkers(card)
	if !equalStrings(got, expected) {
		t.Errorf("phase markers:\n got: %v\nwant: %v", got, expected)
	}
}

// testHITLReviewLoopStub drives a two-round review loop in HITL mode.
// Round 1 plans, executes, then sends "please revise" to trigger
// review_revise; round 2 replans, executes, and approves. Asserts the
// FSM walked through replan and review fired twice.
//
// We don't assert the card's RevisionAttempts/ReviewAttempts counter:
// the runner's IncrementRevisionAttemptsAction pushes the new value
// via update_card with a "revision_attempts" key, but CM's update_card
// tool only forwards a fixed set of mutable fields (Title, Priority,
// Labels, Skills, Body) and silently drops unknown fields. The
// in-memory counter on the runner side is still correct (the
// autonomous max-revision-attempts guard uses it), so this is a
// data-observability gap rather than a correctness bug — to be wired
// up in a follow-up. Activity-log entries are the load-bearing
// signal for now.
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

	// Round 1: plan → execute → review (revise).
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)
	s.messageCard(t, cardID, "approve the plan")
	s.waitForPhase(t, cardID, "wait_execution_start", stubScenarioTimeout)
	s.messageCard(t, cardID, "go")
	s.waitForPhase(t, cardID, "review", stubScenarioTimeout)
	s.messageCard(t, cardID, "please revise: tighten the test coverage")

	// Round 2: replan → execute → review (approve). wait_execution_start
	// and review are re-entered, so wait for the second occurrence.
	s.waitForPhase(t, cardID, "replan", stubScenarioTimeout)
	s.messageCard(t, cardID, "approve the revised plan")
	s.waitForPhaseN(t, cardID, "wait_execution_start", 2, stubScenarioTimeout)
	s.messageCard(t, cardID, "go")
	s.waitForPhaseN(t, cardID, "review", 2, stubScenarioTimeout)
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
	cardTitle := fmt.Sprintf("Real-Claude canary: TEST-MARKER-%s", canaryUUID)
	cardBody := fmt.Sprintf("Append a single line `<!-- TEST-MARKER-%s -->` to the end of README.md, then commit and push. Do not perform any other work.", canaryUUID)

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
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf)

	s.triggerRun(t, cardID, false /*interactive*/)

	card := s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	// Assert the bare repo received the canary commit. We check the
	// actual diff content (the README.md line addition), not the commit
	// message — agents vary the commit subject and the marker uuid is
	// not guaranteed to appear there. `git log -p --all` shows the
	// patches across all branches; grep for the marker line in the diff.
	bareRepo := filepath.Join(s.cfg.tmpDir, "fixture.git")
	patches := mustOutput(t, s.cfg.tmpDir, "git", "-C", bareRepo,
		"log", "-p", "--all", "--no-color")
	markerLine := "TEST-MARKER-" + canaryUUID
	if !strings.Contains(patches, markerLine) {
		// Print the commit graph + recent log for diagnostics.
		oneliners := mustOutput(t, s.cfg.tmpDir, "git", "-C", bareRepo,
			"log", "--all", "--oneline")
		t.Errorf("fixture bare repo never received the %s diff line\ngit log --all:\n%s", markerLine, oneliners)
	}

	// review_attempts counts REVISION rounds (incremented by
	// IncrementRevisionAttempts on a revise verdict), not the number
	// of reviews performed. A first-pass approve leaves it at 0.
	// HITL review-loop scenarios in Plan 2 will exercise the > 0 case.
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
