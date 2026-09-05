package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCardDefaults_NilIsBuiltin(t *testing.T) {
	got := ResolveCardDefaults(nil)

	assert.False(t, got.Autonomous)
	assert.False(t, got.MaxCapability)
	assert.Equal(t, 0, got.MobParticipants)
	assert.Nil(t, got.MobPhases)
	require.NotNil(t, got.CreatePR, "resolved defaults must carry a concrete create_pr")
	assert.True(t, *got.CreatePR, "built-in create_pr is on")
	assert.False(t, got.AwaitCI)
	assert.False(t, got.AwaitCopilotReview)
}

func TestResolveCardDefaults_OmittedCreatePRStaysOn(t *testing.T) {
	got := ResolveCardDefaults(&CardDefaults{Autonomous: true})

	assert.True(t, got.Autonomous)
	assert.True(t, got.CreatePROn(), "a block that omits create_pr keeps the built-in true")
}

func TestResolveCardDefaults_ExplicitFalseCreatePRWins(t *testing.T) {
	got := ResolveCardDefaults(&CardDefaults{CreatePR: new(false)})

	assert.False(t, got.CreatePROn())
}

func TestResolveCardDefaults_ClonesPhases(t *testing.T) {
	src := &CardDefaults{MobParticipants: 3, MobPhases: []string{"review"}}
	got := ResolveCardDefaults(src)
	got.MobPhases[0] = "plan"

	assert.Equal(t, []string{"review"}, src.MobPhases, "resolution must not alias the project's slice")
}

func TestCardDefaults_Normalize(t *testing.T) {
	tests := []struct {
		name string
		in   *CardDefaults
		want *CardDefaults
	}{
		{name: "nil stays nil", in: nil, want: nil},
		{name: "empty block drops to nil", in: &CardDefaults{}, want: nil},
		{name: "create_pr true is the built-in and drops to nil", in: &CardDefaults{CreatePR: new(true)}, want: nil},
		{name: "create_pr false is kept", in: &CardDefaults{CreatePR: new(false)}, want: &CardDefaults{CreatePR: new(false)}},
		{name: "phases without seats drop", in: &CardDefaults{MobPhases: []string{"review"}}, want: nil},
		{name: "seats keep phases", in: &CardDefaults{MobParticipants: 3, MobPhases: []string{"review"}}, want: &CardDefaults{MobParticipants: 3, MobPhases: []string{"review"}}},
		{name: "empty phases slice becomes nil", in: &CardDefaults{MobParticipants: 3, MobPhases: []string{}}, want: &CardDefaults{MobParticipants: 3}},
		{name: "flags kept", in: &CardDefaults{Autonomous: true, AwaitCI: true}, want: &CardDefaults{Autonomous: true, AwaitCI: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.Normalize())
		})
	}
}

func TestCardDefaults_NormalizeDoesNotMutateInput(t *testing.T) {
	in := &CardDefaults{CreatePR: new(true), MobPhases: []string{"review"}}
	_ = in.Normalize()

	require.NotNil(t, in.CreatePR)
	assert.True(t, *in.CreatePR)
	assert.Equal(t, []string{"review"}, in.MobPhases)
}

func TestCardDefaults_ProjectYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()

	cfg := &ProjectConfig{
		Name:        "test-project",
		Prefix:      "TEST",
		NextID:      1,
		States:      []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:       []string{"task"},
		Priorities:  []string{"medium"},
		Transitions: map[string][]string{"todo": {"in_progress"}, "stalled": {"todo"}, "not_planned": {"todo"}},
		CardDefaults: &CardDefaults{
			Autonomous: true, MaxCapability: true, MobParticipants: 3, MobPhases: []string{"review"},
			CreatePR: new(false), AwaitCI: true, AwaitCopilotReview: true,
		},
	}

	require.NoError(t, SaveProjectConfig(dir, cfg))

	loaded, err := LoadProjectConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded.CardDefaults)
	assert.True(t, loaded.CardDefaults.Autonomous)
	assert.True(t, loaded.CardDefaults.MaxCapability)
	assert.Equal(t, 3, loaded.CardDefaults.MobParticipants)
	assert.Equal(t, []string{"review"}, loaded.CardDefaults.MobPhases)
	require.NotNil(t, loaded.CardDefaults.CreatePR)
	assert.False(t, *loaded.CardDefaults.CreatePR)
	assert.True(t, loaded.CardDefaults.AwaitCI)
	assert.True(t, loaded.CardDefaults.AwaitCopilotReview)
}

func TestCardDefaults_OmittedFromYAMLWhenNil(t *testing.T) {
	cfg := &ProjectConfig{
		Name:        "test-project",
		Prefix:      "TEST",
		NextID:      1,
		States:      []string{"todo", "stalled", "not_planned"},
		Types:       []string{"task"},
		Priorities:  []string{"medium"},
		Transitions: map[string][]string{"todo": {}, "stalled": {"todo"}, "not_planned": {"todo"}},
	}

	data, err := SerializeProjectConfig(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "card_defaults")
}
