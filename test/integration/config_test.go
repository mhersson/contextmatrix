//go:build integration

package integration_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Worker image tags built by ensureAgentAssets / ensureChatAssets and baked
// into a project's remote_execution.worker_image by initBoardsRepo.
const (
	agentWorkerImage = "cm-agent-worker:test"
	chatWorkerImage  = "cm-chat-worker:test"
)

// scenarioConfig holds everything needed to generate a CM config plus the
// sibling backend serve configs for one scenario. Each scenario runs a fully
// isolated CM (own boards dir, own ports, own SQLite files) so runs never
// share state.
type scenarioConfig struct {
	scenarioID string
	tmpDir     string
	boardsDir  string

	cmPort    int
	agentPort int
	chatPort  int

	// Host services the workers reach via host.docker.internal. Populated by
	// the scenario after the listeners bind (kernel-assigned ports).
	stubLLMPort int
	gitPort     int

	// Shared secrets. api_key is the HMAC secret between CM and a backend;
	// mcpAPIKey authenticates workers to CM's MCP endpoint.
	agentAPIKey string
	chatAPIKey  string
	mcpAPIKey   string

	// workerImage is baked into the project's remote_execution.worker_image.
	workerImage string

	// repoURL is the project's code repo (the seeded git server's container
	// URL). CM never clones it; the worker does.
	repoURL string

	// Per-backend host state dirs.
	agentSecretsDir string
	chatSecretsDir  string
	chatRunDir      string
}

func newScenarioConfig(t *testing.T, scenarioID string) *scenarioConfig {
	t.Helper()

	tmpDir := t.TempDir()

	boardsDir := filepath.Join(tmpDir, "boards")
	if err := os.MkdirAll(boardsDir, 0o755); err != nil {
		t.Fatalf("mkdir boards: %v", err)
	}

	mkdir := func(name string, perm os.FileMode) string {
		p := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(p, perm); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}

		return p
	}

	return &scenarioConfig{
		scenarioID:      scenarioID,
		tmpDir:          tmpDir,
		boardsDir:       boardsDir,
		cmPort:          freePort(t),
		agentPort:       freePort(t),
		chatPort:        freePort(t),
		agentAPIKey:     randomHex(t, 32),
		chatAPIKey:      randomHex(t, 32),
		mcpAPIKey:       randomHex(t, 32),
		agentSecretsDir: mkdir("cm-agent-secrets", 0o700),
		chatSecretsDir:  mkdir("cm-chat-secrets", 0o700),
		chatRunDir:      mkdir("cm-chat-runs", 0o700),
	}
}

// cmConfigOptions selects which backends CM declares. ChatREST needs neither;
// the agent scenario needs agent; the chat scenario needs chat.
type cmConfigOptions struct {
	agent bool
	chat  bool
}

