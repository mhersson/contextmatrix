package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestKnowledgeMeta_RoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	meta := &KnowledgeMeta{
		SchemaVersion: 1,
		Repos: map[string]KnowledgeRepoMeta{
			"contextmatrix": {
				URL:             "git@github.com:o/contextmatrix.git",
				LastBuiltAt:     now,
				LastBuiltCommit: "a7bf68c",
				LastBuiltBy:     "human:morten",
				Docs: map[string]KnowledgeDocMeta{
					"architecture.md": {LastBuiltCommit: "a7bf68c", HumanEdited: false},
					"glossary.md":     {LastBuiltCommit: "a7bf68c", HumanEdited: true},
				},
			},
		},
	}

	data, err := yaml.Marshal(meta)
	require.NoError(t, err)

	var loaded KnowledgeMeta
	require.NoError(t, yaml.Unmarshal(data, &loaded))

	require.Contains(t, loaded.Repos, "contextmatrix")
	r := loaded.Repos["contextmatrix"]
	assert.Equal(t, "a7bf68c", r.LastBuiltCommit)
	assert.True(t, r.Docs["glossary.md"].HumanEdited)
	assert.False(t, r.Docs["architecture.md"].HumanEdited)
}

func TestKnowledgeDocNames(t *testing.T) {
	assert.ElementsMatch(t,
		[]string{"architecture.md", "code-structure.md", "api-documentation.md", "glossary.md"},
		KnowledgeDocNames,
	)
}

func TestIsValidKnowledgeDoc(t *testing.T) {
	assert.True(t, IsValidKnowledgeDoc("architecture.md"))
	assert.False(t, IsValidKnowledgeDoc("../etc/passwd"))
	assert.False(t, IsValidKnowledgeDoc("random.md"))
}
