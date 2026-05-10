package refresh

import (
	"context"
	"log/slog"
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

	if cfg.StaleErrMessage == "" {
		cfg.StaleErrMessage = "no progress callback for staleness threshold; runner may have crashed"
	}

	ticker := clk.NewTicker(cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			n := r.PromoteStale(cfg.StaleThreshold, cfg.StaleErrMessage)
			if n > 0 {
				logger.Warn("promoted stale refresh jobs to failed",
					"count", n,
					"threshold", cfg.StaleThreshold,
				)
			}

			r.GarbageCollectExpired(cfg.KeepWindow)
		}
	}
}
