package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validProjectConfig() *ProjectConfig {
	return &ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		Repo:       "git@github.com:org/test-project.git",
		States:     []string{"todo", "in_progress", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
		},
	}
}

func minimalProjectConfig() *ProjectConfig {
	return &ProjectConfig{
		Name:       "minimal",
		Prefix:     "MIN",
		NextID:     1,
		States:     []string{"todo", "stalled"},
		Types:      []string{"task"},
		Priorities: []string{"medium"},
		Transitions: map[string][]string{
			"todo":    {},
			"stalled": {"todo"},
		},
	}
}

func TestLoadSaveProjectConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := validProjectConfig()

	err := SaveProjectConfig(dir, original)
	require.NoError(t, err)

	loaded, err := LoadProjectConfig(dir)
	require.NoError(t, err)

	assert.Equal(t, original.Name, loaded.Name)
	assert.Equal(t, original.Prefix, loaded.Prefix)
	assert.Equal(t, original.NextID, loaded.NextID)
	assert.Equal(t, original.Repo, loaded.Repo)
	assert.Equal(t, original.States, loaded.States)
	assert.Equal(t, original.Types, loaded.Types)
	assert.Equal(t, original.Priorities, loaded.Priorities)
	assert.Equal(t, original.Transitions, loaded.Transitions)
}

func TestLoadSaveProjectConfig_MinimalConfig(t *testing.T) {
	dir := t.TempDir()
	original := minimalProjectConfig()

	err := SaveProjectConfig(dir, original)
	require.NoError(t, err)

	loaded, err := LoadProjectConfig(dir)
	require.NoError(t, err)

	assert.Equal(t, original.Name, loaded.Name)
	assert.Equal(t, original.Prefix, loaded.Prefix)
	assert.Equal(t, original.NextID, loaded.NextID)
	assert.Empty(t, loaded.Repo)
	assert.Equal(t, original.States, loaded.States)
	assert.Equal(t, original.Types, loaded.Types)
	assert.Equal(t, original.Priorities, loaded.Priorities)
}

func TestLoadProjectConfig_NotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadProjectConfig(dir)
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestLoadProjectConfig_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".board.yaml")

	err := os.WriteFile(path, []byte("invalid: yaml: content: ["), 0644)
	require.NoError(t, err)

	_, err = LoadProjectConfig(dir)
	assert.ErrorIs(t, err, ErrMalformedProjectConfig)
}

func TestLoadProjectConfig_MissingStalledState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".board.yaml")

	yaml := `name: test
prefix: TEST
next_id: 1
states: [todo, done]
types: [task]
priorities: [medium]
transitions:
  todo: [done]
  done: [todo]
`
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	_, err = LoadProjectConfig(dir)
	assert.ErrorIs(t, err, ErrMissingStalledState)
}

func TestLoadProjectConfig_MissingStalledTransitions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".board.yaml")

	yaml := `name: test
prefix: TEST
next_id: 1
states: [todo, done, stalled]
types: [task]
priorities: [medium]
transitions:
  todo: [done]
  done: [todo]
`
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	_, err = LoadProjectConfig(dir)
	assert.ErrorIs(t, err, ErrMissingStalledTransitions)
}

func TestSaveProjectConfig_ValidatesBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	cfg := &ProjectConfig{
		Name:   "", // Invalid: empty name
		Prefix: "TEST",
		NextID: 1,
		States: []string{"stalled"},
		Transitions: map[string][]string{
			"stalled": {},
		},
	}

	err := SaveProjectConfig(dir, cfg)
	assert.ErrorIs(t, err, ErrInvalidProjectConfig)

	// File should not have been created
	_, err = os.Stat(filepath.Join(dir, ".board.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestValidateProjectConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*ProjectConfig)
		expectedErr error
	}{
		{
			name:        "valid config",
			modify:      func(cfg *ProjectConfig) {},
			expectedErr: nil,
		},
		{
			name:        "empty name",
			modify:      func(cfg *ProjectConfig) { cfg.Name = "" },
			expectedErr: ErrInvalidProjectConfig,
		},
		{
			name:        "empty prefix",
			modify:      func(cfg *ProjectConfig) { cfg.Prefix = "" },
			expectedErr: ErrInvalidProjectConfig,
		},
		{
			name:        "zero next_id",
			modify:      func(cfg *ProjectConfig) { cfg.NextID = 0 },
			expectedErr: ErrInvalidProjectConfig,
		},
		{
			name:        "negative next_id",
			modify:      func(cfg *ProjectConfig) { cfg.NextID = -5 },
			expectedErr: ErrInvalidProjectConfig,
		},
		{
			name: "missing stalled state",
			modify: func(cfg *ProjectConfig) {
				cfg.States = []string{"todo", "done"}
			},
			expectedErr: ErrMissingStalledState,
		},
		{
			name: "missing stalled transitions",
			modify: func(cfg *ProjectConfig) {
				delete(cfg.Transitions, "stalled")
			},
			expectedErr: ErrMissingStalledTransitions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProjectConfig()
			tt.modify(cfg)

			err := validateProjectConfig(cfg)
			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.expectedErr)
			}
		})
	}
}

