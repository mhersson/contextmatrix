package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var timingStart = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func minute(n int) time.Time {
	return timingStart.Add(time.Duration(n) * time.Minute)
}

// claimAt and releaseAt mirror what the lock manager does around a claim.
func claimAt(c *Card, agent string, ts time.Time) {
	c.RecordClaim(agent, ts)
	c.AssignedAgent = agent
	c.LastHeartbeat = &ts
}

func releaseAt(c *Card, ts time.Time) {
	c.RecordRelease(ts)
	c.ClearClaim()
}

func TestComputeTiming_ClaimLifecycle(t *testing.T) {
	c := &Card{Created: timingStart, State: StateInProgress}

	c.ComputeTiming(minute(0))
	assert.Equal(t, timingStart, c.StateSince)
	assert.Nil(t, c.WorkedSeconds, "no claim yet")
	require.NotNil(t, c.UnclaimedSince)
	assert.Equal(t, timingStart, *c.UnclaimedSince, "never claimed: unclaimed since the state began")

	claimAt(c, "worker-a", minute(0))
	c.ComputeTiming(minute(2))
	require.NotNil(t, c.WorkingSince)
	assert.Equal(t, minute(0), *c.WorkingSince)
	assert.Nil(t, c.UnclaimedSince)
	require.NotNil(t, c.WorkedSeconds)
	assert.InDelta(t, 120.0, *c.WorkedSeconds, 0.001)

	c.ActivityLog = append(c.ActivityLog, ActivityEntry{Timestamp: minute(10), Action: StateChangedAction, Message: "in_progress -> review"})
	c.State = StateReview
	releaseAt(c, minute(15))
	c.ComputeTiming(minute(60))
	assert.Equal(t, minute(10), c.StateSince)
	assert.Nil(t, c.WorkingSince)
	require.NotNil(t, c.UnclaimedSince)
	assert.Equal(t, minute(15), *c.UnclaimedSince, "unclaimed since the release")
	assert.InDelta(t, 900.0, *c.WorkedSeconds, 0.001)

	claimAt(c, "worker-b", minute(60))
	c.ComputeTiming(minute(61))
	assert.InDelta(t, 960.0, *c.WorkedSeconds, 0.001)
	require.Len(t, c.WorkIntervals, 2)
	assert.Equal(t, "worker-a", c.WorkIntervals[0].Agent)
	require.NotNil(t, c.WorkIntervals[0].To)
	assert.Equal(t, minute(15), *c.WorkIntervals[0].To)
	assert.Nil(t, c.WorkIntervals[1].To, "the held claim is open")
}

func TestComputeTiming_UnknownTotal(t *testing.T) {
	tests := []struct {
		name  string
		build func() *Card
	}{
		{
			name: "held by a claim made before claims were recorded",
			build: func() *Card {
				return &Card{Created: minute(-60), State: StateInProgress, AssignedAgent: "worker"}
			},
		},
		{
			name: "released a claim made before claims were recorded, then claimed again",
			build: func() *Card {
				c := &Card{Created: minute(-60), State: StateInProgress, AssignedAgent: "worker"}
				releaseAt(c, minute(-10))
				claimAt(c, "worker", minute(-5))

				return c
			},
		},
		{
			name: "an unreleased claim the card no longer holds",
			build: func() *Card {
				c := &Card{Created: minute(-60), State: StateReview}
				c.RecordClaim("worker", minute(-30))

				return c
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.build()
			c.ComputeTiming(minute(0))
			assert.Nil(t, c.WorkedSeconds)
		})
	}
}

func TestComputeTiming_HeldWithoutRecordedClaimFallsBackToClaimedAt(t *testing.T) {
	claimedAt := minute(-20)
	c := &Card{Created: minute(-60), State: StateInProgress, AssignedAgent: "worker", ClaimedAt: &claimedAt}

	c.ComputeTiming(minute(0))
	require.NotNil(t, c.WorkingSince)
	assert.Equal(t, claimedAt, *c.WorkingSince)
}

func TestComputeTiming_ReplayedEntries(t *testing.T) {
	c := &Card{Created: minute(-60), State: StateInProgress}
	claimAt(c, "worker", minute(-2))
	c.RecordClaim("worker", minute(-1))
	releaseAt(c, minute(0))
	// A merge can carry the same release twice; that is not a missing claim.
	c.ActivityLog = append(c.ActivityLog, c.ActivityLog[len(c.ActivityLog)-1])

	c.ComputeTiming(minute(5))
	require.Len(t, c.WorkIntervals, 1, "a replayed claim does not start a second interval")
	require.NotNil(t, c.WorkedSeconds)
	assert.InDelta(t, 120.0, *c.WorkedSeconds, 0.001)
}

func TestComputeTiming_TakeoverWithoutReleaseEndsThePreviousClaim(t *testing.T) {
	c := &Card{Created: minute(-60), State: StateInProgress}
	claimAt(c, "worker-a", minute(-10))
	claimAt(c, "worker-b", minute(-4))

	c.ComputeTiming(minute(0))
	require.Len(t, c.WorkIntervals, 2)
	require.NotNil(t, c.WorkIntervals[0].To)
	assert.Equal(t, minute(-4), *c.WorkIntervals[0].To)
	require.NotNil(t, c.WorkedSeconds)
	assert.InDelta(t, 600.0, *c.WorkedSeconds, 0.001)
}

func TestComputeTiming_IsNeverPersisted(t *testing.T) {
	c := &Card{ID: "TEST-001", Title: "t", Project: "p", Type: "task", State: StateTodo, Priority: "low", Created: minute(0), Updated: minute(0)}
	claimAt(c, "worker", minute(0))
	c.ComputeTiming(minute(1))
	require.NotNil(t, c.WorkedSeconds)

	raw, err := SerializeCard(c)
	require.NoError(t, err)

	parsed, err := ParseCard(raw)
	require.NoError(t, err)
	assert.Nil(t, parsed.WorkedSeconds)
	assert.Nil(t, parsed.WorkingSince)
	assert.True(t, parsed.StateSince.IsZero())
	assert.Empty(t, parsed.WorkIntervals)
}

func TestComputeTiming_ManyClaimsOutliveTheLogCap(t *testing.T) {
	c := &Card{Created: timingStart, State: StateInProgress}

	for i := range 60 {
		claimAt(c, "worker", minute(i))
		c.ActivityLog = TrimActivityLog(append(c.ActivityLog,
			ActivityEntry{Agent: "worker", Timestamp: minute(i).Add(10 * time.Second), Action: "note"}))
		releaseAt(c, minute(i).Add(30*time.Second))
	}

	require.Len(t, c.ActivityLog, 120+MaxActivityLogEntries, "notes capped, claims and releases kept")

	c.ComputeTiming(minute(60))
	require.Len(t, c.WorkIntervals, 60)
	require.NotNil(t, c.WorkedSeconds)
	assert.InDelta(t, 1800.0, *c.WorkedSeconds, 0.001)
}
