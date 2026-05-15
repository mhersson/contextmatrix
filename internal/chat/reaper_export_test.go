package chat

import "context"

// SweepStaleRehydrationForTest exports the private sweepStaleRehydration for testing.
func (r *IdleReaper) SweepStaleRehydrationForTest(ctx context.Context) {
	r.sweepStaleRehydration(ctx)
}
