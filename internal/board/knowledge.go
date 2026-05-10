package board

import "time"

// KnowledgeDocNames is the canonical list of doc filenames.
var KnowledgeDocNames = []string{
	"architecture.md",
	"code-structure.md",
	"api-documentation.md",
	"glossary.md",
}

// IsValidKnowledgeDoc reports whether name is one of the canonical KB doc names.
// Used to reject path-traversal attempts and unknown doc names at the API boundary.
func IsValidKnowledgeDoc(name string) bool {
	for _, n := range KnowledgeDocNames {
		if name == n {
			return true
		}
	}

	return false
}

// KnowledgeMeta is the on-disk shape of <project>/knowledge/.meta.yaml.
type KnowledgeMeta struct {
	SchemaVersion int                          `yaml:"schema_version" json:"schema_version"`
	Repos         map[string]KnowledgeRepoMeta `yaml:"repos" json:"repos"`
}

// KnowledgeRepoMeta tracks build state for one repo's KB.
type KnowledgeRepoMeta struct {
	URL             string                      `yaml:"url" json:"url"`
	LastBuiltAt     time.Time                   `yaml:"last_built_at" json:"last_built_at"`
	LastBuiltCommit string                      `yaml:"last_built_commit" json:"last_built_commit"`
	LastBuiltBy     string                      `yaml:"last_built_by" json:"last_built_by"`
	Docs            map[string]KnowledgeDocMeta `yaml:"docs" json:"docs"`
}

// KnowledgeDocMeta tracks per-doc build state.
type KnowledgeDocMeta struct {
	LastBuiltCommit string `yaml:"last_built_commit" json:"last_built_commit"`
	HumanEdited     bool   `yaml:"human_edited" json:"human_edited"`
}
