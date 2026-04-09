package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapPriority(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Highest", "critical"},
		{"highest", "critical"},
		{"Critical", "critical"},
		{"Blocker", "critical"},
		{"High", "high"},
		{"high", "high"},
		{"Medium", "medium"},
		{"medium", "medium"},
		{"Normal", "medium"},
		{"Low", "low"},
		{"low", "low"},
		{"Lowest", "low"},
		{"Trivial", "low"},
		{"Minor", "low"},
		{"", "medium"},
		{"Unknown", "medium"},
		{"custom-priority", "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, MapPriority(tt.input))
		})
	}
}
