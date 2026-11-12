package board

import (
	"slices"
	"strings"
	"time"
)

// WorkInterval is one recorded claim. A nil To is the claim still held.
type WorkInterval struct {
	Agent string     `json:"agent"`
	From  time.Time  `json:"from"`
	To    *time.Time `json:"to"`
}

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

// ComputeTiming fills the derived timing fields from the activity log:
// StateSince (last transition into the current state, else creation),
// WorkingSince (the held claim's start, else ClaimedAt), UnclaimedSince (last
// release, else StateSince; unheld cards only), WorkIntervals and
// WorkedSeconds. WorkedSeconds stays nil when the log cannot account for
// every claim, such as a claim made before claims were recorded.
func (c *Card) ComputeTiming(now time.Time) {
	c.StateSince = c.Created
	c.WorkingSince, c.UnclaimedSince, c.WorkedSeconds, c.WorkIntervals = nil, nil, nil, nil

	log := slices.Clone(c.ActivityLog)
	slices.SortStableFunc(log, func(a, b ActivityEntry) int { return a.Timestamp.Compare(b.Timestamp) })

	var (
		open        *WorkInterval
		lastRelease *ActivityEntry
		complete    bool
		// gap marks a release without a recorded claim: a claim made before
		// claims were recorded, whose length is unknown.
		gap bool
	)

	for _, e := range log {
		if e.Timestamp.After(now) {
			continue
		}

		switch e.Action {
		case StateChangedAction:
			_, to, ok := strings.Cut(e.Message, " -> ")
			if ok && to == c.State && !e.Timestamp.Before(c.StateSince) {
				c.StateSince = e.Timestamp
			}
		case ClaimedAction:
			// A replayed claim by the same holder does not restart the interval.
			if open != nil && open.Agent == e.Agent {
				continue
			}

			// A takeover without a release ends the previous claim.
			if open != nil {
				end := e.Timestamp
				open.To = &end
				c.WorkIntervals = append(c.WorkIntervals, *open)
			}

			open = &WorkInterval{Agent: e.Agent, From: e.Timestamp}
			complete = true
		case ReleasedAction:
			switch {
			case open != nil && open.Agent == e.Agent:
				end := e.Timestamp
				open.To = &end
				c.WorkIntervals = append(c.WorkIntervals, *open)
				open = nil
			case lastRelease == nil || *lastRelease != e:
				gap = true
			}

			lastRelease = &e
		}
	}

	if open != nil {
		if open.Agent == c.AssignedAgent {
			from := open.From
			c.WorkingSince = &from
			c.WorkIntervals = append(c.WorkIntervals, *open)
		} else {
			// An unreleased claim the card no longer holds (an old or merged
			// log) has no known end, so the total is unknown.
			complete = false
		}
	}

	if c.AssignedAgent != "" && c.WorkingSince == nil {
		c.WorkingSince = c.ClaimedAt
	}

	if c.AssignedAgent == "" {
		since := c.StateSince
		if lastRelease != nil {
			since = lastRelease.Timestamp
		}

		c.UnclaimedSince = &since
	}

	if complete && !gap {
		total := 0.0

		for _, interval := range c.WorkIntervals {
			end := now
			if interval.To != nil {
				end = *interval.To
			}

			if end.After(interval.From) {
				total += end.Sub(interval.From).Seconds()
			}
		}

		c.WorkedSeconds = &total
	}
}
