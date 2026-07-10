package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
)

// newProbeServer returns an httptest server whose /health handler runs fn
// and records how many times it was called. Used by the healthProbeCache
// tests to verify caching, singleflight coalescing, and detached probe ctx.
func newProbeServer(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fn(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

func TestHealthProbeCache_CachesSuccessForTTL(t *testing.T) {
	srv, calls := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"running_containers":1,"max_concurrent":4}`))
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache healthProbeCache

	info, err := cache.get(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 4, info.MaxConcurrent)

	// Second call within TTL must NOT hit the upstream.
	_, err = cache.get(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "second call within TTL should be served from cache")
}

func TestHealthProbeCache_CachesFailureForTTL(t *testing.T) {
	srv, calls := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusInternalServerError)
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache healthProbeCache

	_, err := cache.get(context.Background(), client)
	require.Error(t, err)

	_, err = cache.get(context.Background(), client)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "second call within TTL should not re-probe even on failure")
}

func TestHealthProbeCache_SingleflightDeduplicatesConcurrentProbes(t *testing.T) {
	// Block all upstream handlers until release is closed so concurrent
	// callers stack up on the singleflight gate.
	release := make(chan struct{})
	srv, calls := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release

		_, _ = w.Write([]byte(`{"ok":true,"max_concurrent":2}`))
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache healthProbeCache

	const callers = 16

	var (
		wg      sync.WaitGroup
		results = make([]backend.HealthInfo, callers)
		errs    = make([]error, callers)
	)

	for i := range callers {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			results[idx], errs[idx] = cache.get(context.Background(), client)
		}(i)
	}

	// Give the goroutines a moment to stack on the singleflight gate.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, 2, results[i].MaxConcurrent, "caller %d", i)
	}

	assert.Equal(t, int32(1), calls.Load(), "singleflight must collapse concurrent probes to one upstream call")
}

func TestHealthProbeCache_DetachedContextSurvivesCallerCancellation(t *testing.T) {
	// Upstream blocks until release closes. The first caller cancels mid-flight.
	// The cache must NOT store context.Canceled (which would poison the TTL
	// window for every subsequent caller).
	release := make(chan struct{})
	srv, _ := newProbeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release

		_, _ = w.Write([]byte(`{"ok":true,"max_concurrent":3}`))
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache healthProbeCache

	ctx1, cancel1 := context.WithCancel(context.Background())

	type result struct {
		info backend.HealthInfo
		err  error
	}

	caller1Done := make(chan result, 1)

	go func() {
		info, err := cache.get(ctx1, client)
		caller1Done <- result{info, err}
	}()

	// Give caller1 time to enter the singleflight Do.
	time.Sleep(50 * time.Millisecond)
	cancel1()

	r1 := <-caller1Done
	require.Error(t, r1.err)
	require.ErrorIs(t, r1.err, context.Canceled, "caller1 should observe its own cancellation")

	// Release the upstream so the (still-running, detached) probe completes
	// and the cache fills with the SUCCESS result, not context.Canceled.
	close(release)

	// Give the detached probe a moment to land.
	require.Eventually(t, func() bool {
		info, err := cache.get(context.Background(), client)

		return err == nil && info.MaxConcurrent == 3
	}, 1*time.Second, 10*time.Millisecond, "cache should hold the successful probe result, not a poisoned context.Canceled")
}

func TestHealthProbeCache_UpstreamTimeoutTighterThanCallerTimeout(t *testing.T) {
	// Upstream never responds. The probe should fail with deadline exceeded
	// well before the 10s default backend client timeout.
	srv, _ := newProbeServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Second)
	})

	client := backend.NewClient(srv.URL, "test-key")

	var cache healthProbeCache

	start := time.Now()
	_, err := cache.get(context.Background(), client)
	elapsed := time.Since(start)

	require.Error(t, err)
	// Allow some headroom but ensure we don't hit the full 10s client timeout.
	assert.Less(t, elapsed, 5*time.Second,
		"upstream probe should time out via the dedicated backendHealthProbeTimeout (3s), not the full client timeout")

	// Error message should not leak raw transport details either way; that's
	// the responsibility of the handler, not the cache. Cache must surface
	// the error to the caller.
	assert.NotEmpty(t, strings.TrimSpace(err.Error()))
}
