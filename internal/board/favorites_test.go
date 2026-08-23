package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTierFavorites_RoundTrip_PlainList(t *testing.T) {
	original := TierFavorites{
		All: []string{"gpt-4o", "claude-3-opus"},
	}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var recovered TierFavorites
	require.NoError(t, yaml.Unmarshal(data, &recovered))

	assert.Equal(t, original.All, recovered.All)
	assert.Nil(t, recovered.ByRole)
}

func TestTierFavorites_RoundTrip_ByRole(t *testing.T) {
	original := TierFavorites{
		ByRole: map[string][]string{
			"coder":    {"gpt-4o"},
			"reviewer": {"claude-3-opus"},
		},
	}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var recovered TierFavorites
	require.NoError(t, yaml.Unmarshal(data, &recovered))

	assert.Nil(t, recovered.All)
	assert.Equal(t, original.ByRole, recovered.ByRole)
}

func TestTierFavorites_RoundTrip_ProjectConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := &ProjectConfig{
		Name:        "test-project",
		Prefix:      "TEST",
		NextID:      1,
		States:      []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:       []string{"task"},
		Priorities:  []string{"medium"},
		Transitions: map[string][]string{"todo": {"in_progress"}, "stalled": {"todo"}, "not_planned": {"todo"}},
		Favorites: map[string]TierFavorites{
			"complex": {
				All: []string{"gpt-4o", "claude-3-opus"},
			},
			"simple": {
				ByRole: map[string][]string{
					"coder": {"gemini-2.0-flash"},
				},
			},
		},
	}

	require.NoError(t, SaveProjectConfig(dir, cfg))

	loaded, err := LoadProjectConfig(dir)
	require.NoError(t, err)

	complexFav, ok := loaded.Favorites["complex"]
	require.True(t, ok, "complex favorites must survive round-trip")
	assert.Equal(t, []string{"gpt-4o", "claude-3-opus"}, complexFav.All)
	assert.Nil(t, complexFav.ByRole)

	simpleFav, ok := loaded.Favorites["simple"]
	require.True(t, ok, "simple favorites must survive round-trip")
	assert.Equal(t, map[string][]string{"coder": {"gemini-2.0-flash"}}, simpleFav.ByRole)
	assert.Nil(t, simpleFav.All)
}
