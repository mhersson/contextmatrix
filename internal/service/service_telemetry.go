package service

import (
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// phaseStart marks when a card entered its current FSM phase.
type phaseStart struct {
	phase string
	since time.Time
}

// The in-memory timer maps behind the phase-duration and card-run metrics
// are best-effort: a CM restart loses in-flight timers, costing at most one
// observation per open card. Entries normally leave via observeRunEnd or a
// phase ending; the size-gated sweep only catches cards whose runs died
// without a terminal event.
const (
	telemetrySweepThreshold = 128
	telemetryTTL            = 48 * time.Hour
)

func telemetryKey(project, id string) string { return project + "/" + id }

// trackPhaseChange observes the finished phase's duration and starts timing
// the new one. Called after the mutation's commit landed. Empty and done
// phases end timing without starting a new timer.
func (s *CardService) trackPhaseChange(project, id, oldPhase, newPhase string) {
	key := telemetryKey(project, id)
	now := s.clk.Now()

	s.telemetryMu.Lock()
	defer s.telemetryMu.Unlock()

	if e, ok := s.phaseStarts[key]; ok && e.phase == oldPhase {
		metrics.PhaseDuration.WithLabelValues(project, metrics.NormalizePhase(oldPhase)).
			Observe(now.Sub(e.since).Seconds())
	}

	if newPhase == "" || newPhase == "done" {
		delete(s.phaseStarts, key)
	} else {
		s.phaseStarts[key] = phaseStart{phase: newPhase, since: now}
	}

	s.sweepTelemetryLocked(now)
}

// recordRunStart notes a fresh claim. Callers gate on parent/standalone
// cards with no previous agent.
func (s *CardService) recordRunStart(project, id string) {
	key := telemetryKey(project, id)
	now := s.clk.Now()

	s.telemetryMu.Lock()
	defer s.telemetryMu.Unlock()

	s.runStarts[key] = now
	s.sweepTelemetryLocked(now)
}

// observeRunEnd records a terminal run event for a parent or standalone
// card. The run counter and agent count always advance; the duration is
// observed only when the matching claim was seen (pop-guard: CM restarts
// lose the start, and duplicate terminal paths must not observe twice).
func (s *CardService) observeRunEnd(project string, card *board.Card, outcome string) {
	if card.Parent != "" {
		return
	}

	mode, ok := runModeOf(card)
	if !ok {
		mode = "normal"
	}

	key := telemetryKey(project, card.ID)
	now := s.clk.Now()

	s.telemetryMu.Lock()
	start, started := s.runStarts[key]
	delete(s.runStarts, key)
	delete(s.phaseStarts, key)
	s.telemetryMu.Unlock()

	metrics.CardRunsTotal.WithLabelValues(project, outcome, mode).Inc()
	metrics.RunAgents.WithLabelValues(mode).Observe(float64(runAgentCount(card, mode)))

	if started {
		metrics.CardRunDuration.WithLabelValues(project, outcome, mode).Observe(now.Sub(start).Seconds())
	}
}

// releaseOutcome classifies a release by the card's state at release time:
// complete_task transitions to done before releasing, review parks leave the
// card in review, anything else is a plain release.
func releaseOutcome(state string) string {
	switch state {
	case board.StateDone:
		return "completed"
	case board.StateReview:
		return "review_parked"
	default:
		return "released"
	}
}

func runAgentCount(card *board.Card, mode string) int {
	switch mode {
	case "mob":
		return card.MobParticipants + len(card.MobGuests)
	case "best_of_n":
		return card.BestOfN
	default:
		return 1
	}
}

func (s *CardService) sweepTelemetryLocked(now time.Time) {
	if len(s.phaseStarts)+len(s.runStarts) <= telemetrySweepThreshold {
		return
	}

	cutoff := now.Add(-telemetryTTL)

	for k, e := range s.phaseStarts {
		if e.since.Before(cutoff) {
			delete(s.phaseStarts, k)
		}
	}

	for k, ts := range s.runStarts {
		if ts.Before(cutoff) {
			delete(s.runStarts, k)
		}
	}
}
