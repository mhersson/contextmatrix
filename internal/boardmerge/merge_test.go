package boardmerge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		path    string
		kind    Kind
		project string
		id      string
	}{
		{"alpha/tasks/ALPHA-001.md", KindCard, "alpha", "ALPHA-001"},
		{"alpha/.board.yaml", KindProject, "alpha", ""},
		{"playbooks/release.yaml", KindPlaybook, "", "release"},
		{"alpha/templates/bug.md", KindOther, "", ""},
		{"alpha/tasks/.tmp-123", KindOther, "", ""},
		{"README.md", KindOther, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			k, p, id := Classify(tt.path)
			assert.Equal(t, tt.kind, k)
			assert.Equal(t, tt.project, p)
			assert.Equal(t, tt.id, id)
		})
	}
}

func TestResolve_OtherTakesTheirs(t *testing.T) {
	out, err := Resolve(Input{Path: "alpha/templates/bug.md", Base: []byte("b"), Ours: []byte("o"), Theirs: []byte("t")}, Context{})
	require.NoError(t, err)
	assert.Equal(t, []byte("t"), out.Content)
	require.Len(t, out.Resolutions, 1)
	assert.Equal(t, RuleTheirsOther, out.Resolutions[0].Rule)

	out, err = Resolve(Input{Path: "alpha/templates/bug.md", Base: []byte("b"), Ours: []byte("o"), Theirs: nil}, Context{})
	require.NoError(t, err)
	assert.True(t, out.Deleted)
}
