package board

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  *VerifyConfig
		want bool
	}{
		{"nil", nil, true},
		{"empty struct", &VerifyConfig{}, true},
		{"command set", &VerifyConfig{Command: "make test"}, false},
		{"timeout set", &VerifyConfig{TimeoutSeconds: 300}, false},
		{"env set", &VerifyConfig{Env: []string{"JAVA_HOME"}}, false},
		{"empty env slice", &VerifyConfig{Env: []string{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.IsZero())
		})
	}
}

func TestResolveVerify(t *testing.T) {
	tests := []struct {
		name    string
		card    *VerifyConfig
		project *VerifyConfig
		want    *VerifyConfig
	}{
		{
			name: "both nil",
			want: nil,
		},
		{
			name: "card only",
			card: &VerifyConfig{Command: "go test ./...", TimeoutSeconds: 120, Env: []string{"CGO_ENABLED"}},
			want: &VerifyConfig{Command: "go test ./...", TimeoutSeconds: 120, Env: []string{"CGO_ENABLED"}},
		},
		{
			name:    "project only",
			project: &VerifyConfig{Command: "make test", TimeoutSeconds: 600},
			want:    &VerifyConfig{Command: "make test", TimeoutSeconds: 600},
		},
		{
			name:    "card command overrides project",
			card:    &VerifyConfig{Command: "go test ./..."},
			project: &VerifyConfig{Command: "make test", TimeoutSeconds: 600},
			want:    &VerifyConfig{Command: "go test ./...", TimeoutSeconds: 600},
		},
		{
			name:    "field mix: card timeout, project command and env",
			card:    &VerifyConfig{TimeoutSeconds: 90},
			project: &VerifyConfig{Command: "mvn verify", Env: []string{"JAVA_HOME"}},
			want:    &VerifyConfig{Command: "mvn verify", TimeoutSeconds: 90, Env: []string{"JAVA_HOME"}},
		},
		{
			name:    "card empty env slice overrides project env",
			card:    &VerifyConfig{Command: "go test", Env: []string{}},
			project: &VerifyConfig{Env: []string{"JAVA_HOME"}},
			want:    &VerifyConfig{Command: "go test", Env: []string{}},
		},
		{
			name:    "card timeout zero falls through to project",
			card:    &VerifyConfig{Command: "go test"},
			project: &VerifyConfig{TimeoutSeconds: 300},
			want:    &VerifyConfig{Command: "go test", TimeoutSeconds: 300},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveVerify(tt.card, tt.project)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveVerify_DoesNotMutateInputs(t *testing.T) {
	card := &VerifyConfig{TimeoutSeconds: 90}
	project := &VerifyConfig{Command: "make test", Env: []string{"JAVA_HOME"}}

	_ = ResolveVerify(card, project)

	assert.Empty(t, card.Command, "card must not be mutated")
	assert.Equal(t, "make test", project.Command, "project must not be mutated")
	assert.Equal(t, 90, card.TimeoutSeconds)
}

func TestVerifyConfig_ProjectYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()

	cfg := &ProjectConfig{
		Name:        "test-project",
		Prefix:      "TEST",
		NextID:      1,
		States:      []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:       []string{"task"},
		Priorities:  []string{"medium"},
		Transitions: map[string][]string{"todo": {"in_progress"}, "stalled": {"todo"}, "not_planned": {"todo"}},
		Verify:      &VerifyConfig{Command: "make test", TimeoutSeconds: 600, Env: []string{"JAVA_HOME"}},
	}

	require.NoError(t, SaveProjectConfig(dir, cfg))

	loaded, err := LoadProjectConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded.Verify)
	assert.Equal(t, "make test", loaded.Verify.Command)
	assert.Equal(t, 600, loaded.Verify.TimeoutSeconds)
	assert.Equal(t, []string{"JAVA_HOME"}, loaded.Verify.Env)
}

func TestVerifyConfig_CardYAMLRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	card := &Card{
		ID:       "TEST-001",
		Title:    "Card with verify",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Verify:   &VerifyConfig{Command: "go test ./...", TimeoutSeconds: 120, Env: []string{"CGO_ENABLED"}},
		Created:  now,
		Updated:  now,
		Body:     "body",
	}

	data, err := SerializeCard(card)
	require.NoError(t, err)

	parsed, err := ParseCard(data)
	require.NoError(t, err)
	require.NotNil(t, parsed.Verify)
	assert.Equal(t, "go test ./...", parsed.Verify.Command)
	assert.Equal(t, 120, parsed.Verify.TimeoutSeconds)
	assert.Equal(t, []string{"CGO_ENABLED"}, parsed.Verify.Env)
}

// TestVerifyConfig_EmptyEnvYAMLRoundTrip pins that a non-nil empty Env survives
// the card YAML round-trip distinguishably from an absent Env, so a card's
// "override to clear env" is not silently collapsed into "inherit" on reload.
func TestVerifyConfig_EmptyEnvYAMLRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	card := &Card{
		ID: "TEST-001", Title: "override to clear", Project: "p", Type: "task",
		State: "todo", Priority: "medium",
		Verify:  &VerifyConfig{Command: "go test ./...", Env: []string{}},
		Created: now, Updated: now, Body: "b",
	}

	data, err := SerializeCard(card)
	require.NoError(t, err)
	assert.Contains(t, string(data), "env: []", "explicit empty env must be written to YAML")

	parsed, err := ParseCard(data)
	require.NoError(t, err)
	require.NotNil(t, parsed.Verify)
	require.NotNil(t, parsed.Verify.Env, "empty env must round-trip as non-nil (override), not absent")
	assert.Empty(t, parsed.Verify.Env)

	// A card with no env at all must round-trip as nil (inherit), distinct
	// from the empty-override case above.
	inherit := &Card{
		ID: "TEST-002", Title: "inherit", Project: "p", Type: "task",
		State: "todo", Priority: "medium",
		Verify:  &VerifyConfig{Command: "go test ./..."},
		Created: now, Updated: now, Body: "b",
	}
	data, err = SerializeCard(inherit)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "env:", "absent env must not be written")

	parsed, err = ParseCard(data)
	require.NoError(t, err)
	require.NotNil(t, parsed.Verify)
	assert.Nil(t, parsed.Verify.Env, "absent env must round-trip as nil (inherit)")
}

func TestVerifyConfig_OmittedFromYAMLWhenNil(t *testing.T) {
	dir := t.TempDir()

	cfg := &ProjectConfig{
		Name:        "test-project",
		Prefix:      "TEST",
		NextID:      1,
		States:      []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:       []string{"task"},
		Priorities:  []string{"medium"},
		Transitions: map[string][]string{"todo": {"in_progress"}, "stalled": {"todo"}, "not_planned": {"todo"}},
	}

	require.NoError(t, SaveProjectConfig(dir, cfg))

	data, err := os.ReadFile(filepath.Join(dir, boardConfigFile))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "verify:", "nil verify must be omitted from .board.yaml")
}
