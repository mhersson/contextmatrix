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
	scenarioID  string
	tmpDir      string
	boardsDir   string
	cmPort      int
	runnerPort  int
	apiKey      string // HMAC shared secret
	mcpAPIKey   string // MCP bearer token
	workerImage string // cm-stub-orchestrated:test or cm-orchestrated:test
}

func newScenarioConfig(t *testing.T, scenarioID string, realClaude bool) *scenarioConfig {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	if err := os.MkdirAll(boardsDir, 0o755); err != nil {
		t.Fatalf("mkdir boards: %v", err)
	}

	image := "cm-stub-orchestrated:test"
	if realClaude {
		image = "cm-orchestrated:test"
	}

	return &scenarioConfig{
		scenarioID:  scenarioID,
		tmpDir:      tmpDir,
		boardsDir:   boardsDir,
		cmPort:      freePort(t),
		runnerPort:  freePort(t),
		apiKey:      randomHex(t, 32),
		mcpAPIKey:   randomHex(t, 32),
		workerImage: image,
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
cors_origin: http://127.0.0.1:0
theme: everforest
github:
  auth_mode: pat
  pat:
    token: harness-not-used
`, sc.cmPort, sc.mcpAPIKey, sc.boardsDir, sc.runnerPort, sc.apiKey)
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

	body := fmt.Sprintf(`port: %d
log_format: text
log_level: debug
contextmatrix_url: http://127.0.0.1:%d
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
github:
  auth_mode: pat
  pat:
    token: not-used-by-stub
%s`, sc.runnerPort, sc.cmPort, sc.apiKey, sc.workerImage, sc.workerImage, sc.workerImage, credsBlock)

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
