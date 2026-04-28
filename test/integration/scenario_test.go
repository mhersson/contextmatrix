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
next_id: 1
repo: https://example.invalid/harness/%s.git
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
priorities:
  - low
  - medium
  - high
remote_execution:
  enabled: true
`, project, project)

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
	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards/%s/run", s.project, cardID), body, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("trigger: HTTP %d body=%s\nCM stderr tail:\n%s\n\nrunner stderr tail:\n%s",
			status, raw, tail(s.cm.stderr.String(), 30), tail(s.runner.stderr.String(), 50))
	}
}

func (s *scenarioCtx) createCard(t *testing.T, title string, autonomous bool) string {
	t.Helper()
	body := map[string]any{
		"title":      title,
		"type":       "task",
		"priority":   "medium",
		"autonomous": autonomous,
	}
	var resp struct {
		ID string `json:"id"`
	}
	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards", s.project), body, &resp)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("createCard: HTTP %d body=%s\nCM stderr tail:\n%s", status, raw,
			tail(s.cm.stderr.String(), 30))
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
	// For terminal targets (done, not_planned), also wait for runner_status
	// to clear — the runner posts UpdateRunnerStatus("completed") slightly
	// after the card transitions, so polling state alone races the cleanup.
	terminal := target == "done" || target == "not_planned"
	pollUntil(ctx, t, fmt.Sprintf("card %s reach state=%s (terminal=%v)", cardID, target, terminal), func() bool {
		snap = s.client.getCard(t, s.project, cardID)
		if snap.State != target {
			return false
		}
		if terminal && snap.RunnerStatus != "" {
			return false
		}
		return true
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

// initFixtureRepo creates a bare git repo with one seed commit (a tiny
// README.md) and returns its file:// URL. Used in real-Claude mode so
// the agent has something to clone and push back to.
func initFixtureRepo(t *testing.T, parentTmpDir string) string {
	t.Helper()

	bare := filepath.Join(parentTmpDir, "fixture.git")
	mustRun(t, parentTmpDir, "git", "init", "--bare", bare)

	// Set up a seed commit by cloning the bare, committing, pushing.
	work := filepath.Join(parentTmpDir, "fixture-seed")
	mustRun(t, parentTmpDir, "git", "clone", bare, work)
	mustRun(t, work, "git", "config", "user.email", "harness@cm.test")
	mustRun(t, work, "git", "config", "user.name", "harness")

	if err := os.WriteFile(filepath.Join(work, "README.md"),
		[]byte("# integration harness fixture\n\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, work, "git", "add", "README.md")
	mustRun(t, work, "git", "commit", "-m", "init")
	mustRun(t, work, "git", "branch", "-M", "main")
	mustRun(t, work, "git", "push", "-u", "origin", "main")

	return "file://" + bare
}

// projectSnapshot is the subset of board.ProjectConfig the harness needs
// to round-trip a PUT /api/projects/{project} call (full-replacement
// semantics — omitting states/types/priorities/transitions would clear
// them). Only the fields involved in repo-URL retargeting are decoded;
// the rest are echoed back verbatim.
type projectSnapshot struct {
	Repo        string              `json:"repo,omitempty"`
	States      []string            `json:"states"`
	Types       []string            `json:"types"`
	Priorities  []string            `json:"priorities"`
	Transitions map[string][]string `json:"transitions"`
}

// createCardWithRepo creates a card and additionally retargets the
// project's repo URL via PUT /api/projects/{project}. Used in
// real-Claude mode where the runner needs to know where to clone from.
//
// CM stores the clone URL on the project (board.ProjectConfig.Repo,
// surfaced as `repo` in JSON), not on the card — the runner's
// TriggerPayload.RepoURL is sourced from projectCfg.Repo at run time.
// PUT semantics on /api/projects/{project} are full-replacement, so
// the helper fetches the current config and re-PUTs with `repo`
// swapped for the fixture URL.
func (s *scenarioCtx) createCardWithRepo(t *testing.T, title string, autonomous bool, repoURL string) string {
	t.Helper()

	cardID := s.createCard(t, title, autonomous)

	var current projectSnapshot
	if status := s.client.get(t, fmt.Sprintf("/api/projects/%s", s.project), &current); status != http.StatusOK {
		t.Fatalf("get project for repo retarget: HTTP %d", status)
	}

	body := map[string]any{
		"repo":        repoURL,
		"states":      current.States,
		"types":       current.Types,
		"priorities":  current.Priorities,
		"transitions": current.Transitions,
	}
	status, raw := s.client.putRaw(t, fmt.Sprintf("/api/projects/%s", s.project), body, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("set project repo: HTTP %d body=%s", status, raw)
	}

	return cardID
}
