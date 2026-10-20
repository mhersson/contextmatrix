package api

import (
	"net/http"
	"sync"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

const (
	aboutStatusRunning     = "running"
	aboutStatusConnected   = "connected"
	aboutStatusUnreachable = "unreachable"
)

// aboutComponent is one entry of GET /api/about.
type aboutComponent struct {
	Name                string              `json:"name"`
	Status              string              `json:"status"`
	Build               *protocol.BuildInfo `json:"build,omitempty"`                 // absent when unreachable or older than protocol v0.36.0
	WorkerImageRevision string              `json:"worker_image_revision,omitempty"` // agent and chat only
}

type aboutResponse struct {
	Components []aboutComponent `json:"components"`
}

type aboutBackend struct {
	name   string
	prober healthProber
	cache  healthProbeCache
}

type aboutHandlers struct {
	build    protocol.BuildInfo
	backends []*aboutBackend // agent, chat, architect order; configured ones only
}

// getAbout handles GET /api/about: CM's own build plus each configured
// backend's, probed in parallel through the shared health cache. Backend URLs
// and raw probe errors never leave the server.
func (h *aboutHandlers) getAbout(w http.ResponseWriter, r *http.Request) {
	backends := make([]aboutComponent, len(h.backends))

	var wg sync.WaitGroup

	for i, b := range h.backends {
		wg.Go(func() {
			info, err := b.cache.get(r.Context(), b.prober)
			if err != nil {
				ctxlog.Logger(r.Context()).Warn("about: backend health probe failed", "backend", b.name, "error", err)

				backends[i] = aboutComponent{Name: b.name, Status: aboutStatusUnreachable}

				return
			}

			backends[i] = aboutComponent{
				Name:                b.name,
				Status:              aboutStatusConnected,
				Build:               withChanges(info.Build),
				WorkerImageRevision: info.WorkerImageRevision,
			}
		})
	}

	wg.Wait()

	self := h.build

	components := append([]aboutComponent{{
		Name:   "contextmatrix",
		Status: aboutStatusRunning,
		Build:  withChanges(&self),
	}}, backends...)

	writeJSON(w, http.StatusOK, aboutResponse{Components: components})
}

// withChanges returns a copy of b whose Changes is never nil, so the wire
// carries [] rather than null. A nil b stays nil.
func withChanges(b *protocol.BuildInfo) *protocol.BuildInfo {
	if b == nil {
		return nil
	}

	c := *b
	if c.Changes == nil {
		c.Changes = []protocol.Change{}
	}

	return &c
}
