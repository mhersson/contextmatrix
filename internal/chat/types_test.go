package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_Parse(t *testing.T) {
	cases := []struct {
		in   string
		want Status
		ok   bool
	}{
		{"cold", StatusCold, true},
		{"active", StatusActive, true},
		{"warm-idle", StatusWarmIdle, true},
		{"ending", StatusEnding, true},
		{"garbage", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseStatus(tc.in)
		assert.Equal(t, tc.ok, ok, "parse %q", tc.in)

		if ok {
			assert.Equal(t, tc.want, got)
		}
	}
}
