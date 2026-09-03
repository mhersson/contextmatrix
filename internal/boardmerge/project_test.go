package boardmerge

import (
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func projCfg() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name: "alpha", Prefix: "ALPHA", NextID: 5,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo": {"in_progress"}, "in_progress": {"done"},
			"stalled": {"todo"}, "not_planned": {"todo"},
		},
	}
}

func serializeProj(t *testing.T, cfg *board.ProjectConfig) []byte {
	t.Helper()

	b, err := board.SerializeProjectConfig(cfg)
	require.NoError(t, err)

	return b
}

func TestResolveProject(t *testing.T) {
	base := projCfg()
	ours := projCfg()
	ours.NextID = 7
	ours.States = []string{"todo", "in_progress", "qa", "done", "stalled", "not_planned"}
	ours.Transitions["in_progress"] = []string{"qa", "done"}
	ours.Prefix = "ALPHA"
	theirs := projCfg()
	theirs.NextID = 6
	theirs.Types = []string{"task", "bug"}
	theirs.Repo = "https://github.com/x/y"
	theirs.States = []string{"todo", "done", "stalled", "not_planned"} // removed in_progress
	delete(theirs.Transitions, "todo")                                 // dangling target once in_progress is gone

	out, err := Resolve(Input{
		Path: "alpha/.board.yaml", Base: serializeProj(t, base),
		Ours: serializeProj(t, ours), Theirs: serializeProj(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParseProjectConfig(out.Content)
	require.NoError(t, err)
	assert.Equal(t, 7, got.NextID)
	assert.ElementsMatch(t, []string{"todo", "in_progress", "qa", "done", "stalled", "not_planned"}, got.States)
	assert.ElementsMatch(t, []string{"task", "bug"}, got.Types)
	assert.ElementsMatch(t, []string{"qa", "done"}, got.Transitions["in_progress"])
	assert.Equal(t, "https://github.com/x/y", got.Repo)
	assert.Empty(t, out.Resolutions)
}

func TestResolveProject_ImmutableAndDelete(t *testing.T) {
	base, ours, theirs := projCfg(), projCfg(), projCfg()
	ours.Prefix = "BETA"
	out, err := Resolve(Input{
		Path: "alpha/.board.yaml", Base: serializeProj(t, base),
		Ours: serializeProj(t, ours), Theirs: serializeProj(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParseProjectConfig(out.Content)
	require.NoError(t, err)
	assert.Equal(t, "ALPHA", got.Prefix)
	assert.Equal(t, RuleProjectImmutable, out.Resolutions[0].Rule)

	out, err = Resolve(Input{
		Path: "alpha/.board.yaml", Base: serializeProj(t, base),
		Ours: nil, Theirs: serializeProj(t, theirs),
	}, testCtx())
	require.NoError(t, err)
	assert.True(t, out.Deleted)
}

func TestResolveProject_ScalarAndDeepConflictsKeepRemote(t *testing.T) {
	base := projCfg()
	ours := projCfg()
	ours.Repo = "https://github.com/local/fork"
	ours.RemoteExecution = &board.RemoteExecutionConfig{WorkerImage: "local-image"}
	theirs := projCfg()
	theirs.Repo = "https://github.com/remote/canonical"
	theirs.RemoteExecution = &board.RemoteExecutionConfig{WorkerImage: "remote-image"}

	out, err := Resolve(Input{
		Path: "alpha/.board.yaml", Base: serializeProj(t, base),
		Ours: serializeProj(t, ours), Theirs: serializeProj(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParseProjectConfig(out.Content)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/remote/canonical", got.Repo)
	assert.Equal(t, "remote-image", got.RemoteExecution.WorkerImage)

	rules := make([]string, len(out.Resolutions))
	for i, r := range out.Resolutions {
		rules[i] = r.Rule
	}

	assert.ElementsMatch(t, []string{RuleLaterUpdated, RuleLaterUpdated}, rules)
}

func TestResolveProject_Unparseable(t *testing.T) {
	theirs := projCfg()
	valid := serializeProj(t, theirs)
	garbage := []byte("not: [valid")

	tests := []struct {
		name        string
		ours        []byte
		theirs      []byte
		wantContent []byte
	}{
		{"ours unparseable takes theirs", garbage, valid, valid},
		{"theirs unparseable takes ours", valid, garbage, valid},
		{"neither parses takes theirs", garbage, garbage, garbage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Resolve(Input{Path: "alpha/.board.yaml", Ours: tt.ours, Theirs: tt.theirs}, testCtx())
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, out.Content)
			require.Len(t, out.Resolutions, 1)
			assert.Equal(t, RuleUnparseable, out.Resolutions[0].Rule)
		})
	}
}
