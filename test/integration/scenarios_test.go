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

	if realClaudeOn {
		t.Run("Autonomous_RealClaude", testAutonomousRealClaude)
		t.Run("HITL_RealClaude", testHITLRealClaude)
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

// testHITLPromoteStub validates the promotion path: a HITL card is
// triggered, plan session opens, /promote is called mid-run, and the
// FSM finishes autonomously. The driver pushes the canned promotion
// chat into ChatInputCh; the stub recognises the canned text and
// terminates the plan chat-loop with plan_complete. Post-promotion the
// FSM should bypass WaitForExecutionStart (IsHITL guard is now false)
// and complete via ephemeral subprocesses.
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

	// Wait for the plan chat session to open, then promote. The driver
	// sees the SSE promotion event, flips Mode to autonomous, and pushes
	// the canned promotionChatMessage into ChatInputCh. The stub reads
	// that as the next stream-json user turn and emits plan_complete.
	s.waitForPhase(t, cardID, "plan", stubScenarioTimeout)
	s.promoteCard(t, cardID)

	card := s.waitForState(t, cardID, "done", stubScenarioTimeout)

	if !card.Autonomous {
		t.Errorf("card.Autonomous: got %v, want true (after promotion)", card.Autonomous)
	}

	// Post-promotion IsHITL() returns false, so the FSM's
	// CreatingSubtasks → WaitingForExecutionStart guard should fail and
	// the FSM should drop straight into Executing. No phase=wait_execution_start
	// entries should appear in the activity log.
	for _, e := range card.ActivityLog {
		if e.Action == "phase" && e.Message == "wait_execution_start" {
			t.Errorf("activity log has phase=wait_execution_start; FSM should bypass after promotion")

			break
		}
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
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf)

	s.triggerRun(t, cardID, false /*interactive*/)

	card := s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	assertCanaryServer(t, s, canaryUUID, canaryPort)

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
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, transcriptBuf)

	s.triggerRun(t, cardID, true /*interactive*/)

	// Drive each chat-loop with a single terse, directive message. Real
	// Claude is fast to comply when the wording is unambiguous; longer
	// phrasing risks follow-up questions and a timeout.
	s.waitForPhase(t, cardID, "plan", realClaudeScenarioTimeout)
	s.messageCard(t, cardID,
		"Plan one subtask: 'Implement main.go and main_test.go per "+
			"the card body, commit, push'. Approve and call plan_complete now.")

	s.waitForPhase(t, cardID, "wait_execution_start", realClaudeScenarioTimeout)
	s.messageCard(t, cardID, "go")

	s.waitForPhase(t, cardID, "review", realClaudeScenarioTimeout)
	s.messageCard(t, cardID,
		"The diff implements the spec correctly. Approve and call review_approve now.")

	_ = s.waitForState(t, cardID, "done", realClaudeScenarioTimeout)

	assertCanaryServer(t, s, canaryUUID, canaryPort)

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
greps for the exact string). Commit and push.`, canaryPort, canaryUUID)
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
