package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// backendHealthResponse is the wire shape for GET /api/backend/health.
type backendHealthResponse struct {
	OK                bool `json:"ok"`
	RunningContainers int  `json:"running_containers"`
	MaxConcurrent     int  `json:"max_concurrent"`
}

// getBackendHealth handles GET /api/backend/health by proxying to the backend's
// /health endpoint and returning the parsed shape. The UI reads max_concurrent
// from here to render the NowRail capacity meter - it's the backend-global cap,
// not a per-project value. Returns 503 when no task backend is configured and 502
// when the backend is unreachable; callers should fail soft (hide capacity).
//
// Probe results are cached for backendHealthCacheTTL so concurrent tabs and
// rapid refreshes don't tie up the backend with redundant probes, and a backend
// outage doesn't make every browser request block for the full timeout.
// Upstream errors are sanitized (raw err details never leave the server)
// to match how every other backend endpoint handles them.
func (h *backendHandlers) getBackendHealth(w http.ResponseWriter, r *http.Request) {
	if h.backend == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeBackendDisabled, "no execution backend is configured", "")

		return
	}

	info, err := h.healthCache.get(r.Context(), h.backend)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("backend health probe failed", "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "backend health probe failed", "")

		return
	}

	writeJSON(w, http.StatusOK, backendHealthResponse{
		OK:                info.OK,
		RunningContainers: info.RunningContainers,
		MaxConcurrent:     info.MaxConcurrent,
	})
}

// get returns a (possibly cached) backend /health response. The cache TTL
// (backendHealthCacheTTL) is short so operator changes propagate quickly,
// but covers both success and failure so a backend outage cannot cause
// every concurrent caller to issue a fresh probe.
//
// On a cold or expired cache, concurrent callers are coalesced via
// singleflight - exactly one upstream probe runs per TTL window. The
// probe runs against a detached context (`context.WithoutCancel`) so a
// single caller's cancellation (browser tab closed mid-probe) cannot
// write a transient `context.Canceled` into the cache and poison every
// other caller's read for the rest of the TTL window.
//
// `ctx` is only consulted to abandon the wait when the caller goes
// away; the in-flight probe continues so the result still lands in
// the cache for the next caller.
func (c *healthProbeCache) get(ctx context.Context, client TaskBackend) (backend.HealthInfo, error) {
	c.mu.Lock()
	if time.Now().Before(c.expires) {
		info, err := c.info, c.err
		c.mu.Unlock()

		return info, err
	}
	c.mu.Unlock()

	ch := c.flight.DoChan("probe", func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backendHealthProbeTimeout)
		defer cancel()

		info, err := client.Health(probeCtx)

		c.mu.Lock()
		c.info = info
		c.err = err
		c.expires = time.Now().Add(backendHealthCacheTTL)
		c.mu.Unlock()

		return info, err
	})

	select {
	case res := <-ch:
		info, _ := res.Val.(backend.HealthInfo)

		return info, res.Err
	case <-ctx.Done():
		// Caller went away; let the singleflight probe keep running so
		// the next caller benefits from the cached result.
		return backend.HealthInfo{}, fmt.Errorf("backend health probe: %w", ctx.Err())
	}
}
