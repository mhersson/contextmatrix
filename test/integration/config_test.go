//go:build integration

package integration_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// scenarioConfig holds everything needed to generate a CM + runner config
// pair for one scenario.
type scenarioConfig struct {
	scenarioID    string
	tmpDir        string
	boardsDir     string
	taskSkillsDir string // host path to the skills fixture; same path passed to CM and runner
	cmPort        int
	runnerPort    int
	apiKey        string // HMAC shared secret
	mcpAPIKey     string // MCP bearer token
	workerImage   string // cm-stub-orchestrated:test or cm-orchestrated:test
}

func newScenarioConfig(t *testing.T, scenarioID string, realClaude bool) *scenarioConfig {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	if err := os.MkdirAll(boardsDir, 0o755); err != nil {
		t.Fatalf("mkdir boards: %v", err)
	}

	skillsDir := filepath.Join(tmpDir, "task-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir task-skills: %v", err)
	}

	image := "cm-stub-orchestrated:test"
	if realClaude {
		image = "cm-orchestrated:test"
	}

	return &scenarioConfig{
		scenarioID:    scenarioID,
		tmpDir:        tmpDir,
		boardsDir:     boardsDir,
		taskSkillsDir: skillsDir,
		cmPort:        freePort(t),
		runnerPort:    freePort(t),
		apiKey:        randomHex(t, 32),
		mcpAPIKey:     randomHex(t, 32),
		workerImage:   image,
	}
}

func (sc *scenarioConfig) writeCMConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(sc.tmpDir, "cm-config.yaml")
	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
mcp_api_key: %q
boards:
  dir: %s
  git_auto_commit: true
runner:
  enabled: true
  url: http://127.0.0.1:%d
  api_key: %q
  orchestrator_sonnet_model: claude-haiku-4-5
  orchestrator_opus_model: claude-haiku-4-5
heartbeat_timeout: 5m
task_skills:
  dir: %s
cors_origin: http://127.0.0.1:0
theme: everforest
github:
  auth_mode: pat
  pat:
    token: harness-not-used
`, sc.cmPort, sc.mcpAPIKey, sc.boardsDir, sc.runnerPort, sc.apiKey, sc.taskSkillsDir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write CM config: %v", err)
	}
	return path
}

func (sc *scenarioConfig) writeRunnerConfig(t *testing.T, claudeCreds *claudeCredentials) string {
	t.Helper()
	path := filepath.Join(sc.tmpDir, "runner-config.yaml")

	// Build an optional Claude-credential block for real-Claude mode.
	// Stub mode falls back to a placeholder anthropic_api_key — the runner's
	// Validate() requires at least one Claude auth field, but the stub worker
	// never calls Anthropic so the key is never used.
	credsBlock := "anthropic_api_key: stub-not-used\n"
	if claudeCreds != nil {
		credsBlock = claudeCreds.runnerYAMLFragment()
	}

	// In real-Claude mode the harness fronts the fixture bare repo with a
	// self-signed HTTPS server (githttps_test.go); the worker container's
	// git client must skip cert verification to clone+push against it.
	// Stub mode never hits the fixture so the env var is harmless either
	// way; we only emit it when claudeCreds != nil to keep stub configs
	// minimal.
	extraEnvBlock := ""
	if claudeCreds != nil {
		extraEnvBlock = "worker_extra_env:\n  GIT_SSL_NO_VERIFY: \"1\"\n"
	}

	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
contextmatrix_url: http://127.0.0.1:%d
container_contextmatrix_url: http://host.docker.internal:%d
api_key: %q
deployment_profile: dev
base_image: %s
agent_image: %s
allowed_images:
  - %s
image_pull_policy: never
container_timeout: 10m
idle_output_timeout: 90s
maintenance_interval: 60s
webhook_replay_cache_size: 64
webhook_replay_skew_seconds: 30
message_dedup_cache_size: 64
message_dedup_ttl_seconds: 60
worker_type: cc-orchestrated
task_skills_dir: %s
github:
  auth_mode: pat
  pat:
    token: not-used-by-stub
%s%s`, sc.runnerPort, sc.cmPort, sc.cmPort, sc.apiKey, sc.workerImage, sc.workerImage, sc.workerImage, sc.taskSkillsDir, credsBlock, extraEnvBlock)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write runner config: %v", err)
	}
	return path
}

// canarySkillName is the unique skill the harness provisions and asserts
// gets engaged. Picked to NOT collide with any built-in or plugin-bundled
// skill name (Claude Code ships go-development, documentation, etc.) so a
// post-run skill_engaged entry with this exact name proves the agent
// resolved it from the runner's task_skills_dir mount, not from some
// other source the harness can't control.
const canarySkillName = "harness-canary-skill"

// writeCanarySkill creates a minimal harness-canary-skill at
// sc.taskSkillsDir/<name>/SKILL.md so the worker has something to copy
// into ~/.claude/skills/. The description directs the agent to engage
// it whenever it sees the SYSINFO-CANARY marker, so the canary card
// body deterministically triggers engagement during the execute phase.
func (sc *scenarioConfig) writeCanarySkill(t *testing.T) {
	t.Helper()

	skillDir := filepath.Join(sc.taskSkillsDir, canarySkillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s skill: %v", canarySkillName, err)
	}

	body := `---
name: harness-canary-skill
description: Use exclusively when working on a card whose body contains the SYSINFO-CANARY-* marker. Provides required guidance for the integration harness sysinfo-server canary task. ALWAYS engage this skill before writing any code on a SYSINFO-CANARY task.
---

# Harness Canary Skill

You are working on the integration harness sysinfo-server canary. This skill
records that the runner's filesystem-skill mounting path successfully
delivered task-specific guidance into the worker container.

## Required actions

When implementing the SYSINFO-CANARY task:

- Use only the Go standard library (net/http, encoding/json, os, runtime).
- Set Content-Type: application/json on the GET / response before writing
  the body.
- Reject non-GET methods with 405 Method Not Allowed.
- Write a main_test.go using net/http/httptest that asserts status 200,
  Content-Type, and the presence of every sysinfo field.
- Place the literal "// SYSINFO-CANARY-<uuid>" comment as the very first
  line of main.go.

## Why this skill matters

This skill is provisioned by the integration harness via task_skills_dir.
A skill_engaged activity-log entry with skill="harness-canary-skill"
proves end-to-end: trigger payload → mount → entrypoint copy → agent
discovery → Skill tool invocation → runner-side detection → CM activity log.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("randomHex: %v", err)
	}
	return hex.EncodeToString(b)
}
