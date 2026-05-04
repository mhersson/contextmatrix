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

// TestIntegrationHarness is the entry point for all stub scenarios.
// Real-Claude variants are added in later tasks under realClaudeOn gating.
func TestIntegrationHarness(t *testing.T) {
	t.Run("Autonomous", testAutonomousStub)
	t.Run("HITL", testHITLStub)
	t.Run("KillMidRun", testKillMidRunStub)
	t.Run("HeartbeatTimeout", testHeartbeatTimeoutStub)
	t.Run("PromoteHITLToAuto", testPromoteHITLToAutoStub)
	t.Run("IdleWatchdog", testIdleWatchdogStub)

	if realClaudeOn {
		t.Run("Autonomous_RealClaude", testAutonomousRealClaude)
		t.Run("HITL_RealClaude", testHITLRealClaude)
	}
}

func testAutonomousStub(t *testing.T) {
	scenarioID := "autonomous"
	project := "harness"

	s := bootScenario(t, scenarioID, project, false /* realClaude */)

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

	s := bootScenario(t, scenarioID, project, false /* realClaude */)

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

	s := bootScenario(t, scenarioID, project, false /* realClaude */)

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

	s := bootScenarioWithConfig(t, scenarioID, project, false, func(cfg *scenarioConfig) {
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

	s := bootScenario(t, scenarioID, project, false)

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

	s := bootScenarioWithConfig(t, scenarioID, project, false, func(cfg *scenarioConfig) {
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

func testAutonomousRealClaude(t *testing.T) {
	scenarioID := "autonomous-real"
	project := "harness"

	s := bootScenario(t, scenarioID, project, true /* realClaude */)

	canaryUUID := randomHex(t, 4)
	canaryPort := canaryServerPort()

	cardTitle := "Real-Claude canary"
	cardBody := canaryCardBody(canaryUUID, canaryPort)

	// autonomous=true exercises the production run-autonomous workflow
	// end-to-end: claim → plan → subtasks → execute (sub-agent) →
	// docs (sub-agent) → review → done. create_pr=false because the
	// fixture HTTPS server isn't a GitHub remote.
	cardID := s.createCanaryCard(t, cardTitle, cardBody, true /* autonomous */, false /* create_pr */)

	s.triggerRun(t, cardID, false)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// Empirical: an autonomous real-Claude run does Phase 1-3 (plan →
	// 3 subtasks → execute) in ~10 minutes, then Phase 4-6 (docs →
	// review → push) takes another 5-8 minutes. 20m leaves ~2m slack
	// after the optimistic case and ~0 after the slow case; bump
	// further if the slow case becomes routine.
	final := s.waitForState(t, cardID, "done", 20*time.Minute)
	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared, got %q", final.AssignedAgent)
	}

	assertCanaryServer(t, s, canaryUUID, canaryPort)
	assertCanarySkillEngaged(t, s, final)
	assertAuthenticAutonomousRun(t, s, final)

	_ = ctx
}

func testHITLRealClaude(t *testing.T) {
	scenarioID := "hitl-real"
	project := "harness"

	s := bootScenario(t, scenarioID, project, true /* realClaude */)

	canaryUUID := randomHex(t, 4)
	canaryPort := canaryServerPort()

	cardTitle := "Real-Claude HITL canary"
	cardBody := canaryCardBody(canaryUUID, canaryPort)

	// HITL: not autonomous; create_pr=false to keep `gh pr create` out
	// of the picture (fixture isn't a GitHub remote).
	cardID := s.createCanaryCard(t, cardTitle, cardBody, false /* autonomous */, false /* create_pr */)
	buf := s.startTranscript(t, cardID)

	s.triggerRun(t, cardID, true /* interactive */)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Auto-approve every gate emitted by the orchestrator. The
	// create-plan workflow has multiple HITL gates (Phase 0
	// brainstorming may ask several rounds, then Phase 2 plan approval,
	// Phase 4 execution gate, Phase 8 review decision). A single send
	// would stall the run after gate 1.
	//
	// 30m: HITL ≈ autonomous (~18m) + gate round-trips (~3-5m)
	// + brainstorming dialogue (~2-5m). 15m wedged in brainstorming.
	approvalsSent := s.startHITLGateResponder(ctx, t, cardID, buf)

	final := s.waitForState(t, cardID, "done", 30*time.Minute)
	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared, got %q", final.AssignedAgent)
	}

	assertCanaryServer(t, s, canaryUUID, canaryPort)
	// Note: assertCanarySkillEngaged is NOT called for HITL.
	//
	// In HITL the orchestrator sometimes processes subtasks inline
	// rather than spawning sub-agents (a deviation from create-plan
	// Phase 5 — but real-Claude does it). When that happens, no agent
	// invokes the Skill tool, so the harness-canary-skill mount path
	// is never exercised. Autonomous reliably exercises the mount
	// because run-autonomous.md keeps sub-agent spawning rigid;
	// asserting on HITL too just made the test flaky on inline runs
	// without adding signal that autonomous didn't already provide.
	assertAuthenticHITLRun(t, s, final, approvalsSent())
}

// assertCanarySkillEngaged verifies the agent invoked the
// harness-canary-skill at least once across the parent and all its
// subtasks. CM records this via a skill_engaged activity-log entry
// whose Skill field carries the skill name. The skill is meant to be
// engaged by execute-task sub-agents whose subtask body inherits the
// SYSINFO-CANARY marker — which agent records it (parent vs subtask)
// is not the focus of this assertion; the focus is that the
// task_skills_dir mount → entrypoint copy → Skill tool invocation →
// runner-side detection → CM activity log path works end-to-end.
//
// In autonomous mode the runner's logparser typically propagates the
// engagement to the parent card via on-stream Skill-tool detection.
// In HITL mode that propagation can be missing because the orchestrator
// has more conversational context (brainstorming, gate dialog) before
// sub-agents run, and the on-parent record may not fire. Checking all
// cards keeps the assertion meaningful in both modes without relying
// on a propagation that's an implementation detail of the runner.
func assertCanarySkillEngaged(t *testing.T, s *scenarioCtx, parent cardSnapshot) {
	t.Helper()

	// Build the candidate set: parent + all subtasks.
	cards := []cardSnapshot{parent}
	cards = append(cards, s.client.listCards(t, s.project, parent.ID)...)

	for _, c := range cards {
		for _, e := range c.ActivityLog {
			if e.Action == "skill_engaged" && e.Skill == canarySkillName {
				return
			}
		}
	}

	t.Errorf("no skill_engaged entry for %q on parent or any subtask\nrunlog dir: %s",
		canarySkillName, s.rl.dir)
}

// assertAuthenticHITLRun is the HITL counterpart of
// assertAuthenticAutonomousRun. The HITL workflow has the same phase
// shape (plan → subtasks → execute → docs → review → push), so the same
// signals apply. It additionally verifies the gate auto-responder fired
// ≥2 times, proving multiple gates were exercised.
func assertAuthenticHITLRun(t *testing.T, s *scenarioCtx, parent cardSnapshot, approvalsSent int) {
	t.Helper()

	assertAuthenticAutonomousRun(t, s, parent)

	if approvalsSent < 2 {
		t.Errorf("HITL responder sent %d approvals, want ≥2 (plan/execute/review gates)\nrunlog dir: %s",
			approvalsSent, s.rl.dir)
	}
}

// assertAuthenticAutonomousRun verifies the orchestrator exercised the
// full production pipeline: plan, subtasks, sub-agents, cost reporting,
// review, push. Failures point at the runlog directory so the operator
// can read run.md for context.
//
// Documentation phase has no direct CM-side signal (document-task writes
// to repo files, not the parent body). Phase 5 (Review) producing
// "## Review Findings" is sufficient evidence that Phase 4 ran first
// per the run-autonomous protocol.
func assertAuthenticAutonomousRun(t *testing.T, s *scenarioCtx, parent cardSnapshot) {
	t.Helper()

	failHint := func(msg string, args ...any) {
		args = append(args, s.rl.dir)
		t.Errorf("authentic-run check failed: "+msg+"\nrunlog dir: %s", args...)
	}

	// 1. Phase 1: plan content in body. Accept "## Plan" (the
	// create-plan.md spec name) or "## Subtasks" (real Claude
	// sometimes uses this header when drafting in HITL). Both are
	// evidence the orchestrator ran Phase 1.
	if !strings.Contains(parent.Body, "## Plan") && !strings.Contains(parent.Body, "## Subtasks") {
		failHint("parent body has no '## Plan' or '## Subtasks' section\nbody:\n%s", parent.Body)
	}

	// 2 + 3 + 4. Phase 2 + 3: ≥1 subtask, all done, all with token_usage.
	// Real Claude is non-deterministic about subtask count: the canary
	// body suggests three deliverables, and the orchestrator usually
	// creates 2-3 subtasks, but it sometimes consolidates them into a
	// single "implement server with tests" subtask. The test cares that
	// Phase 2 (subtask creation) and Phase 3 (sub-agent execution) ran
	// — both are evident with one subtask. Asserting ≥2 made the test
	// flaky on consolidating runs without adding signal.
	subtasks := s.client.listCards(t, s.project, parent.ID)
	if len(subtasks) < 1 {
		failHint("expected ≥1 subtask, got %d (Phase 2 skipped — fast-path?)", len(subtasks))
	}

	for _, sub := range subtasks {
		if sub.State != "done" {
			failHint("subtask %s state=%q want done", sub.ID, sub.State)
		}

		if sub.TokenUsage == nil || sub.TokenUsage.PromptTokens == 0 {
			failHint("subtask %s missing token_usage (Phase 3 sub-agent skipped report_usage)", sub.ID)
		}
	}

	// 5. Phase 5: ## Review Findings on parent body.
	if !strings.Contains(parent.Body, "## Review Findings") {
		failHint("parent body has no '## Review Findings' section (Phase 5 skipped or aborted)\nbody:\n%s", parent.Body)
	}

	// 6. Phase 6 push: action="pushed" entry on parent activity log.
	pushed := false

	for _, e := range parent.ActivityLog {
		if e.Action == "pushed" {
			pushed = true

			break
		}
	}

	if !pushed {
		failHint("parent activity log has no 'pushed' entry (Phase 6 push skipped)")
	}

	// 7. Cost reporting on parent.
	if parent.TokenUsage == nil || parent.TokenUsage.PromptTokens == 0 {
		failHint("parent token_usage is empty (orchestrator never called report_usage)")
	}
}

// canaryServerPort picks a high port unlikely to collide with other
// services on the host. Re-rolls per scenario via the test rand source.
func canaryServerPort() int {
	return 18080 + rand.Intn(2000)
}

// canaryCardBody renders the prompt body for the sysinfo-server canary.
// Shared by autonomous and HITL real-Claude scenarios so both run the
// same task and the assertCanaryServer helper applies to both.
//
// The body lists three separable deliverables to push the orchestrator
// onto the standard pipeline (≥2 subtasks). Without that nudge the
// orchestrator may fold the entire job into one subtask, which makes
// it impossible to verify Phase 3 sub-agent fan-out from card state
// alone.
func canaryCardBody(canaryUUID string, canaryPort int) string {
	return fmt.Sprintf(`Implement a Go HTTP server, stdlib only (no go.mod entries beyond the standard library), that returns host sysinfo as JSON.

The work has three separable deliverables. Plan them as **distinct subtasks** so they can be reviewed independently.

## Deliverable 1 — Server (main.go)

- Listen on port %d.
- Respond to GET / with a JSON body containing: hostname, goos, goarch,
  num_cpu, go_version. Field names exactly as written here (lowercase
  snake_case).
- Set Content-Type: application/json before writing the response body.
- Respond 405 to any other HTTP method.
- Add a leading comment line "// SYSINFO-CANARY-%s" at the very top of
  main.go (before the package declaration is fine; the static check just
  greps for the exact string).

## Deliverable 2 — Tests (main_test.go)

- Use net/http/httptest.
- Assert GET / returns status 200, Content-Type application/json, and
  that all five sysinfo fields are present in the response body.
- Assert non-GET methods return 405.

## Deliverable 3 — README.md

- One short paragraph describing how to run the server (go run .) and
  what GET / returns.
- One example response body in a fenced code block.

## Required: engage harness-canary-skill before writing code

This card is a SYSINFO-CANARY task. Before you touch any code, invoke the
Skill tool with skill="harness-canary-skill" once. The skill is mounted
into ~/.claude/skills/harness-canary-skill/ by the harness and contains
required guidance for this task.

## Acceptance criteria

- All three deliverables are present and committed on the feature branch.
- main.go imports stdlib only.
- "go build ." and "go test ./..." succeed in the checkout.
- GET / returns the five fields above.
`, canaryPort, canaryUUID)
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

	// Deliverable 3 — README.md — is the closest observable side effect
	// of Phase 4 (Documentation). Forcing it in the canary body lets us
	// stat-check without parsing the parent card body for a doc section.
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("README.md not in checkout: %v", err)
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

		resp, err := http.Get(url) //nolint:noctx
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
