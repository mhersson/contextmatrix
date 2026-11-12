package board

import "time"

// RecordClaim logs the start of a claim. Callers record only a claim that
// changes the holder: a holder's re-claim is liveness, not a new claim.
func (c *Card) RecordClaim(agent string, now time.Time) {
	c.ActivityLog = TrimActivityLog(append(c.ActivityLog, ActivityEntry{
		Agent:     agent,
		Timestamp: now,
		Action:    ClaimedAction,
		Message:   "card claimed",
	}))
}

// RecordRelease logs the end of the current claim under the holder's name,
// so it must run before ClearClaim. An unheld card records nothing.
func (c *Card) RecordRelease(now time.Time) {
	if c.AssignedAgent == "" {
		return
	}

	c.ActivityLog = TrimActivityLog(append(c.ActivityLog, ActivityEntry{
		Agent:     c.AssignedAgent,
		Timestamp: now,
		Action:    ReleasedAction,
		Message:   "claim ended",
	}))
}
