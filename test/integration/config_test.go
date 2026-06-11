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
	scenarioID               string
	tmpDir                   string
	boardsDir                string
	secretsDir               string // host path the runner stages per-container secrets files in
	cmPort                   int
	runnerPort               int
	apiKey                   string // HMAC shared secret
	mcpAPIKey                string // MCP bearer token
	workerImage              string // cm-stub-legacy:test
	heartbeatTimeoutSeconds  int    // 0 = use CM default (30m)
	stalledCheckSeconds      int    // 0 = use CM default (1m)
	idleWatchdogSeconds      int    // 0 = use runner default
	idleOutputTimeoutSeconds int    // 0 = use hardcoded default (90s)
}

func newScenarioConfig(t *testing.T, scenarioID string) *scenarioConfig {
	t.Helper()

	tmpDir := t.TempDir()

	boardsDir := filepath.Join(tmpDir, "boards")
	if err := os.MkdirAll(boardsDir, 0o755); err != nil {
		t.Fatalf("mkdir boards: %v", err)
	}

	// The default runner SecretsDir lives at /var/run/cm-runner/secrets,
	// which only root can create. Override to a per-test tmp path so the
	// dispatcher can stage worker secrets files without root.
	secretsDir := filepath.Join(tmpDir, "cm-secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir cm-secrets: %v", err)
	}

	return &scenarioConfig{
		scenarioID:  scenarioID,
		tmpDir:      tmpDir,
		boardsDir:   boardsDir,
		secretsDir:  secretsDir,
		cmPort:      freePort(t),
		runnerPort:  freePort(t),
		apiKey:      randomHex(t, 32),
		mcpAPIKey:   randomHex(t, 32),
		workerImage: "cm-stub-legacy:test",
	}
}

func (sc *scenarioConfig) writeCMConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(sc.tmpDir, "cm-config.yaml")

	heartbeatLine := ""
	if sc.heartbeatTimeoutSeconds > 0 {
		heartbeatLine = fmt.Sprintf("heartbeat_timeout: %ds", sc.heartbeatTimeoutSeconds)
	}

	stalledCheckLine := ""
	if sc.stalledCheckSeconds > 0 {
		stalledCheckLine = fmt.Sprintf("stalled_check_interval: %ds", sc.stalledCheckSeconds)
	}

	// chat.db_path and images.db_path must point into the scenario tmp dir:
	// without them CM falls back to the developer's real XDG state databases,
	// making the harness non-hermetic (and failing outright when the shared
	// chats.db predates the current schema).
	// backends.runner: roles and callback path (/api/runner) are derived from
	// the entry name; no selector fields or callback_path needed.
	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
mcp_api_key: %q
boards:
  dir: %s
  git_auto_commit: true
backends:
  runner:
    url: http://127.0.0.1:%d
    api_key: %q
chat:
  db_path: %s
images:
  db_path: %s
%s
%s
cors_origin: http://127.0.0.1:0
theme: everforest
github:
  auth_mode: pat
  pat:
    token: harness-not-used
`, sc.cmPort, sc.mcpAPIKey, sc.boardsDir, sc.runnerPort, sc.apiKey,
		filepath.Join(sc.tmpDir, "chats.db"), filepath.Join(sc.tmpDir, "images.db"),
		heartbeatLine, stalledCheckLine)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write CM config: %v", err)
	}

	return path
}

func (sc *scenarioConfig) writeRunnerConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(sc.tmpDir, "runner-config.yaml")

	idleOutputTimeoutLine := "idle_output_timeout: 90s"
	if sc.idleOutputTimeoutSeconds > 0 {
		idleOutputTimeoutLine = fmt.Sprintf("idle_output_timeout: %ds", sc.idleOutputTimeoutSeconds)
	}

	idleWatchdogLine := ""
	if sc.idleWatchdogSeconds > 0 {
		idleWatchdogLine = fmt.Sprintf("\nidle_watchdog_interval: %ds", sc.idleWatchdogSeconds)
	}

	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
contextmatrix_url: http://127.0.0.1:%d
container_contextmatrix_url: http://host.docker.internal:%d
api_key: %q
deployment_profile: dev
base_image: %s
allowed_images:
  - %s
image_pull_policy: never
container_timeout: 30m
%s
maintenance_interval: 60s
webhook_replay_cache_size: 64
webhook_replay_skew_seconds: 30
secrets_dir: %s
github:
  auth_mode: pat
  pat:
    token: not-used-by-stub
anthropic_api_key: stub-not-used
%s`, sc.runnerPort, sc.cmPort, sc.cmPort, sc.apiKey, sc.workerImage, sc.workerImage, idleOutputTimeoutLine, sc.secretsDir, idleWatchdogLine)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write runner config: %v", err)
	}

	return path
}

func randomHex(t *testing.T, n int) string {
	t.Helper()

	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("randomHex: %v", err)
	}

	return hex.EncodeToString(b)
}
