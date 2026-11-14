package sentinel

import (
	"slices"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// pushEvery is the least time between two of Sentinel's pushes to a project's
// guidance PR, and after a revision that failed or went stale. A refused
// revision pushed nothing and does not count.
const pushEvery = time.Hour

// maxAllowed is the agent's limit on a guidance PR's allowed entries.
const maxAllowed = 64

const (
	reasonRevisionUnreported = "the guidance run never reported"
	reasonRevisionStale      = "the guidance branch or pull request was not as last read"
)

// attributeAbsent traces each entry the text check found absent from the repo
// to the sent findings whose text holds it, ignoring case, and returns
// allowed, the list the revision was sent with, grown. An entry a holder's
// source card has in its human-written text (source, by finding ID) joins
// allowed as reported while it has room; each holder whose source lacks it,
// and every holder of an entry that does not fit, is dropped as introducing
// it. An entry no finding holds drops every sent finding. A finding keeps its
// first reason.
func attributeAbsent(allowed, absent []string, sent []state.Finding, source map[int64]string) ([]string, map[int64]string) {
	var dropped map[int64]string

	drop := func(ids []int64, reason string) {
		for _, id := range ids {
			if dropped == nil {
				dropped = map[int64]string{}
			}

			if _, ok := dropped[id]; !ok {
				dropped[id] = reason
			}
		}
	}

	for _, entry := range absent {
		needle := strings.ToLower(entry)

		var traced, untraced []int64

		for _, f := range sent {
			if !strings.Contains(strings.ToLower(f.Text), needle) {
				continue
			}

			if strings.Contains(strings.ToLower(source[f.ID]), needle) {
				traced = append(traced, f.ID)
			} else {
				untraced = append(untraced, f.ID)
			}
		}

		if len(traced)+len(untraced) == 0 {
			drop(findingIDs(sent), "refused: "+entry+" not traced to a finding")

			continue
		}

		if len(traced) > 0 {
			var left []string
			if allowed, left = withAllowed(allowed, []string{entry}); len(left) > 0 {
				untraced = append(untraced, traced...)
			}
		}

		drop(untraced, "introduces "+entry)
	}

	return allowed, dropped
}

// withAllowed is allowed with each entry of more it lacks appended while it
// holds fewer than maxAllowed; left are the entries that did not fit.
func withAllowed(allowed, more []string) (grown, left []string) {
	for _, e := range more {
		switch {
		case slices.Contains(allowed, e):
			continue
		case len(allowed) >= maxAllowed:
			left = append(left, e)
		default:
			allowed = append(allowed, e)
		}
	}

	return allowed, left
}

func findingIDs(fs []state.Finding) []int64 {
	ids := make([]int64, 0, len(fs))
	for _, f := range fs {
		ids = append(ids, f.ID)
	}

	return ids
}

// revisionAction is what a pass does next for a project's guidance.
type revisionAction int

const (
	revisionWait revisionAction = iota
	revisionDispatch
	revisionEnd
)

// revisionStep is a project's next guidance step.
type revisionStep struct {
	Action        revisionAction
	Stage, Reason string   // revisionEnd
	Unreported    bool     // revisionEnd for a dispatch that never reported
	ExpectHead    string   // revisionDispatch
	Allowed       []string // revisionDispatch
}

// revisionView is what the pass read for one project's guidance.
type revisionView struct {
	Project           string
	PR                *state.StandingPR // nil = none on record
	Pending, Proposed []state.Finding
	Open              *state.Heal // the project's open revision; nil = none
	LastRefused       *state.Heal // its newest refused revision since the last push; nil = none
	LastFailed        *state.Heal // its newest revision that failed or went stale; nil = none
	LastRevised       *state.Heal // its newest revised revision; nil = none
	On                bool        // friction is on, so a revision may go out
	Room              bool        // a worker slot is free for a run kind
	Listed            bool        // the open revision's in-flight dispatch is listed
	Now               time.Time
}

// nextRevision decides a project's next guidance step; the first rule that
// matches wins. A revision in flight or answered finishes whatever friction
// is; only a new send needs it on. An open revision whose dispatch was never
// recorded or was cleared is sent again by the rules after its own.
func nextRevision(v revisionView) revisionStep {
	wait := revisionStep{Action: revisionWait}

	if o := v.Open; o != nil && o.Dispatch.At != nil {
		switch {
		case inFlight(o.Dispatch) && !unreported(o.Dispatch, v.Listed, v.Now):
			return wait
		case inFlight(o.Dispatch):
			s := revisionEnded(state.StageRevisionFailed, reasonRevisionUnreported)
			s.Unreported = true

			return s
		}

		return revisionAnswered(*o)
	}

	pr := v.PR

	switch {
	case !v.On, len(v.Pending) == 0:
		return wait
	case pr != nil && pr.State == state.PRChangedOutside:
		return wait
	case pr != nil && v.Now.Sub(pr.PushedAt) < pushEvery:
		return wait
	case v.LastFailed != nil && v.Now.Sub(v.LastFailed.UpdatedAt) < pushEvery:
		return wait
	case !v.Room:
		return wait
	}

	s := revisionStep{Action: revisionDispatch}

	if pr != nil && pr.State == state.PROpen {
		s.ExpectHead = pr.HeadSHA

		if v.LastRevised != nil {
			s.Allowed, _ = withAllowed(nil, v.LastRevised.Allowed)
		}
	}

	if r := v.LastRefused; r != nil && (pr == nil || r.CreatedAt.After(pr.PushedAt)) {
		s.Allowed, _ = withAllowed(s.Allowed, r.Allowed)
	}

	return s
}

// revisionAnswered ends an answered revision by its outcome; one answered
// without a result failed.
func revisionAnswered(o state.Heal) revisionStep {
	if o.Revision == nil {
		return revisionEnded(state.StageRevisionFailed, o.Dispatch.Failure)
	}

	switch r := o.Revision; r.Outcome {
	case protocol.GuidancePushed, protocol.GuidanceUnchanged:
		return revisionEnded(state.StageRevised, "")
	case protocol.GuidanceStale:
		return revisionEnded(state.StageRevisionStale, reasonRevisionStale)
	case protocol.GuidanceRefused:
		return revisionEnded(state.StageRevisionRefused, "absent from the repo: "+strings.Join(r.AbsentFromRepo, ", "))
	default:
		return revisionEnded(state.StageRevisionFailed, "the guidance run reported outcome "+r.Outcome)
	}
}

func revisionEnded(stage, reason string) revisionStep {
	return revisionStep{Action: revisionEnd, Stage: stage, Reason: reason}
}
