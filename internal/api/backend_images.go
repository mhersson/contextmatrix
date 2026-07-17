package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// imageLister is the one backend capability the images route needs.
// *backend.Client satisfies it for both the agent and chat backends - the
// HMAC GET transport is identical.
type imageLister interface {
	ListImages(ctx context.Context) ([]backend.ImageInfo, error)
}

// backendImagesCacheTTL is how long a /images probe result is reused. Image
// inventories change on redeploys, not per-request, so this is deliberately
// longer than the health cache - and the CM-side cache is load-bearing:
// concurrent same-second signed GETs produce identical signatures and would
// collide in the backend's replay cache.
const backendImagesCacheTTL = 30 * time.Second

// backendImagesProbeTimeout bounds one upstream /images call.
const backendImagesProbeTimeout = 5 * time.Second

// imagesProbeCache is a TTL cache for one backend's /images responses,
// mirroring healthProbeCache: successes and failures both cached, concurrent
// callers coalesced via singleflight, probe on a detached context so a
// departing caller cannot poison the TTL window.
type imagesProbeCache struct {
	mu      sync.Mutex
	expires time.Time
	images  []backend.ImageInfo
	err     error

	flight singleflight.Group
}

func (c *imagesProbeCache) get(ctx context.Context, lister imageLister) ([]backend.ImageInfo, error) {
	c.mu.Lock()
	if time.Now().Before(c.expires) {
		images, err := c.images, c.err
		c.mu.Unlock()

		return images, err
	}
	c.mu.Unlock()

	ch := c.flight.DoChan("probe", func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backendImagesProbeTimeout)
		defer cancel()

		images, err := lister.ListImages(probeCtx)

		c.mu.Lock()
		c.images = images
		c.err = err
		c.expires = time.Now().Add(backendImagesCacheTTL)
		c.mu.Unlock()

		return images, err
	})

	select {
	case res := <-ch:
		images, _ := res.Val.([]backend.ImageInfo)

		return images, res.Err
	case <-ctx.Done():
		return nil, fmt.Errorf("backend images probe: %w", ctx.Err())
	}
}

// backendImageEntry is one dropdown candidate in the images response.
type backendImageEntry struct {
	Tags    []string `json:"tags"`
	Digests []string `json:"digests,omitempty"`
	Created int64    `json:"created,omitempty"`
	Size    int64    `json:"size,omitempty"`
}

// backendImagesResponse is the wire shape for GET /api/backends/{backend}/images.
type backendImagesResponse struct {
	OK     bool                `json:"ok"`
	Images []backendImageEntry `json:"images"`
}

// imagesHandlers serves the per-backend worker-image lists feeding the
// project-settings dropdowns. agent/chat are nil when that backend is not
// configured; the handler answers 503 BACKEND_DISABLED for them so the UI
// fails soft. Admin-gated in multi mode (same gate as the project-settings
// PUT the list feeds); none mode has no gate, like the rest of the API.
type imagesHandlers struct {
	agent imageLister // nil when no task backend is configured
	chat  imageLister // nil when no chat backend is configured

	authEnabled bool

	agentCache imagesProbeCache
	chatCache  imagesProbeCache
}

// listImages handles GET /api/backends/{backend}/images.
func (h *imagesHandlers) listImages(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled {
		if requireAdmin(w, r) == nil {
			return
		}
	}

	var (
		lister imageLister
		cache  *imagesProbeCache
	)

	switch r.PathValue("backend") {
	case config.BackendNameAgent:
		lister, cache = h.agent, &h.agentCache
	case config.BackendNameChat:
		lister, cache = h.chat, &h.chatCache
	default:
		writeError(w, http.StatusNotFound, ErrCodeBackendNotFound, "unknown backend", "")

		return
	}

	if lister == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeBackendDisabled, "backend is not configured", "")

		return
	}

	images, err := cache.get(r.Context(), lister)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("backend images probe failed",
			"backend", r.PathValue("backend"), "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "backend images probe failed", "")

		return
	}

	entries := make([]backendImageEntry, 0, len(images))
	for _, img := range images {
		entries = append(entries, backendImageEntry{
			Tags:    img.Tags,
			Digests: img.Digests,
			Created: img.Created,
			Size:    img.Size,
		})
	}

	writeJSON(w, http.StatusOK, backendImagesResponse{OK: true, Images: entries})
}
