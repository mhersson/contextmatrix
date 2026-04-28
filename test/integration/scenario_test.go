//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scenarioCtx struct {
	cfg     *scenarioConfig
	cm      *process
	runner  *process
	client  *cmClient
	project string
	creds   *claudeCredentials // nil in stub mode
}

func bootScenario(t *testing.T, scenarioID, project string, realClaude bool) *scenarioCtx {
	t.Helper()

	var creds *claudeCredentials
	if realClaude {
		c, err := resolveClaudeCredentials()
		if err != nil {
			t.Skipf("real-Claude credentials not configured: %v", err)
		}
		creds = c
	}

	cfg := newScenarioConfig(t, scenarioID, realClaude)
	initBoardsRepo(t, cfg.boardsDir, project)

	cmConfigPath := cfg.writeCMConfig(t)
	runnerConfigPath := cfg.writeRunnerConfig(t, creds)

	cm := startCM(t, cmConfigPath, cfg.cmPort)
	runner := startRunner(t, runnerConfigPath, cfg.runnerPort)

	client := newCMClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.cmPort))

	t.Cleanup(func() {
		out, _ := exec.Command("docker", "ps", "-aq",
			"--filter", fmt.Sprintf("label=contextmatrix.test=%s", scenarioID)).Output()
		ids := nonEmptyLines(string(out))
		if len(ids) > 0 {
			args := append([]string{"rm", "-f"}, ids...)
			_ = exec.Command("docker", args...).Run()
		}
	})

	return &scenarioCtx{
		cfg:     cfg,
		cm:      cm,
		runner:  runner,
		client:  client,
		project: project,
		creds:   creds,
	}
}

func initBoardsRepo(t *testing.T, boardsDir, project string) {
	t.Helper()
	mustRun(t, boardsDir, "git", "init")
	mustRun(t, boardsDir, "git", "config", "user.email", "harness@cm.test")
	mustRun(t, boardsDir, "git", "config", "user.name", "harness")

	projectDir := filepath.Join(boardsDir, project)
	if err := os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	boardYAML := fmt.Sprintf(`name: %s
prefix: INT
states:
  - todo
  - in_progress
  - review
  - done
  - not_planned
  - stalled
transitions:
  todo: [in_progress, not_planned]
  in_progress: [review, todo, not_planned]
  review: [in_progress, done, not_planned]
  done: []
  not_planned: [todo]
  stalled: [todo, in_progress, review]
types:
  - task
remote_execution:
  enabled: true
`, project)

	if err := os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardYAML), 0o644); err != nil {
		t.Fatalf("write .board.yaml: %v", err)
	}

	mustRun(t, boardsDir, "git", "add", ".")
	mustRun(t, boardsDir, "git", "commit", "-m", "init harness boards")
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s %v: %v", name, args, err)
	}
}

func (s *scenarioCtx) triggerRun(t *testing.T, cardID string, interactive bool) {
	t.Helper()
	body := map[string]any{"interactive": interactive}
	status := s.client.post(t, fmt.Sprintf("/api/projects/%s/cards/%s/run", s.project, cardID), body, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("trigger: HTTP %d\nrunner stderr:\n%s", status,
			tail(s.runner.stderr.String(), 50))
	}
}

func (s *scenarioCtx) createCard(t *testing.T, title string, autonomous bool) string {
	t.Helper()
	body := map[string]any{
		"title":      title,
		"type":       "task",
		"autonomous": autonomous,
	}
	var resp struct {
		ID string `json:"id"`
	}
	status := s.client.post(t, fmt.Sprintf("/api/projects/%s/cards", s.project), body, &resp)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("createCard: HTTP %d", status)
	}
	if resp.ID == "" {
		t.Fatalf("createCard: empty ID in response")
	}
	return resp.ID
}

func (s *scenarioCtx) waitForState(t *testing.T, cardID, target string, timeout time.Duration) cardSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var snap cardSnapshot
	pollUntil(ctx, t, fmt.Sprintf("card %s reach state=%s", cardID, target), func() bool {
		snap = s.client.getCard(t, s.project, cardID)
		return snap.State == target
	})
	return snap
}

// tail returns the last n lines of s, joined with newlines. Used for
// truncated error context.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
