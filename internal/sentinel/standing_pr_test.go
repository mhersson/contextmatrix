package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestPRState(t *testing.T) {
	rec := state.StandingPR{HeadSHA: "abc"}

	tests := []struct {
		name string
		p    PullState
		want string
	}{
		{"open at the pushed head", PullState{Open: true, HeadSHA: "abc"}, state.PROpen},
		{"open at another head", PullState{Open: true, HeadSHA: "def"}, state.PRChangedOutside},
		{"merged", PullState{Merged: true, HeadSHA: "def", MergeSHA: "m"}, state.PRMerged},
		{"closed unmerged", PullState{HeadSHA: "abc"}, state.PRClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prState(rec, tt.p))
		})
	}
}

func TestMergedFates(t *testing.T) {
	a := state.Finding{ID: 1, Text: "Run ./ci/verify first.\nKeep commits small."}
	b := state.Finding{ID: 2, Text: "Never touch generated files."}

	tests := []struct {
		name         string
		file         string
		found        bool
		proposed     []state.Finding
		wantMerged   []int64
		wantRejected []int64
	}{
		{"spacing changed, B gone", "# Guidance\nRun ./ci/verify  first.\n", true, []state.Finding{a, b}, []int64{1}, []int64{2}},
		{"one line kept, one typo-fixed", "Run ./ci/verify first.\nKeep commits smaller.\n", true, []state.Finding{a}, []int64{1}, nil},
		{"every line gone", "unrelated\n", true, []state.Finding{a, b}, nil, []int64{1, 2}},
		{"kept under a list marker", "- Never touch generated files.\n", true, []state.Finding{b}, []int64{2}, nil},
		{"blank lines are not evidence", "anything\n\n", true, []state.Finding{{ID: 3, Text: "\n  \n"}}, nil, []int64{3}},
		{"human deleted a fenced finding", "```\n-\n---\n", true, []state.Finding{{ID: 4, Text: "```bash\nmake test\n```\n-"}}, nil, []int64{4}},
		{"prose kept around fences", "make test before pushing\n```\n", true, []state.Finding{{ID: 5, Text: "make test before pushing\n```bash\nmake test\n```"}}, []int64{5}, nil},
		{"file deleted by the merge", "", false, []state.Finding{a, b}, nil, []int64{1, 2}},
		{"nothing proposed", "x", true, nil, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, rejected := mergedFates(tt.file, tt.found, tt.proposed)
			assert.Equal(t, tt.wantMerged, merged)
			assert.Equal(t, tt.wantRejected, rejected)
		})
	}
}
