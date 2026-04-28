//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
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

	repoURL := initFixtureRepo(t, s.cfg.tmpDir)

	canaryUUID := randomHex(t, 4)
	cardTitle := fmt.Sprintf("Real-Claude canary: TEST-MARKER-%s", canaryUUID)
	cardBody := fmt.Sprintf("Append a single line `<!-- TEST-MARKER-%s -->` to the end of README.md, then commit and push. Do not perform any other work.", canaryUUID)

	cardID := s.createCardWithRepo(t, cardTitle, true /*autonomous*/, repoURL)
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

	// Assert the bare repo received the canary commit.
	branchOut := mustOutput(t, s.cfg.tmpDir, "git", "-C", repoFromURL(repoURL),
		"log", "--all", "--oneline")
	if !strings.Contains(branchOut, "TEST-MARKER-"+canaryUUID) {
		t.Errorf("fixture bare repo did not receive a TEST-MARKER commit\ngit log --all:\n%s", branchOut)
	}

	if card.ReviewAttempts != 1 {
		t.Errorf("review_attempts: got %d, want 1", card.ReviewAttempts)
	}

	// Friction analysis (informational; doesn't fail the test).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := analyzeTranscript(ctx, t, transcriptBuf.snapshot())
	if err != nil {
		t.Logf("friction analyzer failed: %v", err)
	}

	if report != nil {
		frictionN = len(report.Findings)
	}

	printFrictionReport("Autonomous_RealClaude", report)
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