func TestGenerateCardID_Padding(t *testing.T) {
	tests := []struct {
		name     string
		nextID   int
		prefix   string
		expected string
	}{
		{name: "single digit", nextID: 1, prefix: "ALPHA", expected: "ALPHA-001"},
		{name: "double digit", nextID: 42, prefix: "ALPHA", expected: "ALPHA-042"},
		{name: "triple digit", nextID: 999, prefix: "ALPHA", expected: "ALPHA-999"},
		{name: "four digits", nextID: 1000, prefix: "ALPHA", expected: "ALPHA-1000"},
		{name: "large number", nextID: 12345, prefix: "TEST", expected: "TEST-12345"},
		{name: "short prefix", nextID: 1, prefix: "A", expected: "A-001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ProjectConfig{
				NextID: tt.nextID,
				Prefix: tt.prefix,
			}

			id := GenerateCardID(cfg)
			assert.Equal(t, tt.expected, id)
		})
	}
}

func TestGenerateCardID_IncrementsNextID(t *testing.T) {
	cfg := &ProjectConfig{
		NextID: 5,
		Prefix: "TEST",
	}

	id1 := GenerateCardID(cfg)
	assert.Equal(t, "TEST-005", id1)
	assert.Equal(t, 6, cfg.NextID)

	id2 := GenerateCardID(cfg)
	assert.Equal(t, "TEST-006", id2)
	assert.Equal(t, 7, cfg.NextID)
}

func TestGenerateCardID_Rollover999To1000(t *testing.T) {
	cfg := &ProjectConfig{
		NextID: 999,
		Prefix: "TEST",
	}

	id1 := GenerateCardID(cfg)
	assert.Equal(t, "TEST-999", id1)
	assert.Equal(t, 1000, cfg.NextID)

	id2 := GenerateCardID(cfg)
	assert.Equal(t, "TEST-1000", id2)
	assert.Equal(t, 1001, cfg.NextID)
}

func TestLoadTemplates_AllTypes(t *testing.T) {
	dir := t.TempDir()
	templatesPath := filepath.Join(dir, "templates")
	err := os.MkdirAll(templatesPath, 0755)
	require.NoError(t, err)

	// Create template files
	templateContents := map[string]string{
		"task":    "# Task Template\n\n## Description\n",
		"bug":     "# Bug Report\n\n## Steps to Reproduce\n",
		"feature": "# Feature Request\n\n## User Story\n",
	}

	for name, content := range templateContents {
		path := filepath.Join(templatesPath, name+".md")
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	templates, err := LoadTemplates(dir)
	require.NoError(t, err)

	assert.Len(t, templates, 3)
	for name, expectedContent := range templateContents {
		assert.Equal(t, expectedContent, templates[name])
	}
}

func TestLoadTemplates_NoTemplatesDir(t *testing.T) {
	dir := t.TempDir()

	templates, err := LoadTemplates(dir)
	require.NoError(t, err)
	assert.Empty(t, templates)
}

func TestLoadTemplates_EmptyTemplatesDir(t *testing.T) {
	dir := t.TempDir()
	err := os.MkdirAll(filepath.Join(dir, "templates"), 0755)
	require.NoError(t, err)

	templates, err := LoadTemplates(dir)
	require.NoError(t, err)
	assert.Empty(t, templates)
}

func TestLoadTemplates_IgnoresNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	templatesPath := filepath.Join(dir, "templates")
	err := os.MkdirAll(templatesPath, 0755)
	require.NoError(t, err)

	// Create various files
	files := map[string]string{
		"task.md":    "# Task",
		"readme.txt": "This is a readme",
		"draft.bak":  "backup file",
	}

	for name, content := range files {
		path := filepath.Join(templatesPath, name)
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	templates, err := LoadTemplates(dir)
	require.NoError(t, err)

	assert.Len(t, templates, 1)
	assert.Equal(t, "# Task", templates["task"])
	assert.NotContains(t, templates, "readme")
	assert.NotContains(t, templates, "draft")
}

func TestLoadTemplates_IgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	templatesPath := filepath.Join(dir, "templates")
	err := os.MkdirAll(templatesPath, 0755)
	require.NoError(t, err)

	// Create a subdirectory and a file
	err = os.MkdirAll(filepath.Join(templatesPath, "subdir.md"), 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(templatesPath, "task.md"), []byte("# Task"), 0644)
	require.NoError(t, err)

	templates, err := LoadTemplates(dir)
	require.NoError(t, err)

	assert.Len(t, templates, 1)
	assert.Equal(t, "# Task", templates["task"])
}

