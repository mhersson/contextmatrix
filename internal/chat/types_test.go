package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_String(t *testing.T) {
	assert.Equal(t, "cold", StatusCold.String())
	assert.Equal(t, "active", StatusActive.String())
	assert.Equal(t, "warm-idle", StatusWarmIdle.String())
	assert.Equal(t, "ending", StatusEnding.String())
}

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

func TestNewID_IsULID(t *testing.T) {
	id := NewID()
	assert.Len(t, id, 26, "ULID is 26 chars in Crockford base32")
}
