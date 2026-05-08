//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"io"
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
	rl      *runLog           // observability (combined log + per-source files + run.md)
	tb      *transcriptBuffer // optional; set by startTranscript for HITL scenarios
}

func bootScenarioWithConfig(t *testing.T, scenarioID, project string, override func(*scenarioConfig)) *scenarioCtx {
	t.Helper()

	cfg := newScenarioConfig(t, scenarioID)
	if override != nil {
		override(cfg)
	}

	rl, err := newRunLog(scenarioID)
	if err != nil {
		t.Fatalf("runlog: %v", err)
	}

	scenarioStart := time.Now()

	// sc is captured by the finalize cleanup below by pointer so the
	// closure can pull the transcript out of sc.tb (set later by
	// startTranscript for HITL scenarios) at finalize time.
	var sc *scenarioCtx

	initBoardsRepo(t, cfg.boardsDir, project)

	cmConfigPath := cfg.writeCMConfig(t)
	runnerConfigPath := cfg.writeRunnerConfig(t)

	client := newCMClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.cmPort))

	// Cleanup ordering — the t.Cleanup stack is LIFO, so registrations
	// here run in reverse. The required execution order is:
	//
	//   1. capture.stop        — close worker.raw.jsonl
	//   2. SIGTERM CM + Wait   — io.Copy goroutines stop writing to cmSink
	//   3. SIGTERM runner+Wait — io.Copy goroutines stop writing to runnerSink
	//   4. finalize            — reads cmSink/runnerSink (now stable) + on-disk artefacts
	//   5. docker rm -f        — sweep any leftover labelled containers
	//
	// To get that LIFO, we register the docker-sweep + finalize BEFORE
	// startCM/startRunner (which register their own SIGTERM+Wait
	// cleanups), and capture.stop AFTER. Without this ordering, the
	// race detector flags reads of cmSink/runnerSink in finalize
	// against the still-running io.Copy goroutines.
	t.Cleanup(func() {
		ids := dockerListByScenario(scenarioID)
		if len(ids) > 0 {
			args := append([]string{"rm", "-f"}, ids...)
			_ = exec.Command("docker", args...).Run()
		}
	})

	t.Cleanup(func() {
		status := "PASS"
		if t.Skipped() {
			status = "SKIP"
		} else if t.Failed() {
			status = "FAIL"
		}

		var transcriptJSONL []byte
		if sc != nil && sc.tb != nil {
			transcriptJSONL = transcriptToJSONL(sc.tb.snapshot())
		}

		rl.finalize(scenarioID, status, time.Since(scenarioStart), transcriptJSONL)

		t.Logf("scenario diagnostics: %s", rl.dir)
	})

	cm := startCM(t, cmConfigPath, cfg.cmPort, rl)

	// Cards-snapshot cleanup. Registered AFTER startCM so it runs BEFORE
	// CM's SIGTERM cleanup (LIFO). Always-on so run.md can render the
	// final card state on success runs too. Errors are logged and
	// swallowed — a missing snapshot must not abort the rest of the
	// LIFO chain (combined.log finalize, etc.).
	t.Cleanup(func() {
		url := fmt.Sprintf("http://127.0.0.1:%d/api/projects/%s/cards", cfg.cmPort, project)
		snapClient := &http.Client{Timeout: 2 * time.Second}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			rl.writeLine("harness", "cards snapshot: build request: "+err.Error())

			return
		}

		req.Header.Set("X-Agent-ID", "human:harness")

		resp, err := snapClient.Do(req)
		if err != nil {
			rl.writeLine("harness", "cards snapshot: do: "+err.Error())

			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			rl.writeLine("harness", fmt.Sprintf("cards snapshot: HTTP %d", resp.StatusCode))

			return
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err := os.WriteFile(filepath.Join(rl.dir, "cards.json"), body, 0o644); err != nil {
			rl.writeLine("harness", "cards snapshot: write: "+err.Error())
		}
	})

	runner := startRunner(t, runnerConfigPath, cfg.runnerPort, rl)

	// Live-stream the worker container's stdout+stderr to disk. The
	// goroutine polls for the container (it appears only after /trigger),
	// then runs "docker logs -f" until the container exits or the context
	// is cancelled. Registered LAST so it runs FIRST in LIFO — stop()
	// flushes and closes worker.raw.jsonl before finalize reads it.
	capture := startWorkerCapture(rl, scenarioID)
	t.Cleanup(capture.stop)

	sc = &scenarioCtx{
		cfg:     cfg,
		cm:      cm,
		runner:  runner,
		client:  client,
		project: project,
		rl:      rl,
	}

	return sc
}

func bootScenario(t *testing.T, scenarioID, project string) *scenarioCtx {
	return bootScenarioWithConfig(t, scenarioID, project, nil)
}

