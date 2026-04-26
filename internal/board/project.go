package board

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// RemoteExecutionConfig controls per-project remote execution settings.
type RemoteExecutionConfig struct {
	Enabled     *bool  `yaml:"enabled,omitempty"      json:"enabled,omitempty"`
	RunnerImage string `yaml:"runner_image,omitempty"  json:"runner_image,omitempty"`
}

// GitHubImportConfig controls per-project GitHub issue import settings.
type GitHubImportConfig struct {
	ImportIssues    bool     `yaml:"import_issues"              json:"import_issues"`
	Owner           string   `yaml:"owner,omitempty"            json:"owner,omitempty"`
	Repo            string   `yaml:"repo,omitempty"             json:"repo,omitempty"`
	CardType        string   `yaml:"card_type,omitempty"        json:"card_type,omitempty"`
	DefaultPriority string   `yaml:"default_priority,omitempty" json:"default_priority,omitempty"`
	Labels          []string `yaml:"labels,omitempty"           json:"labels,omitempty"`
}

// ProjectConfig represents the configuration of a project board.
// Stored in boards/{project}/.board.yaml.
type ProjectConfig struct {
	Name            string                 `yaml:"name" json:"name"`
	DisplayName     string                 `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Prefix          string                 `yaml:"prefix" json:"prefix"`
	NextID          int                    `yaml:"next_id" json:"next_id"`
	Repo            string                 `yaml:"repo,omitempty" json:"repo,omitempty"`
	States          []string               `yaml:"states" json:"states"`
	Types           []string               `yaml:"types" json:"types"`
	Priorities      []string               `yaml:"priorities" json:"priorities"`
	Transitions     map[string][]string    `yaml:"transitions" json:"transitions"`
	RemoteExecution *RemoteExecutionConfig `yaml:"remote_execution,omitempty" json:"remote_execution,omitempty"`
	GitHub          *GitHubImportConfig    `yaml:"github,omitempty"           json:"github,omitempty"`
	DefaultSkills   *[]string              `yaml:"default_skills,omitempty"   json:"default_skills,omitempty"`
	Templates       map[string]string      `yaml:"-" json:"templates,omitempty"` // loaded from templates/ dir at runtime
}

var (
	// ErrProjectNotFound is returned when a .board.yaml file does not exist.
	ErrProjectNotFound = errors.New("project not found")
	// ErrMalformedProjectConfig is returned when .board.yaml cannot be parsed.
	ErrMalformedProjectConfig = errors.New("malformed project config")
	// ErrMissingStalledState is returned when config lacks required 'stalled' state.
	ErrMissingStalledState = errors.New("missing required 'stalled' state")
	// ErrMissingStalledTransitions is returned when transitions lack 'stalled' key.
	ErrMissingStalledTransitions = errors.New("missing required 'stalled' transitions")
	// ErrMissingNotPlannedState is returned when config lacks required 'not_planned' state.
	ErrMissingNotPlannedState = errors.New("missing required 'not_planned' state")
	// ErrMissingNotPlannedTransitions is returned when transitions lack 'not_planned' key.
	ErrMissingNotPlannedTransitions = errors.New("missing required 'not_planned' transitions")
	// ErrInvalidProjectConfig is returned for other validation failures.
	ErrInvalidProjectConfig = errors.New("invalid project config")
)

// Well-known state names used for system logic (auto-transitions,
// deferred-commit flush, timeout checker, etc.).
const (
	StateTodo       = "todo"
	StateInProgress = "in_progress"
	StateReview     = "review"
	StateDone       = "done"
	StateStalled    = "stalled"
	StateNotPlanned = "not_planned"
)

const (
	boardConfigFile   = ".board.yaml"
	templatesDir      = "templates"
	templateExtension = ".md"
)

// LoadProjectConfig reads a project's .board.yaml configuration.
// The dir parameter should be the project directory (e.g., "boards/project-alpha").
func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, boardConfigFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrProjectNotFound
		}

		return nil, fmt.Errorf("read project config: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedProjectConfig, err)
	}

	if err := validateProjectConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveProjectConfig writes a project's .board.yaml configuration.
// Creates the directory if it does not exist.
// Validates the config before writing.
func SaveProjectConfig(dir string, cfg *ProjectConfig) error {
	if err := validateProjectConfig(cfg); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}

	path := filepath.Join(dir, boardConfigFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write project config: %w", err)
	}

	return nil
}

// validateProjectConfig checks that the config meets all required invariants.
func validateProjectConfig(cfg *ProjectConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidProjectConfig)
	}

	if cfg.Prefix == "" {
		return fmt.Errorf("%w: prefix is required", ErrInvalidProjectConfig)
	}

	if cfg.NextID < 1 {
		return fmt.Errorf("%w: next_id must be >= 1", ErrInvalidProjectConfig)
	}

	// Check stalled state exists
	if !slices.Contains(cfg.States, StateStalled) {
		return ErrMissingStalledState
	}

	// Check stalled transition exists
	if _, ok := cfg.Transitions[StateStalled]; !ok {
		return ErrMissingStalledTransitions
	}

	// Check not_planned state exists
	if !slices.Contains(cfg.States, StateNotPlanned) {
		return ErrMissingNotPlannedState
	}

	// Check not_planned transition exists
	if _, ok := cfg.Transitions[StateNotPlanned]; !ok {
		return ErrMissingNotPlannedTransitions
	}

	// Validate transition targets exist in the state list (including built-in states).
	allStates := append(slices.Clone(cfg.States), StateStalled, StateNotPlanned)
	for fromState, targets := range cfg.Transitions {
		for _, target := range targets {
			if !slices.Contains(allStates, target) {
				return fmt.Errorf("%w: transition from %q targets non-existent state %q", ErrInvalidProjectConfig, fromState, target)
			}
		}
	}

	return nil
}

// GenerateCardID generates the next card ID for a project.
// Format: PREFIX-NNN, zero-padded to 3 digits minimum.
// Examples: ALPHA-001, ALPHA-042, ALPHA-999, ALPHA-1000
// IMPORTANT: Increments cfg.NextID - caller must save config after calling.
func GenerateCardID(cfg *ProjectConfig) string {
	id := cfg.NextID
	cfg.NextID++

	return fmt.Sprintf("%s-%03d", cfg.Prefix, id)
}

// LoadTemplates reads all .md files from the project's templates directory.
// Returns a map of type name (filename without .md) to template content.
// Returns empty map (not error) if templates directory doesn't exist.
func LoadTemplates(dir string) (map[string]string, error) {
	templates := make(map[string]string)

	templatesPath := filepath.Join(dir, templatesDir)

	entries, err := os.ReadDir(templatesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return templates, nil
		}

		return nil, fmt.Errorf("read templates directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, templateExtension) {
			continue
		}

		typeName := strings.TrimSuffix(name, templateExtension)
		filePath := filepath.Join(templatesPath, name)

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", name, err)
		}

		templates[typeName] = string(content)
	}

	return templates, nil
}

// DiscoverProjects scans the boards directory for all valid projects.
// Returns a slice of ProjectConfig for each subdirectory containing .board.yaml.
// Skips directories without .board.yaml (no error).
// Loads templates for each discovered project.
func DiscoverProjects(boardsDir string) ([]ProjectConfig, error) {
	entries, err := os.ReadDir(boardsDir)
	if err != nil {
		return nil, fmt.Errorf("read boards directory: %w", err)
	}

	var projects []ProjectConfig

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectPath := filepath.Join(boardsDir, entry.Name())

		cfg, err := LoadProjectConfig(projectPath)
		if err != nil {
			if errors.Is(err, ErrProjectNotFound) {
				continue
			}

			slog.Warn("skipping project with invalid config",
				"path", projectPath,
				"error", err,
			)

			continue
		}

		// Load templates for this project
		templates, err := LoadTemplates(projectPath)
		if err != nil {
			slog.Warn("skipping project with template errors",
				"project", cfg.Name,
				"error", err,
			)

			continue
		}

		cfg.Templates = templates

		projects = append(projects, *cfg)
	}

	return projects, nil
}
