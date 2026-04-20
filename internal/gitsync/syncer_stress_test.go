package gitsync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStressPushCommitNoIndexLockRace exercises the acceptance criterion from
// CTXMAX-318: 1000 concurrent HeartbeatCard calls (each triggering a git
// commit) racing against repeated pushWithRetry invocations must produce zero
// index.lock errors. The fix from CTXMAX-364 serialises push against card
// mutations via the service write lock — this test validates that invariant
// under load.
func TestStressPushCommitNoIndexLockRace(t *testing.T) {
	syncer, _, _, _ := setupSyncTest(t)
	ctx := context.Background()

	const (
		numCards       = 10
		goroutines     = 100 // goroutines per run; each does 10 heartbeats
		heartbeatsEach = 10  // heartbeats per goroutine
		semSize        = 8   // bounded concurrency under -race
		testDuration   = 5 * time.Second
		pushInterval   = 100 * time.Millisecond
		minSuccessful  = 3 // floor: at least this many pushes must succeed
	)

	// --- Seed cards and claim each with a unique agent ---
	cards := make([]string, numCards)
	agentIDs := make([]string, numCards)

	for i := range numCards {
		agentIDs[i] = fmt.Sprintf("stress-agent-%02d", i)
		card, err := syncer.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title:    fmt.Sprintf("Stress Card %02d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err, "create card %d", i)
		cards[i] = card.ID

		_, err = syncer.svc.ClaimCard(ctx, "test-project", card.ID, agentIDs[i])
		require.NoError(t, err, "claim card %d (%s)", i, card.ID)
	}

	// --- Stress phase ---
	stressCtx, cancel := context.WithTimeout(ctx, testDuration)
	defer cancel()

	sem := make(chan struct{}, semSize)

	var (
		hbMu   sync.Mutex
		hbErrs []error

		pushMu   sync.Mutex
		pushErrs []error
		pushOK   int
	)

	// Ticker goroutine: push every pushInterval for the full test window.
	var pushWg sync.WaitGroup
	pushWg.Add(1)

	go func() {
		defer pushWg.Done()

		ticker := time.NewTicker(pushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stressCtx.Done():
				return
			case <-ticker.C:
				err := syncer.pushWithRetry(stressCtx)

				pushMu.Lock()
				if err == nil {
					pushOK++
				} else {
					pushErrs = append(pushErrs, err)
				}
				pushMu.Unlock()
			}
		}
	}()

	// 100 goroutines each do 10 heartbeats against their assigned card.
	var hbWg sync.WaitGroup
	for i := range goroutines {
		hbWg.Add(1)

		go func(goroutineIdx int) {
			defer hbWg.Done()

			sem <- struct{}{} // acquire semaphore slot

			defer func() { <-sem }()

			cardIdx := goroutineIdx % numCards
			cardID := cards[cardIdx]
			agentID := agentIDs[cardIdx]

			for range heartbeatsEach {
				err := syncer.svc.HeartbeatCard(stressCtx, "test-project", cardID, agentID)
				if err != nil {
					// Context cancellation at the very end of the window is fine.
					if stressCtx.Err() != nil {
						return
					}

					hbMu.Lock()

					hbErrs = append(hbErrs, err)
					hbMu.Unlock()
				}
			}
		}(i)
	}

	// Wait for all heartbeat goroutines, then signal the push goroutine to stop.
	hbWg.Wait()
	cancel()
	pushWg.Wait()

	// --- Assertions ---

	// No heartbeat error must contain index.lock strings.
	for _, err := range hbErrs {
		msg := err.Error()
		assert.NotContains(t, msg, "index.lock",
			"heartbeat error contains index.lock: %v", err)
		assert.NotContains(t, msg, "unable to lock",
			"heartbeat error contains 'unable to lock': %v", err)
	}

	// All heartbeats must have succeeded (no errors at all, beyond context cancellation).
	assert.Empty(t, hbErrs, "expected zero heartbeat errors, got %d", len(hbErrs))

	// No push error must mention index.lock.
	for _, err := range pushErrs {
		msg := err.Error()
		assert.NotContains(t, msg, "index.lock",
			"push error contains index.lock: %v", err)
		assert.NotContains(t, msg, "unable to lock",
			"push error contains 'unable to lock': %v", err)
	}

	// At least minSuccessful pushes must have completed without error, proving
	// writeMu contention does not deadlock the push path.
	assert.GreaterOrEqual(t, pushOK, minSuccessful,
		"expected at least %d successful pushes, got %d (push errors: %v)",
		minSuccessful, pushOK, pushErrs)
}
