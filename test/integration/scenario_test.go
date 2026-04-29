//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
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
	cfg        *scenarioConfig
	cm         *process
	runner     *process
	client     *cmClient
	project    string
	creds      *claudeCredentials // nil in stub mode
	fixtureURL string             // empty in stub mode; HTTPS clone URL in real-Claude
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

	// Real-Claude mode: provision the fixture bare repo + HTTPS server
	// FIRST, then bake the resulting URL into .board.yaml when CM boots.
	// We can't update the URL after boot via PUT — CM's UpdateProject
	// only writes cfg.Repo (singular) and never reconciles cfg.Repos
	// (plural, the registry the runner reads via MCP get_task_context),
	// so a post-boot retarget leaves the runner cloning the stale URL.
	// Stub mode keeps the placeholder URL — it never spawns a real
	// worker against it.
	fixtureURL := ""
	if realClaude {
		fixtureURL = initFixtureRepo(t, cfg.tmpDir)
	}

	initBoardsRepo(t, cfg.boardsDir, project, fixtureURL)

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

	// On test failure, archive stderr buffers + final card snapshots
	// to /tmp/cm-int-failures/<scenarioID>/ so post-mortems survive
	// t.TempDir() cleanup. Runs BEFORE the docker-cleanup hook above
	// because t.Cleanup is LIFO; we need the stderr buffers and the
	// docker container intact at archive time.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		archiveDir := filepath.Join(os.TempDir(), "cm-int-failures", scenarioID)
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			t.Logf("archive dir %s: %v", archiveDir, err)
			return
		}
		_ = os.WriteFile(filepath.Join(archiveDir, "cm.stderr.log"), []byte(cm.stderr.String()), 0o644)
		_ = os.WriteFile(filepath.Join(archiveDir, "runner.stderr.log"), []byte(runner.stderr.String()), 0o644)

		// Snapshot every card in the project so we can see plan body,
		// activity log, runner_status, etc. post-mortem.
		var listResp struct {
			Cards []map[string]any `json:"cards"`
		}
		if status := client.get(t, fmt.Sprintf("/api/projects/%s/cards", project), &listResp); status == http.StatusOK {
			cardsBlob, _ := json.MarshalIndent(listResp, "", "  ")
			_ = os.WriteFile(filepath.Join(archiveDir, "cards.json"), cardsBlob, 0o644)
		}

		// Capture per-container docker logs for any worker that ran in
		// this scenario. The label was set by the runner at spawn time.
		out, _ := exec.Command("docker", "ps", "-aq",
			"--filter", fmt.Sprintf("label=contextmatrix.test=%s", scenarioID)).Output()
		for i, id := range nonEmptyLines(string(out)) {
			if logs, err := exec.Command("docker", "logs", id).CombinedOutput(); err == nil {
				_ = os.WriteFile(filepath.Join(archiveDir, fmt.Sprintf("worker-%d.log", i)), logs, 0o644)
			}
		}

		t.Logf("test failed; diagnostics archived to %s", archiveDir)
	})

	return &scenarioCtx{
		cfg:        cfg,
		cm:         cm,
		runner:     runner,
		client:     client,
		project:    project,
		creds:      creds,
		fixtureURL: fixtureURL,
	}
}

