//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// claudeCredentials holds whichever Claude credential we resolved.
// Exactly one of the three fields is non-empty.
type claudeCredentials struct {
	AuthDir    string
	OAuthToken string
	APIKey     string
}

// runnerYAMLFragment renders the credential as a YAML snippet suitable
// for appending to the runner config. Indented to align under the root
// keys.
func (c *claudeCredentials) runnerYAMLFragment() string {
	if c == nil {
		return ""
	}

	switch {
	case c.AuthDir != "":
		return fmt.Sprintf("claude_auth_dir: %q\n", c.AuthDir)
	case c.OAuthToken != "":
		return fmt.Sprintf("claude_oauth_token: %q\n", c.OAuthToken)
	case c.APIKey != "":
		return fmt.Sprintf("anthropic_api_key: %q\n", c.APIKey)
	}

	return ""
}

// resolveClaudeCredentials reads ~/.config/contextmatrix-runner/config.yaml
// then falls back to env vars. Returns an error if nothing is set so the
// caller can t.Skipf with a clear message.
func resolveClaudeCredentials() (*claudeCredentials, error) {
	if creds := credsFromEnv(); creds != nil {
		return creds, nil
	}

	if creds, err := credsFromRunnerConfig(); err == nil && creds != nil {
		return creds, nil
	}

	checked := []string{
		"~/.config/contextmatrix-runner/config.yaml (claude_auth_dir / claude_oauth_token / anthropic_api_key)",
		"$CMR_CLAUDE_AUTH_DIR",
		"$CMR_CLAUDE_OAUTH_TOKEN",
		"$CMR_ANTHROPIC_API_KEY",
	}

	return nil, fmt.Errorf("none of: %s", strings.Join(checked, ", "))
}

func credsFromEnv() *claudeCredentials {
	if v := os.Getenv("CMR_CLAUDE_AUTH_DIR"); v != "" {
		return &claudeCredentials{AuthDir: v}
	}

	if v := os.Getenv("CMR_CLAUDE_OAUTH_TOKEN"); v != "" {
		return &claudeCredentials{OAuthToken: v}
	}

	if v := os.Getenv("CMR_ANTHROPIC_API_KEY"); v != "" {
		return &claudeCredentials{APIKey: v}
	}

	return nil
}

func credsFromRunnerConfig() (*claudeCredentials, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(home, ".config", "contextmatrix-runner", "config.yaml")

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg struct {
		ClaudeAuthDir    string `yaml:"claude_auth_dir"`
		ClaudeOAuthToken string `yaml:"claude_oauth_token"`
		AnthropicAPIKey  string `yaml:"anthropic_api_key"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	switch {
	case cfg.ClaudeAuthDir != "":
		return &claudeCredentials{AuthDir: cfg.ClaudeAuthDir}, nil
	case cfg.ClaudeOAuthToken != "":
		return &claudeCredentials{OAuthToken: cfg.ClaudeOAuthToken}, nil
	case cfg.AnthropicAPIKey != "":
		return &claudeCredentials{APIKey: cfg.AnthropicAPIKey}, nil
	}

	return nil, nil
}