// transcriptToJSONL renders captured SSE events as one JSON object per
// line (JSONL) — the same shape saveTranscript writes. Used by the
// runlog finalize hook so transcript.jsonl ends up in the per-scenario
// directory alongside cm.log / runner.log / combined.log.
func transcriptToJSONL(events []transcriptEvent) []byte {
	if len(events) == 0 {
		return nil
	}

	var b []byte

	for _, ev := range events {
		if ev.RawJSON == "" {
			continue
		}

		b = append(b, []byte(ev.RawJSON)...)
		b = append(b, '\n')
	}

	return b
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

	// Stub-only suite: the worker never actually clones or pushes, so
	// the repo URL is a placeholder that the runner's validator
	// accepts but no real network call hits.
	repoURL := fmt.Sprintf("https://example.invalid/harness/%s.git", project)

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
  - feature
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
// Used by HITL scenarios to drive the chat-loop conversation. The text
// is also recorded in the scenario's combined log under [user_chat] so
// post-mortems show what the simulated human typed.
func (s *scenarioCtx) messageCard(t *testing.T, cardID, content string) {
	t.Helper()

	if s.rl != nil {
		s.rl.writeLine("user_chat", fmt.Sprintf("%s: %s", cardID, content))
	}

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

	if s.rl != nil {
		s.rl.writeLine("user_promote", cardID)
	}

	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards/%s/promote", s.project, cardID), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("promoteCard: HTTP %d body=%s\nCM stderr tail:\n%s",
			status, raw, tail(s.cm.stderr.String(), 30))
	}
}

func (s *scenarioCtx) stopCard(t *testing.T, cardID string) {
	t.Helper()

	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards/%s/stop", s.project, cardID), nil, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("stop: HTTP %d body=%s\nCM stderr tail:\n%s\n\nrunner stderr tail:\n%s",
			status, raw, tail(s.cm.stderr.String(), 30), tail(s.runner.stderr.String(), 50))
	}
}

func (s *scenarioCtx) waitForCardClaimed(ctx context.Context, t *testing.T, cardID string) {
	t.Helper()
	pollUntil(ctx, t, fmt.Sprintf("card %s claimed", cardID), func() bool {
		return s.client.getCard(t, s.project, cardID).AssignedAgent != ""
	})
}

func (s *scenarioCtx) waitForAgentCleared(ctx context.Context, t *testing.T, cardID string) {
	t.Helper()
	pollUntil(ctx, t, fmt.Sprintf("card %s agent cleared", cardID), func() bool {
		return s.client.getCard(t, s.project, cardID).AssignedAgent == ""
	})
}

func (s *scenarioCtx) createCard(t *testing.T, title string, autonomous bool) string {
	t.Helper()

	return s.createCardOfType(t, title, "task", autonomous)
}

func (s *scenarioCtx) createCardWithBody(t *testing.T, title, body string, autonomous bool) string {
	t.Helper()

	req := map[string]any{
		"title":      title,
		"type":       "task",
		"priority":   "medium",
		"autonomous": autonomous,
		"body":       body,
	}

	var resp struct {
		ID string `json:"id"`
	}

	status, raw := s.client.postRaw(t, fmt.Sprintf("/api/projects/%s/cards", s.project), req, &resp)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("createCardWithBody: HTTP %d body=%s\nCM stderr tail:\n%s", status, raw,
			tail(s.cm.stderr.String(), 30))
	}

	if resp.ID == "" {
		t.Fatalf("createCardWithBody: empty ID in response")
	}

	return resp.ID
}

// createCardOfType is a variant of createCard that lets the test pick
// the card's `type`. Brainstorm scenarios need `type=feature` so the
// runner's NeedsBrainstormGuard fires; standard task/plan scenarios
// keep using createCard with the default `task` type.
func (s *scenarioCtx) createCardOfType(t *testing.T, title, cardType string, autonomous bool) string {
	t.Helper()

	body := map[string]any{
		"title":      title,
		"type":       cardType,
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

// startTranscript opens a transcript buffer and SSE consumer for the given
// card and wires it into s.tb so the runlog finalize hook writes
// transcript.jsonl. Returns the buffer for polling.
func (s *scenarioCtx) startTranscript(t *testing.T, cardID string) *transcriptBuffer {
	t.Helper()

	buf := newTranscriptBuffer(2 * 1024 * 1024) // 2 MiB cap
	cmBaseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.cmPort)
	startTranscriptCapture(t, cmBaseURL, s.project, cardID, buf, s.rl)
	s.tb = buf

	return buf
}

// dockerListByScenario returns container IDs for containers managed by the
// contextmatrix runner. The id parameter is accepted for call-site
// compatibility but is unused — the runner labels containers with
// contextmatrix.runner=true rather than a per-scenario tag, so we filter
// by the label it actually applies. Subtests run sequentially, so at most
// one worker container is live at any given time.
func dockerListByScenario(_ string) []string {
	out, err := exec.Command("docker", "ps", "-aq",
		"--filter", "label=contextmatrix.runner=true").Output()
	if err != nil {
		return nil
	}

	return nonEmptyLines(string(out))
}
