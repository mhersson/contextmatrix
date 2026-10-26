package config

import (
	"bytes"
	"reflect"
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
	require.NotNil(t, back.Backends.Architect)
	assert.False(t, back.Backends.Architect.IsEnabled())
	assert.Equal(t, ResearchOff, back.Backends.Architect.Research)
	assert.Equal(t, ResearchProviderBrave, back.Backends.Architect.ResearchProvider)
	assert.Contains(t, text, "research_api_key:")
	assert.Equal(t, "60s", back.Backends.Agent.ReconcileInterval)

	// Boards print in the single mapping form, not the list form. Decoding
	// into a generic tree says that without pinning the encoder's indent.
	var tree map[string]any
	require.NoError(t, yaml.Unmarshal(out, &tree))

	boards, ok := tree["boards"].(map[string]any)
	require.True(t, ok, "boards must print as a mapping, got %T", tree["boards"])
	assert.Equal(t, "boards", boards["name"])

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

// TestDefaultsCoverLoadDefaults guards drift between Load and Defaults: every
// non-zero value Load yields for a minimal file must also be printed by
// Defaults, or contextmatrix-setup would never sync that key.
func TestDefaultsCoverLoadDefaults(t *testing.T) {
	boardsDir := t.TempDir()
	path := writeConfigFile(t, t.TempDir(),
		"boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: pat\n  pat:\n    token: ghp_test\n  issue_importing:\n    enabled: true\n")

	loaded, err := Load(path)
	require.NoError(t, err)

	got := flattenYAML(t, loaded)
	want := flattenYAML(t, Defaults())

	// Set by the minimal file itself, or derived from the host at load time.
	skip := map[string]bool{
		"boards.dir":                     true, // set by the file
		"github.auth_mode":               true, // set by the file
		"github.pat.token":               true, // set by the file
		"github.issue_importing.enabled": true, // set by the file so Validate applies sync_interval
		"auth.db_path":                   true, // resolved beside the config file
		"auth.master_key_file":           true, // resolved beside the config file
		"images.db_path":                 true, // resolved beside the config file
		"op_store.db_path":               true, // resolved beside the config file
		"workflow_skills_dir":            true, // resolved relative to the config file
		"task_skills.dir":                true, // resolved relative to the config file
	}

	for k, v := range got {
		if skip[k] {
			continue
		}

		assert.Equal(t, v, want[k], "Load applies %s but Defaults() does not print it", k)
	}
}

// flattenYAML marshals v and returns its leaves keyed by dotted path, with
// zero values (empty, false, 0, null) dropped.
func flattenYAML(t *testing.T, v any) map[string]any {
	t.Helper()

	out, err := yaml.Marshal(v)
	require.NoError(t, err)

	var tree map[string]any
	require.NoError(t, yaml.Unmarshal(out, &tree))

	flat := map[string]any{}

	var walk func(prefix string, n any)

	walk = func(prefix string, n any) {
		switch x := n.(type) {
		case map[string]any:
			for k, c := range x {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}

				walk(key, c)
			}
		default:
			if x != nil && !reflect.ValueOf(x).IsZero() {
				flat[prefix] = x
			}
		}
	}

	walk("", tree)

	return flat
}
