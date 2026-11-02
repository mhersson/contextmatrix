package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/config"
)

func TestWireKnowledge(t *testing.T) {
	providers := []config.KnowledgeProvider{{Name: "intent", URL: "https://knowledge.example/mcp", Token: "secret-token"}}

	t.Run("no providers", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state", "knowledge.log")

		svc, err := wireKnowledge(config.KnowledgeConfig{LogPath: path})
		require.NoError(t, err)
		assert.Nil(t, svc)

		_, statErr := os.Stat(path)
		assert.ErrorIs(t, statErr, os.ErrNotExist, "no audit log without providers")
	})

	t.Run("one provider", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state", "knowledge.log")

		svc, err := wireKnowledge(config.KnowledgeConfig{LogPath: path, Providers: providers})
		require.NoError(t, err)
		require.NotNil(t, svc)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		require.NoError(t, svc.Close())
	})

	t.Run("unusable log path", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))

		svc, err := wireKnowledge(config.KnowledgeConfig{LogPath: filepath.Join(file, "knowledge.log"), Providers: providers})
		require.Error(t, err)
		assert.Nil(t, svc)
		assert.NotContains(t, err.Error(), "secret-token")
	})
}
