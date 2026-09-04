package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigCLI_UsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer

	assert.Equal(t, 2, configCLI(nil, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "usage: contextmatrix config")

	stderr.Reset()
	assert.Equal(t, 2, configCLI([]string{"bogus"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), `unknown subcommand "bogus"`)

	stderr.Reset()
	assert.Equal(t, 2, configCLI([]string{"validate"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "validate <file>")
}

func TestConfigCLI_DefaultsPrintsStrictYAML(t *testing.T) {
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, configCLI([]string{"defaults"}, &stdout, &stderr), stderr.String())
	assert.Empty(t, stderr.String())

	var tree map[string]any
	require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &tree))

	for _, key := range []string{"port", "boards", "backends", "auth", "github", "mcp_api_key", "task_skills"} {
		assert.Contains(t, tree, key)
	}

	backends, ok := tree["backends"].(map[string]any)
	require.True(t, ok, "backends must be a mapping")
	assert.Contains(t, backends, "agent")
	assert.Contains(t, backends, "chat")
}

func TestConfigCLI_ValidateAcceptsLoadableFile(t *testing.T) {
	cfgPath, dbPath, keyPath := writeAuthConfig(t, "none")

	var stdout, stderr bytes.Buffer

	assert.Equal(t, 0, configCLI([]string{"validate", cfgPath}, &stdout, &stderr), stderr.String())
	assert.Contains(t, stdout.String(), "ok")

	// validate creates no state: the auth store and master key the config
	// names must still be absent afterwards.
	assert.NoFileExists(t, dbPath)
	assert.NoFileExists(t, keyPath)
}

func TestConfigCLI_ValidateReportsLoaderError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "boards:\n  dir: " + filepath.Join(dir, "boards") + "\ngithub:\n  auth_mode: bogus\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o600))

	var stdout, stderr bytes.Buffer

	assert.Equal(t, 1, configCLI([]string{"validate", cfgPath}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "github.auth_mode")
	assert.Empty(t, stdout.String())
}

func TestConfigCLI_ValidateMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer

	assert.Equal(t, 1, configCLI([]string{"validate", filepath.Join(t.TempDir(), "nope.yaml")}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "nope.yaml")
}
