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
	"slices"
	"testing"
	"time"
)

// harness bundles a booted CM (admin session) plus the host services worker
// containers reach via host.docker.internal (the scripted LLM and the seeded
// git server).
type harness struct {
	sc      *scenarioConfig
	cm      *process
	client  *apiClient
	rl      *runLog
	git     *gitServer
	project string
}

// bootHarness starts the git server, the scripted LLM, and CM (auth.mode:
// multi) with the given backends declared, then bootstraps an admin session.
// workerImage is baked into the project's remote_execution.worker_image.
// It registers the finalize / cards-snapshot / docker-sweep cleanups in the
// LIFO order the runlog needs (finalize reads stable sinks after the
// subprocesses stop).
func bootHarness(t *testing.T, scenarioID, project string, opts cmConfigOptions, reply func(chatRequest) string, workerImage, sweepLabel string) *harness {
	t.Helper()

	sc := newScenarioConfig(t, scenarioID)
	sc.workerImage = workerImage

	rl, err := newRunLog(scenarioID)
	if err != nil {
		t.Fatalf("runlog: %v", err)
	}

	start := time.Now()

	// Registered first → runs last: sweep any labelled containers left behind.
	t.Cleanup(func() {
		if ids := dockerListByLabel(sweepLabel); len(ids) > 0 {
			args := append([]string{"rm", "-f"}, ids...)
			_ = exec.Command("docker", args...).Run()
		}
	})

	// Registered second → runs after the subprocess SIGTERMs (stable sinks).
	t.Cleanup(func() {
		status := "PASS"
		if t.Skipped() {
			status = "SKIP"
		} else if t.Failed() {
			status = "FAIL"
		}

		rl.finalize(scenarioID, status, time.Since(start), nil)
		t.Logf("scenario diagnostics: %s", rl.dir)
	})

	git := startGitServer(t, rl)
	sc.gitPort = git.port
	sc.repoURL = git.containerURL()

	stub := startStubLLM(t, rl, scenarioID, reply)
	sc.stubLLMPort = stub.port

	initBoardsRepo(t, sc, project)

	cfgPath := sc.writeCMConfig(t, opts)

	cm := startCM(t, cfgPath, sc.cmPort, rl)

	admin := bootAdminSession(t, fmt.Sprintf("http://127.0.0.1:%d", sc.cmPort), cm)

	// Cards snapshot for run.md. Registered after startCM → runs before CM's
	// SIGTERM (LIFO), so CM is still up to answer; uses the admin session jar.
	t.Cleanup(func() { snapshotCards(admin, rl, project) })

	return &harness{sc: sc, cm: cm, client: admin, rl: rl, git: git, project: project}
}

// snapshotCards writes the project's cards JSON to cards.json for the runlog
// markdown report. Best-effort (runs in cleanup, no *testing.T): errors are
// swallowed. Uses the admin client's cookie jar for the session-gated read.
func snapshotCards(client *apiClient, rl *runLog, project string) {
	req, err := http.NewRequest(http.MethodGet, client.baseURL+"/api/projects/"+project+"/cards", nil)
	if err != nil {
		return
	}

	resp, err := client.hc.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if len(body) > 0 {
		_ = os.WriteFile(filepath.Join(rl.dir, "cards.json"), body, 0o644)
	}
}

// createCard creates a card via the admin session and returns its ID.
func (h *harness) createCard(t *testing.T, title, body string, autonomous bool) string {
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

	status, raw := h.client.do(t, http.MethodPost, "/api/projects/"+h.project+"/cards", req, &resp)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("createCard: HTTP %d body=%s\nCM stderr tail:\n%s", status, raw, tail(h.cm.stderr.String(), 30))
	}

	if resp.ID == "" {
		t.Fatalf("createCard: empty ID in response")
	}

	return resp.ID
}

// triggerRun triggers remote execution for the card.
func (h *harness) triggerRun(t *testing.T, cardID string, interactive bool) {
	t.Helper()

	body := map[string]any{"interactive": interactive}

	status, raw := h.client.do(t, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/cards/%s/run", h.project, cardID), body, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("trigger: HTTP %d body=%s\nCM stderr tail:\n%s\n\nagent stderr tail:\n%s",
			status, raw, tail(h.cm.stderr.String(), 30), tail(h.rl.agentSink.String(), 60))
	}
}

// waitForState polls the card until it reaches target (and, for terminal
// targets, worker_status has cleared).
func (h *harness) waitForState(t *testing.T, cardID, target string, timeout time.Duration) cardSnapshot {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var snap cardSnapshot

	terminal := target == "done" || target == "not_planned"
	pollUntil(ctx, t, fmt.Sprintf("card %s reach state=%s", cardID, target), func() bool {
		snap = h.client.getCard(t, h.project, cardID)
		if snap.State != target {
			return false
		}

		if terminal && snap.WorkerStatus != "" {
			return false
		}

		return true
	})

	return snap
}

// TestAgentScenario runs the contextmatrix-agent backend end to end against the
// scripted LLM: an autonomous card is decomposed, worked, reviewed, and
// integrated, driving the parent card to done with its feature branch pushed to
// the seeded git server.
func TestAgentScenario(t *testing.T) {
	requireContainerNetworking(t)  // t.Skip when the daemon lacks bridge networking.
	assets := ensureAgentAssets(t) // t.Skip when the sibling repo is absent.

	const project = "harness"

	// The scripted happy path (ported from the agent's own e2e): a two-subtask
	// plan, a passing verify gate, first-round review approval. Costs are
	// non-zero so the report_usage / cost plumbing is exercised.
	backend := &scriptedBackend{
		approveImmediately: true,
		planCost:           0.0100,
		coderCost:          0.0200,
		documentCost:       0.0030,
		specialistCost:     0.0050,
		synthesisCost:      0.0100,
	}

	h := bootHarness(t, "agent", project, cmConfigOptions{agent: true},
		backend.reply, agentWorkerImage, "contextmatrix.agent=true")

	agent := startAgent(t, assets.hostBinary, h.sc.writeAgentConfig(t), h.sc.agentPort, h.rl)
	_ = agent

	capture := startWorkerCapture(h.rl, "contextmatrix.agent=true")
	t.Cleanup(capture.stop)

	cardID := h.createCard(t, "smoke agent card", "Add features A and B.", true /* autonomous */)

	// Diagnostics: capture the worker-log SSE transcript for the run.md report.
	buf := newTranscriptBuffer(2 * 1024 * 1024)
	startTranscriptCapture(t, h.client, project, cardID, buf, h.rl)

	h.triggerRun(t, cardID, false /* interactive */)

	// The full autonomous flow (plan → 2 coders → review → document → integrate)
	// runs a container against real CM + the scripted LLM; allow a generous
	// margin for the container build/boot and the MCP round-trips.
	final := h.waitForState(t, cardID, "done", 6*time.Minute)

	if final.AssignedAgent != "" {
		t.Errorf("agent should be cleared after the run, got %q", final.AssignedAgent)
	}

	if len(final.ActivityLog) == 0 {
		t.Errorf("activity log empty; expected claim/transition/release entries")
	}

	// The card branch (cm/<lowercased-id>) must exist on the seeded remote.
	wantBranch := "cm/int-001"
	branches := h.git.remoteBranches(t)
	if !slices.Contains(branches, wantBranch) {
		t.Errorf("pushed branch %q not found on remote; got %v", wantBranch, branches)
	}
}
