package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordClaim(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := &Card{}

	c.RecordClaim("worker-a", now)

	require.Len(t, c.ActivityLog, 1)
	assert.Equal(t, ClaimedAction, c.ActivityLog[0].Action)
	assert.Equal(t, "worker-a", c.ActivityLog[0].Agent)
	assert.Equal(t, now, c.ActivityLog[0].Timestamp)
}

func TestRecordRelease(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("unheld card records nothing", func(t *testing.T) {
		c := &Card{}
		c.RecordRelease(now)
		assert.Empty(t, c.ActivityLog)
	})

	t.Run("records the holder's name", func(t *testing.T) {
		c := &Card{AssignedAgent: "worker-a"}
		c.RecordRelease(now)

		require.Len(t, c.ActivityLog, 1)
		assert.Equal(t, ReleasedAction, c.ActivityLog[0].Action)
		assert.Equal(t, "worker-a", c.ActivityLog[0].Agent)
	})
}
