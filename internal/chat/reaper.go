package chat

import (
	"context"
	"time"
)

// IdleReaper periodically scans warm-idle sessions and ends those whose
// last_active is older than the Manager's configured IdleTTL.
type IdleReaper struct {
	mgr      *Manager
	interval time.Duration
}

// NewIdleReaper wires a reaper. interval should be << IdleTTL in production;
// tests pass a tiny interval for quick triggering.
func NewIdleReaper(mgr *Manager, interval time.Duration) *IdleReaper {
	return &IdleReaper{mgr: mgr, interval: interval}
}

// Run blocks until ctx is cancelled.
func (r *IdleReaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *IdleReaper) tick(ctx context.Context) {
	r.sweepWarmIdle(ctx)
	r.sweepStaleRehydration(ctx)
}

// sweepWarmIdle ends sessions whose warm-idle TTL has elapsed.
func (r *IdleReaper) sweepWarmIdle(ctx context.Context) {
	sessions, err := r.mgr.store.ListSessions(ctx, SessionFilter{Status: StatusWarmIdle})
	if err != nil {
		r.mgr.logger.Warn("chat: reaper list failed", "error", err)

		return
	}

	cutoff := r.mgr.clk.Now().Add(-r.mgr.idleTTL)

	for _, sess := range sessions {
		if sess.LastActive.Before(cutoff) {
			if err := r.mgr.EndSession(ctx, sess.ID); err != nil {
				r.mgr.logger.Warn("chat: reaper end failed", "session_id", sess.ID, "error", err)
			}
		}
	}
}

// sweepStaleRehydration forces rehydration_active off for sessions whose
// phase has been open longer than the configured timeout. Safety net for
// agents that crashed or otherwise never reached chat_rehydration_complete
// and where the operator hasn't yet typed.
//
// The filter uses RehydrationStartedBefore (not LastActiveBefore) so an
// actively-typing user whose agent crashed mid-rehydration does not keep
// LastActive fresh and evade the sweep.
func (r *IdleReaper) sweepStaleRehydration(ctx context.Context) {
	if r.mgr.rehydrationTimeout <= 0 {
		return
	}

	active := true

	sessions, err := r.mgr.store.ListSessions(ctx, SessionFilter{
		RehydrationActive:        &active,
		RehydrationStartedBefore: r.mgr.clk.Now().Add(-r.mgr.rehydrationTimeout),
	})
	if err != nil {
		r.mgr.logger.Warn("chat: reaper rehydration list failed", "error", err)

		return
	}

	for _, sess := range sessions {
		if err := r.mgr.setRehydrationActive(ctx, sess.ID, false); err != nil {
			r.mgr.logger.Warn("chat: reaper rehydration clear failed",
				"session_id", sess.ID, "error", err)

			continue
		}

		r.mgr.logger.Info("chat: rehydration phase forced off by timeout",
			"session_id", sess.ID)
	}
}
