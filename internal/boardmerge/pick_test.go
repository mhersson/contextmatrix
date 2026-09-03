package boardmerge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPick(t *testing.T) {
	tests := []struct {
		name         string
		b, o, th     string
		want         string
		wantConflict bool
	}{
		{"unchanged", "a", "a", "a", "a", false},
		{"ours changed", "a", "b", "a", "b", false},
		{"theirs changed", "a", "a", "c", "c", false},
		{"both same", "a", "b", "b", "b", false},
		{"both differ", "a", "b", "c", "b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, conflict := pick(tt.b, tt.o, tt.th)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantConflict, conflict)
		})
	}
}

func TestPickEq(t *testing.T) {
	type point struct{ x, y int }

	eq := func(a, b point) bool { return a == b }

	tests := []struct {
		name         string
		b, o, th     point
		want         point
		wantConflict bool
	}{
		{"unchanged", point{1, 1}, point{1, 1}, point{1, 1}, point{1, 1}, false},
		{"ours changed", point{1, 1}, point{2, 2}, point{1, 1}, point{2, 2}, false},
		{"theirs changed", point{1, 1}, point{1, 1}, point{3, 3}, point{3, 3}, false},
		{"both same", point{1, 1}, point{2, 2}, point{2, 2}, point{2, 2}, false},
		{"both differ", point{1, 1}, point{2, 2}, point{3, 3}, point{2, 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, conflict := pickEq(tt.b, tt.o, tt.th, eq)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantConflict, conflict)
		})
	}
}

func TestMergeSet(t *testing.T) {
	base := []string{"A", "B", "C"}
	ours := []string{"A", "C", "D"}   // removed B, added D
	theirs := []string{"A", "B", "E"} // removed C, added E
	got := mergeSet(base, ours, theirs, map[string]string{"D": "D2"})
	assert.Equal(t, []string{"A", "E", "D2"}, got)
	assert.Nil(t, mergeSet(nil, nil, nil, nil))
}

func TestAdd3(t *testing.T) {
	assert.Equal(t, int64(15), add3[int64](10, 12, 13))
	assert.InDelta(t, 1.5, add3(1.0, 1.2, 1.3), 1e-9)
}
