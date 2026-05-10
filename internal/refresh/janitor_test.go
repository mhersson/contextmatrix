package refresh

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/clock"
)

func TestJanitor_PromotesStaleAfterScanInterval(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	_, _ = r.Acquire("p", "r", "human:a")
	_ = r.MarkRunning("p", "r", 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan struct{})

	go func() {
		StartJanitor(ctx, r, clk, JanitorConfig{
			ScanInterval:    60 * time.Second,
			StaleThreshold:  30 * time.Minute,
			KeepWindow:      5 * time.Minute,
			StaleErrMessage: "no progress callback for 30 min",
		}, logger)
		close(done)
	}()

	// Give the goroutine a moment to set up its ticker.
	// Then advance past staleness threshold and the scan interval.
	assert.Eventually(t, func() bool {
		clk.Advance(31 * time.Minute)
		clk.Advance(60 * time.Second)

		snap := r.Snapshot("p")
		j, ok := snap["r"]

		return ok && j.State == StateFailed
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	<-done
}

func TestJanitor_StopsOnContextCancel(t *testing.T) {
	clk := clock.Fake(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	r := NewRegistryWithClock(clk)

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	done := make(chan struct{})

	go func() {
		StartJanitor(ctx, r, clk, JanitorConfig{
			ScanInterval:   60 * time.Second,
			StaleThreshold: 30 * time.Minute,
			KeepWindow:     5 * time.Minute,
		}, logger)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("janitor did not exit on ctx.Done")
	}
}