// writeCMConfig writes the CM config in auth.mode: multi. op_store/images/auth
// paths land in the scenario tmp dir so the run is hermetic (no shared XDG
// state). llm_endpoint points workers at the scripted LLM; github.pat satisfies
// the fail-closed git-token mint (the git server ignores the token).
//
// Note: CM itself cannot resolve host.docker.internal (host has no such alias),
// so its own catalog/chat-picker fetch of llm_endpoint fails — best-effort and
// fail-open by design. Workers reach the endpoint fine from inside containers.
func (sc *scenarioConfig) writeCMConfig(t *testing.T, opts cmConfigOptions) string {
	t.Helper()

	path := filepath.Join(sc.tmpDir, "cm-config.yaml")

	backends := ""
	if opts.agent || opts.chat {
		backends = "backends:\n"
		if opts.agent {
			backends += fmt.Sprintf(`  agent:
    url: http://127.0.0.1:%d
    api_key: %q
    default_model: stub/model
`, sc.agentPort, sc.agentAPIKey)
		}

		if opts.chat {
			backends += fmt.Sprintf(`  chat:
    url: http://127.0.0.1:%d
    api_key: %q
    default_model: stub/model
`, sc.chatPort, sc.chatAPIKey)
		}
	}

	// llm_endpoint is only meaningful when a worker backend runs — CM forwards
	// it to workers. Omit it for backend-free tests so there is no bogus
	// endpoint to fetch.
	llmEndpoint := ""
	if opts.agent || opts.chat {
		llmEndpoint = fmt.Sprintf(`llm_endpoint:
  type: openai
  base_url: http://host.docker.internal:%d
  api_key: stub-llm-key
`, sc.stubLLMPort)
	}

	// Without an explicit workflow_skills_dir CM defaults to a workflow-skills
	// dir next to the generated config — which doesn't exist, and the worker's
	// start_review MCP call fails on the missing review-task.md. Point it at
	// the repo's canonical skills so the scenarios exercise what ships.
	workflowSkillsDir := filepath.Join(harnessRoot, "..", "..", "workflow-skills")

	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
mcp_api_key: %q
workflow_skills_dir: %s
boards:
  dir: %s
  git_auto_commit: true
%s%sop_store:
  db_path: %s
images:
  db_path: %s
cors_origin: http://127.0.0.1:0
theme: everforest
auth:
  mode: multi
  db_path: %s
  master_key_file: %s
github:
  auth_mode: pat
  pat:
    token: harness-pat
`, sc.cmPort, sc.mcpAPIKey, workflowSkillsDir, sc.boardsDir, backends, llmEndpoint,
		filepath.Join(sc.tmpDir, "ops.db"), filepath.Join(sc.tmpDir, "images.db"),
		filepath.Join(sc.tmpDir, "auth.db"), filepath.Join(sc.tmpDir, "master.key"))

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write CM config: %v", err)
	}

	return path
}

// writeAgentConfig writes the contextmatrix-agent serve config. base_image is
// the harness-built minimal worker; image_pull_policy: never keeps it local;
// max_card_cost: 0 disables the ceiling. secrets_dir is a per-scenario tmp dir
// (the default /var/run path needs root).
func (sc *scenarioConfig) writeAgentConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(sc.tmpDir, "agent-serve.yaml")

	body := fmt.Sprintf(`contextmatrix_url: http://127.0.0.1:%d
container_contextmatrix_url: http://host.docker.internal:%d
api_key: %q
mcp_api_key: %q
port: %d
log_level: debug
base_image: %s
image_pull_policy: never
max_concurrent: 2
secrets_dir: %s
default_model: stub/model
max_card_cost: 0
`, sc.cmPort, sc.cmPort, sc.agentAPIKey, sc.mcpAPIKey, sc.agentPort,
		sc.workerImage, sc.agentSecretsDir)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	return path
}

// writeChatConfig writes the contextmatrix-chat serve config. chat_run_dir is
// required. The per-session MCP key, model, and LLM endpoint are supplied by CM
// in the chat-start payload, not here.
func (sc *scenarioConfig) writeChatConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(sc.tmpDir, "chat-serve.yaml")

	body := fmt.Sprintf(`contextmatrix_url: http://127.0.0.1:%d
container_contextmatrix_url: http://host.docker.internal:%d
api_key: %q
port: %d
log_level: debug
base_image: %s
image_pull_policy: never
max_concurrent: 2
secrets_dir: %s
chat_run_dir: %s
`, sc.cmPort, sc.cmPort, sc.chatAPIKey, sc.chatPort,
		sc.workerImage, sc.chatSecretsDir, sc.chatRunDir)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write chat config: %v", err)
	}

	return path
}

// initBoardsRepo initialises the boards git repo with one project whose
// .board.yaml points remote execution at the seeded git server and the
// harness worker image. verify is pinned to `true` so the worker's verify
// gate needs no language toolchain in the minimal image.
func initBoardsRepo(t *testing.T, sc *scenarioConfig, project string) {
	t.Helper()

	mustRun(t, sc.boardsDir, "git", "init")
	mustRun(t, sc.boardsDir, "git", "config", "user.email", "harness@cm.test")
	mustRun(t, sc.boardsDir, "git", "config", "user.name", "harness")

	projectDir := filepath.Join(sc.boardsDir, project)
	if err := os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(projectDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	// The legacy singular `repo:` field is intentionally scheme-unvalidated
	// (unlike repos[]), so the plain-HTTP git-server URL is accepted.
	remoteExec := "remote_execution:\n  enabled: false\n"
	if sc.workerImage != "" {
		enabled := "false"
		if sc.workerImage == agentWorkerImage {
			enabled = "true"
		}

		remoteExec = fmt.Sprintf("remote_execution:\n  enabled: %s\n  worker_image: %s\n",
			enabled, sc.workerImage)
	}

	repoLine := ""
	if sc.repoURL != "" {
		repoLine = "repo: " + sc.repoURL + "\n"
	}

	boardYAML := fmt.Sprintf(`name: %s
prefix: INT
next_id: 1
%sstates:
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
verify:
  command: "true"
%s`, project, repoLine, remoteExec)

	if err := os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardYAML), 0o644); err != nil {
		t.Fatalf("write .board.yaml: %v", err)
	}

	mustRun(t, sc.boardsDir, "git", "add", ".")
	mustRun(t, sc.boardsDir, "git", "commit", "-m", "init harness boards")
}

func randomHex(t *testing.T, n int) string {
	t.Helper()

	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("randomHex: %v", err)
	}

	return hex.EncodeToString(b)
}

// mustRun runs name+args in dir (cwd when dir is empty), failing the test on a
// non-zero exit. Output is streamed to the harness stderr for diagnostics.
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s %v: %v", name, args, err)
	}
}
