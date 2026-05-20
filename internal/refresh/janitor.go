package refresh

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/mhersson/contextmatrix/internal/clock"
)

// JanitorConfig bundles the tunables for StartJanitor. Callers pass values
// from config.yaml (ultimately) or use the package defaults.
type JanitorConfig struct {
	// ScanInterval is how often the janitor wakes to scan for stale and
	// expired jobs. Default 60s.
	ScanInterval time.Duration

	// StaleThreshold is the LastUpdated age beyond which a Running job is
	// promoted to Failed. Default 30 min.
	StaleThreshold time.Duration

	// PlanningMaxAge is the StartedAt age beyond which a Planning job is
	// promoted to Failed. A Planning job is expected to transition to Running
	// within seconds (Acquire is called immediately before the runner trigger
	// in the same handler). A job stuck in Planning for longer than this
	// threshold indicates a crashed or hung handler goroutine.
	// Default 2× ScanInterval (applied after defaults are resolved).
	PlanningMaxAge time.Duration

	// KeepWindow is how long terminal jobs stay in the registry after
	// FinishedAt before GC removes them. Default 5 min.
	KeepWindow time.Duration

	// StaleErrMessage is the error string recorded on jobs promoted by
	// the staleness check.
	StaleErrMessage string
}

// StartJanitor runs the per-tick stale-promotion + GC sweep until ctx
// is cancelled. Blocks the calling goroutine; main wires it via `go
// StartJanitor(...)`.
//
// Panic recovery: if a sweep panics, the stack is logged and the next sweep
// is delayed with exponential back-off (base 5 s, cap 10 min, reset on
// success) to prevent a deterministic panic from spamming the log every tick.
func StartJanitor(ctx context.Context, r *Registry, clk clock.Clock, cfg JanitorConfig, logger *slog.Logger) {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 60 * time.Second
	}

	if cfg.StaleThreshold <= 0 {
		cfg.StaleThreshold = 30 * time.Minute
	}

	if cfg.KeepWindow <= 0 {
		cfg.KeepWindow = 5 * time.Minute
	}

	if cfg.PlanningMaxAge <= 0 {
		// Default: 2× the scan interval so a Planning job that survives two
		// full sweeps without transitioning is considered stuck.
		cfg.PlanningMaxAge = 2 * cfg.ScanInterval
	}

	if cfg.StaleErrMessage == "" {
		cfg.StaleErrMessage = "no progress callback for staleness threshold; runner may have crashed"
	}

	ticker := clk.NewTicker(cfg.ScanInterval)
	defer ticker.Stop()

	const (
		backoffBase = 5 * time.Second
		backoffCap  = 10 * time.Minute
	)

	backoff := time.Duration(0)
	consecutivePanics := 0

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C():
			if backoff > 0 {
				// A previous sweep panicked; wait out the back-off before
				// attempting the next sweep. We burn the tick but do not
				// reset the ticker so normal cadence resumes once healthy.
				select {
				case <-ctx.Done():
					return
				case <-clk.After(backoff):
				}
			}

			func() {
				defer func() {
					if rec := recover(); rec != nil {
						consecutivePanics++
						stack := debug.Stack()
						logger.Error("refresh janitor sweep panicked",
							"panic", rec,
							"consecutive", consecutivePanics,
							"stack", string(stack),
						)

						// Exponential back-off: 5s, 10s, 20s … capped at 10 min.
						if backoff == 0 {
							backoff = backoffBase
						} else {
							backoff *= 2
						}

						if backoff > backoffCap {
							backoff = backoffCap
						}
					}
				}()

				n := r.PromoteStale(cfg.StaleThreshold, cfg.PlanningMaxAge, cfg.StaleErrMessage)
				if n > 0 {
					logger.Warn("promoted stale refresh jobs to failed",
						"count", n,
						"stale_threshold", cfg.StaleThreshold,
						"planning_max_age", cfg.PlanningMaxAge,
					)
				}

				r.GarbageCollectExpired(cfg.KeepWindow)

				// Sweep completed without panic — reset back-off state.
				consecutivePanics = 0
				backoff = 0
			}()
		}
	}
}