func TestDiscoverProjects_MultipleProjects(t *testing.T) {
	boardsDir := t.TempDir()

	// Create 3 valid projects
	for _, name := range []string{"alpha", "beta", "gamma"} {
		projectDir := filepath.Join(boardsDir, name)
		cfg := &ProjectConfig{
			Name:       name,
			Prefix:     name[:1],
			NextID:     1,
			States:     []string{"todo", "stalled"},
			Types:      []string{"task"},
			Priorities: []string{"medium"},
			Transitions: map[string][]string{
				"todo":    {},
				"stalled": {"todo"},
			},
		}
		err := SaveProjectConfig(projectDir, cfg)
		require.NoError(t, err)
	}

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)

	assert.Len(t, projects, 3)

	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
	assert.Contains(t, names, "gamma")
}

func TestDiscoverProjects_SkipsInvalidDirectories(t *testing.T) {
	boardsDir := t.TempDir()

	// Create one valid project
	validDir := filepath.Join(boardsDir, "valid")
	cfg := validProjectConfig()
	err := SaveProjectConfig(validDir, cfg)
	require.NoError(t, err)

	// Create directory without .board.yaml
	err = os.MkdirAll(filepath.Join(boardsDir, "no-config"), 0755)
	require.NoError(t, err)

	// Create a regular file (not a directory)
	err = os.WriteFile(filepath.Join(boardsDir, "not-a-dir"), []byte("file"), 0644)
	require.NoError(t, err)

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)

	assert.Len(t, projects, 1)
	assert.Equal(t, "test-project", projects[0].Name)
}

func TestDiscoverProjects_EmptyBoardsDir(t *testing.T) {
	boardsDir := t.TempDir()

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestDiscoverProjects_LoadsTemplates(t *testing.T) {
	boardsDir := t.TempDir()

	// Create project with templates
	projectDir := filepath.Join(boardsDir, "with-templates")
	cfg := validProjectConfig()
	err := SaveProjectConfig(projectDir, cfg)
	require.NoError(t, err)

	// Add templates
	templatesPath := filepath.Join(projectDir, "templates")
	err = os.MkdirAll(templatesPath, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(templatesPath, "task.md"), []byte("# Task Template"), 0644)
	require.NoError(t, err)

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)

	require.Len(t, projects, 1)
	assert.NotNil(t, projects[0].Templates)
	assert.Equal(t, "# Task Template", projects[0].Templates["task"])
}

func TestDiscoverProjects_SkipsMalformedConfigs(t *testing.T) {
	boardsDir := t.TempDir()

	// Create one valid project
	validDir := filepath.Join(boardsDir, "valid")
	cfg := validProjectConfig()
	err := SaveProjectConfig(validDir, cfg)
	require.NoError(t, err)

	// Create project with malformed config
	malformedDir := filepath.Join(boardsDir, "malformed")
	err = os.MkdirAll(malformedDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(malformedDir, ".board.yaml"), []byte("invalid: [yaml"), 0644)
	require.NoError(t, err)

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)

	// Should only have the valid project
	assert.Len(t, projects, 1)
	assert.Equal(t, "test-project", projects[0].Name)
}

func TestDiscoverProjects_SkipsInvalidConfigs(t *testing.T) {
	boardsDir := t.TempDir()

	// Create one valid project
	validDir := filepath.Join(boardsDir, "valid")
	cfg := validProjectConfig()
	err := SaveProjectConfig(validDir, cfg)
	require.NoError(t, err)

	// Create project with missing stalled state (valid YAML, invalid config)
	invalidDir := filepath.Join(boardsDir, "invalid")
	err = os.MkdirAll(invalidDir, 0755)
	require.NoError(t, err)
	yaml := `name: invalid
prefix: INV
next_id: 1
states: [todo]
types: [task]
priorities: [medium]
transitions:
  todo: []
`
	err = os.WriteFile(filepath.Join(invalidDir, ".board.yaml"), []byte(yaml), 0644)
	require.NoError(t, err)

	projects, err := DiscoverProjects(boardsDir)
	require.NoError(t, err)

	// Should only have the valid project
	assert.Len(t, projects, 1)
	assert.Equal(t, "test-project", projects[0].Name)
}
