//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	rl         *runLog            // observability (combined log + per-source files + run.md)
	tb         *transcriptBuffer  // optional; set by startTranscriptCapture for real-Claude scenarios
}

func bootScenarioWithConfig(t *testing.T, scenarioID, project string, realClaude bool, override func(*scenarioConfig)) *scenarioCtx {
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
	// startTranscriptCapture for real-Claude scenarios) at finalize time.
	var sc *scenarioCtx

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
	cfg.writeCanarySkill(t)

	cmConfigPath := cfg.writeCMConfig(t)
	runnerConfigPath := cfg.writeRunnerConfig(t, creds)

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
		cfg:        cfg,
		cm:         cm,
		runner:     runner,
		client:     client,
		project:    project,
		creds:      creds,
		fixtureURL: fixtureURL,
		rl:         rl,
	}

	return sc
}

func bootScenario(t *testing.T, scenarioID, project string, realClaude bool) *scenarioCtx {
	return bootScenarioWithConfig(t, scenarioID, project, realClaude, nil)
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
  - feature
priorities:
  - low
  - medium
  - high
remote_execution:
  enabled: true
default_skills:
  - harness-canary-skill
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

// tryMessageCard is the goroutine-safe variant of messageCard. It uses
// a stdlib HTTP client directly so no t.Fatalf path is reachable, and
// returns errors instead. Used by the HITL gate responder. The 410
// (stdin_closed) status is returned as a typed error so the caller can
// stop polling without flagging a test failure.
func (s *scenarioCtx) tryMessageCard(cardID, content string) error {
	if s.rl != nil {
		s.rl.writeLine("user_chat", fmt.Sprintf("%s: %s", cardID, content))
	}

	body, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/api/projects/%s/cards/%s/message",
		s.cfg.cmPort, s.project, cardID)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:harness")
	req.Header.Set("X-Requested-With", "contextmatrix")

	hc := &http.Client{Timeout: 5 * time.Second}

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		return errStdinClosed
	}

	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}

	return nil
}

// errStdinClosed is returned by tryMessageCard when CM/runner reports
// the worker container's stdin has already been closed (e.g. after
// promote, or after the agent finished). Callers stop polling on this
// error rather than flag it as a test failure.
var errStdinClosed = errors.New("stdin_closed")

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

	return s.createCanaryCard(t, title, body, autonomous, true)
}

// createCanaryCard creates a card with explicit feature_branch and
// create_pr flags. Real-Claude scenarios set feature_branch=true so
// the agent works on a branch but create_pr=false because the fixture
// HTTPS git server isn't a GitHub remote and `gh pr create` would
// fail. Without this, CM's /run handler auto-enables both when
// feature_branch is unset, forcing create_pr=true.
func (s *scenarioCtx) createCanaryCard(t *testing.T, title, body string, autonomous, createPR bool) string {
	t.Helper()

	req := map[string]any{
		"title":          title,
		"type":           "task",
		"priority":       "medium",
		"autonomous":     autonomous,
		"body":           body,
		"feature_branch": true,
		"create_pr":      createPR,
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

// startHITLGateResponder spawns a goroutine that auto-approves each gate
// emitted by a real-Claude HITL run. It uses idle-detection on the
// transcript buffer: when the buffer stops growing for `idleThreshold`
// after a `text` event, the agent has ended its turn and is waiting for
// stdin → send "approve". Stops when ctx is cancelled OR the
// container's stdin closes (run finished or promoted).
//
// Why idle-detection, not "?"-matching: real-Claude prompts often end
// with a summary or recommendation, not a literal question mark
// ("Here's the design summary. ... I recommend A."). Even with "?"
// matched anywhere, mid-sentence "?" inside the agent's reasoning gets
// false-positive'd and the trailing summary still doesn't match. The
// runner emits no end-of-turn signal of its own. Idle-after-text is
// the only reliable cross-gate signal available.
//
// Why type "text", not "assistant": the runner's logparser walks the
// stream-json envelope ({type:assistant, message:{content:[{type:text,
// ...}]}}) and emits the inner block as logbroadcast.LogEntry{Type:
// "text"}. SSE consumers see Type="text", never Type="assistant".
//
// Returns a func() int that returns the count of approvals sent so far.
// The HITL test calls it after the run completes to verify ≥2 gates were
// exercised.
//
// Why tryMessageCard, not messageCard: the goroutine cannot call
// t.Fatalf safely (testing.T fatal methods must be called from the test
// goroutine). tryMessageCard returns errors instead.
func (s *scenarioCtx) startHITLGateResponder(ctx context.Context, t *testing.T, cardID string, buf *transcriptBuffer) func() int {
	t.Helper()

	const (
		// idleThreshold: after a text event, wait this long with no new
		// events before declaring the agent waiting-for-input. 8s is
		// well below the runner's 90s idle_output_timeout but above the
		// few-hundred-ms gap between consecutive text/thinking frames.
		idleThreshold = 8 * time.Second
		// fireDebounce: minimum gap between two consecutive approvals.
		// Once we fire, Claude responds within 1-3s and the buffer
		// grows, resetting our idle clock. Debounce protects against
		// flapping if Claude doesn't respond at all (MCP dead, etc.).
		fireDebounce = 5 * time.Second
	)

	var (
		mu        sync.Mutex
		approvals int
	)

	go func() {
		var (
			lastBufLen  int
			lastGrowAt  = time.Now()
			lastFiredAt time.Time
		)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			events := buf.snapshot()

			// Buffer growing → agent is producing output. Reset the
			// idle clock; nothing to do.
			if len(events) > lastBufLen {
				lastBufLen = len(events)
				lastGrowAt = time.Now()

				continue
			}

			if lastBufLen == 0 {
				continue
			}

			// Buffer is steady. Has it been steady long enough?
			if time.Since(lastGrowAt) < idleThreshold {
				continue
			}

			// Only fire when the most recent event is a text frame.
			// Idle-after-tool_call usually means the agent is waiting
			// on a tool result, not on the user.
			if events[lastBufLen-1].Type != "text" {
				continue
			}

			if time.Since(lastFiredAt) < fireDebounce {
				continue
			}

			if err := s.tryMessageCard(cardID, "approve"); err != nil {
				if errors.Is(err, errStdinClosed) {
					// Container stdin closed — run finished or was
					// promoted. Stop the responder cleanly.
					return
				}

				if s.rl != nil {
					s.rl.writeLine("harness", "gate_responder: tryMessageCard: "+err.Error())
				}

				continue
			}

			lastFiredAt = time.Now()

			mu.Lock()
			approvals++
			mu.Unlock()

			if s.rl != nil {
				s.rl.writeLine("harness", "gate_responder: sent approve (idle ≥ 8s after text)")
			}
		}
	}()

	return func() int {
		mu.Lock()
		defer mu.Unlock()

		return approvals
	}
}