func initBoardsRepo(t *testing.T, boardsDir, project, repoURL string) {
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

	// Stub mode passes empty repoURL → use a placeholder. Real-Claude
	// passes the local HTTPS git server URL provisioned by initFixtureRepo.
	if repoURL == "" {
		repoURL = fmt.Sprintf("https://example.invalid/harness/%s.git", project)
	}

	boardYAML := fmt.Sprintf(`name: %s
prefix: INT
next_id: 1
repo: %s
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
`, project, repoURL)

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

// messageCard POSTs a chat message to /api/projects/{project}/cards/{id}/message.
// Used by HITL scenarios to drive the chat-loop conversation.
func (s *scenarioCtx) messageCard(t *testing.T, cardID, content string) {
	t.Helper()
	body := map[string]any{"content": content}
	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards/%s/message", s.project, cardID), body, nil)
	if status != http.StatusAccepted {
		t.Fatalf("messageCard: HTTP %d body=%s\nCM stderr tail:\n%s",
			status, raw, tail(s.cm.stderr.String(), 30))
	}
}

// promoteCard POSTs to /api/projects/{project}/cards/{id}/promote, flipping
// the card's autonomous flag and fanning out a promotion event over SSE.
func (s *scenarioCtx) promoteCard(t *testing.T, cardID string) {
	t.Helper()
	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards/%s/promote", s.project, cardID), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("promoteCard: HTTP %d body=%s\nCM stderr tail:\n%s",
			status, raw, tail(s.cm.stderr.String(), 30))
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

// waitForPhase polls the card's activity log until a phase=<name> entry
// shows up. Used by HITL scenarios to time messageCard sends so the chat
// input lands after the runner has opened the corresponding session /
// gate. Fails the test on timeout.
func (s *scenarioCtx) waitForPhase(t *testing.T, cardID, phaseName string, timeout time.Duration) {
	t.Helper()
	s.waitForPhaseN(t, cardID, phaseName, 1, timeout)
}

// waitForPhaseN waits until at least minCount phase=<name> entries are
// present in the card's activity log. Used by review-loop scenarios where
// the same phase (review, wait_execution_start) is re-entered and the
// first-occurrence semantics of waitForPhase would match a stale entry
// from an earlier round.
func (s *scenarioCtx) waitForPhaseN(t *testing.T, cardID, phaseName string, minCount int, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	pollUntil(ctx, t, fmt.Sprintf("card %s reach phase=%s (count>=%d)", cardID, phaseName, minCount), func() bool {
		snap := s.client.getCard(t, s.project, cardID)

		n := 0
		for _, e := range snap.ActivityLog {
			if e.Action == "phase" && e.Message == phaseName {
				n++
				if n >= minCount {
					return true
				}
			}
		}

		return false
	})
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

// initFixtureRepo creates a bare git repo with one seed commit, fronts
// it with a local HTTPS git-http-backend on a free port, and returns
// the URL the worker container should clone from. The runner's
// validator only accepts https/ssh schemes (file:// and git:// are
// rejected), and the worker can't reach host loopback paths anyway —
// hence the HTTPS proxy in front of the bare repo.
//
// The returned URL uses host.docker.internal which the orchestrated
// dispatcher already wires into ExtraHosts. Worker containers also
// need GIT_SSL_NO_VERIFY=1 for the self-signed cert; that's set via
// worker_extra_env in the runner config (see writeRunnerConfig).
func initFixtureRepo(t *testing.T, parentTmpDir string) string {
	t.Helper()

	bare := filepath.Join(parentTmpDir, "fixture.git")
	mustRun(t, parentTmpDir, "git", "init", "--bare", bare)
	enableHTTPReceivePack(t, bare)

	// Seed the bare via a local clone+push. Uses the on-disk path; the
	// worker uses the HTTPS URL we return below.
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

	baseURL := startGitHTTPS(t, parentTmpDir)
	return baseURL + "/fixture.git"
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

	// Verify the PUT actually persisted: read back and assert the repo
	// field matches. CM's trigger sources TriggerPayload.RepoURL from
	// projectCfg.Repo, so a silent PUT failure here would let the worker
	// clone the placeholder URL and fail with "could not resolve host".
	var verify projectSnapshot
	if status := s.client.get(t, fmt.Sprintf("/api/projects/%s", s.project), &verify); status != http.StatusOK {
		t.Fatalf("verify project after PUT: HTTP %d", status)
	}
	if verify.Repo != repoURL {
		t.Fatalf("project.repo did not persist after PUT:\n got: %q\nwant: %q", verify.Repo, repoURL)
	}

	return cardID
}
