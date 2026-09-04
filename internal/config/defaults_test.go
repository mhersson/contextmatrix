package config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestDefaultsRoundTrip guards the installer contract: the printed defaults
// must contain every key the loader accepts, decode back with KnownFields,
// and leave host-dependent paths empty.
func TestDefaultsRoundTrip(t *testing.T) {
	out, err := yaml.Marshal(Defaults())
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(out))
	dec.KnownFields(true)

	var back Config
	require.NoError(t, dec.Decode(&back), "defaults must decode strictly:\n%s", out)

	text := string(out)

	// Backends are present, complete, and disabled.
	assert.Contains(t, text, "backends:\n")
	assert.Contains(t, text, "aa_api_key:")
	assert.Contains(t, text, "default_model:")
	require.NotNil(t, back.Backends.Agent)
	require.NotNil(t, back.Backends.Chat)
	assert.False(t, back.Backends.Agent.IsEnabled())
	assert.False(t, back.Backends.Chat.IsEnabled())
	assert.Equal(t, "60s", back.Backends.Agent.ReconcileInterval)

	// Boards print in the single mapping form. Indent width follows yaml.v3's
	// default encoder (4 spaces), not the brief's illustrative 2.
	assert.Contains(t, text, "boards:\n    name: boards\n")
	assert.NotContains(t, text, "boards:\n- ")

	// Free-form maps print empty, not null.
	assert.Contains(t, text, "token_costs: {}")
	assert.NotContains(t, text, ": null")

	// Host-dependent paths stay empty for the installer to fill.
	assert.Empty(t, back.Auth.DBPath)
	assert.Empty(t, back.Auth.MasterKeyFile)
	assert.Empty(t, back.Images.DBPath)
	assert.Empty(t, back.OpStore.DBPath)

	// Non-path defaults match what Load applies.
	assert.Equal(t, 8080, back.Port)
	assert.Equal(t, AuthModeMulti, back.Auth.Mode)
	assert.Equal(t, 40000, back.Chat.ResumeBudgetTokens)
	assert.Equal(t, 5, back.Mob.MaxParticipants)
}
